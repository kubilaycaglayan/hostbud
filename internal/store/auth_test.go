package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAllowlistIsPlainSQL(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()

	if ok, err := s.EmailAllowed(ctx, "person@example.com"); ok || err != nil {
		t.Fatalf("empty allowlist: %v %v", ok, err)
	}
	// The owner's documented SQL.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO email_allowlist (email_normalized) VALUES ('person@example.com')`); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.EmailAllowed(ctx, "person@example.com"); !ok {
		t.Fatal("inserted address not allowed")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE email_allowlist SET enabled = FALSE WHERE email_normalized = 'person@example.com'`); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.EmailAllowed(ctx, "person@example.com"); ok {
		t.Fatal("disabled address still allowed")
	}
	// Only normalized addresses can be inserted.
	for _, bad := range []string{"Person@example.com", " person@example.com", ""} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO email_allowlist (email_normalized) VALUES ($1)`, bad); err == nil {
			t.Errorf("allowlist accepted %q", bad)
		}
	}
}

func TestUsersAndSessions(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	now := time.Now().UTC().Truncate(time.Microsecond)

	u := User{ID: "01TESTUSER000000000000000A", Email: "Person@example.com", EmailNormalized: "person@example.com", PasswordHash: "$argon2id$x"} //nolint:gosec // placeholder, not a credential
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser(ctx, User{ID: "other", Email: "p", EmailNormalized: "person@example.com", PasswordHash: "h"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	got, err := s.UserByEmail(ctx, "person@example.com")
	if err != nil || got.ID != u.ID || got.Email != u.Email || got.LastLoginAt != nil {
		t.Fatalf("user %+v %v", got, err)
	}
	if _, err := s.UserByEmail(ctx, "ghost@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	if err := s.RecordLogin(ctx, u.ID, now); err != nil {
		t.Fatal(err)
	}

	if err := s.CreateSession(ctx, AuthSession{IDHash: "h1", UserID: u.ID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	su, sess, err := s.SessionUser(ctx, "h1", now)
	if err != nil || su.ID != u.ID || sess.UserID != u.ID || su.LastLoginAt == nil {
		t.Fatalf("session user %+v %+v %v", su, sess, err)
	}
	if _, _, err := s.SessionUser(ctx, "h1", now.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired session returned")
	}
	if err := s.TouchSession(ctx, "h1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET disabled = TRUE WHERE id = $1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SessionUser(ctx, "h1", now); !errors.Is(err, ErrNotFound) {
		t.Fatal("disabled user's session returned")
	}
	if err := s.DeleteSession(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM auth_sessions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("sessions left: %d %v", n, err)
	}
}

func TestRateLimitUpdatesAreAtomic(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()

	const workers = 20
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			if _, err := s.UpdateRateLimit(ctx, "k", func(rl RateLimit) RateLimit {
				rl.Failures++
				rl.LastFailureAt = time.Now().UTC()
				return rl
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, err := s.RateLimits(ctx, "k", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if got["k"].Failures != workers || got["k"].LastFailureAt.IsZero() || !got["k"].BlockedUntil.IsZero() {
		t.Fatalf("concurrent failures: %+v", got["k"])
	}
	if _, ok := got["missing"]; ok {
		t.Fatal("missing key returned")
	}
	until := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	if _, err := s.UpdateRateLimit(ctx, "k", func(rl RateLimit) RateLimit { rl.BlockedUntil = until; return rl }); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.RateLimits(ctx, "k"); !got["k"].BlockedUntil.Equal(until) {
		t.Fatalf("blocked_until %v, want %v", got["k"].BlockedUntil, until)
	}
	if err := s.ClearRateLimit(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.RateLimits(ctx, "k"); len(got) != 0 {
		t.Fatal("bucket not cleared")
	}
}
