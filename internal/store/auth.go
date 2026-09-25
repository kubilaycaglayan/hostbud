package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDuplicate is returned when a unique key already exists.
var ErrDuplicate = errors.New("already exists")

// User is a row of users. PasswordHash is an encoded Argon2id hash.
type User struct {
	ID              string
	Email           string
	EmailNormalized string
	PasswordHash    string
	Disabled        bool
	CreatedAt       time.Time
	LastLoginAt     *time.Time
}

// AuthSession is a row of auth_sessions (the token itself is never stored).
type AuthSession struct {
	IDHash     string
	UserID     string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time
	UserAgent  string
	CreatedIP  string
}

// RateLimit is a row of login_rate_limits.
type RateLimit struct {
	Key           string
	Failures      int
	BlockedUntil  time.Time // zero: not blocked
	LastFailureAt time.Time // zero: never failed
}

// AuthRepository is the persistence the auth package needs.
type AuthRepository interface {
	EmailAllowed(ctx context.Context, emailNormalized string) (bool, error)
	CreateUser(ctx context.Context, u User) error
	UserByEmail(ctx context.Context, emailNormalized string) (User, error)
	RecordLogin(ctx context.Context, userID string, at time.Time) error
	CreateSession(ctx context.Context, s AuthSession) error
	SessionUser(ctx context.Context, idHash string, now time.Time) (User, AuthSession, error)
	TouchSession(ctx context.Context, idHash string, at time.Time) error
	DeleteSession(ctx context.Context, idHash string) error
	RateLimits(ctx context.Context, keys ...string) (map[string]RateLimit, error)
	UpdateRateLimit(ctx context.Context, key string, update func(RateLimit) RateLimit) (RateLimit, error)
	ClearRateLimit(ctx context.Context, key string) error
}

var _ AuthRepository = (*Store)(nil)

// EmailAllowed reports whether the address is enabled in email_allowlist.
func (s *Store) EmailAllowed(ctx context.Context, emailNormalized string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		`SELECT enabled FROM email_allowlist WHERE email_normalized = $1`, emailNormalized).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// CreateUser inserts a user; ErrDuplicate if the email is taken.
func (s *Store) CreateUser(ctx context.Context, u User) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, email_normalized, password_hash) VALUES ($1, $2, $3, $4)`,
		u.ID, u.Email, u.EmailNormalized, u.PasswordHash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrDuplicate
	}
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

const userCols = `u.id, u.email, u.email_normalized, u.password_hash, u.disabled, u.created_at, u.last_login_at`

func scanUser(scan func(...any) error, extra ...any) (User, error) {
	var u User
	var last sql.NullTime
	err := scan(append([]any{&u.ID, &u.Email, &u.EmailNormalized, &u.PasswordHash, &u.Disabled, &u.CreatedAt, &last}, extra...)...)
	if last.Valid {
		t := last.Time.UTC()
		u.LastLoginAt = &t
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return u, err
}

// UserByEmail returns a user or ErrNotFound.
func (s *Store) UserByEmail(ctx context.Context, emailNormalized string) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users u WHERE u.email_normalized = $1`, emailNormalized).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// RecordLogin sets last_login_at.
func (s *Store) RecordLogin(ctx context.Context, userID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = $1, updated_at = $1 WHERE id = $2`, at, userID)
	return err
}

// CreateSession stores a new session.
func (s *Store) CreateSession(ctx context.Context, a AuthSession) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_sessions (id_hash, user_id, expires_at, created_at, last_seen_at, user_agent, created_ip)
		VALUES ($1, $2, $3, $4, $4, $5, $6)`,
		a.IDHash, a.UserID, a.ExpiresAt, a.CreatedAt, a.UserAgent, a.CreatedIP)
	return err
}

// SessionUser returns the unexpired session and its enabled user, or
// ErrNotFound.
func (s *Store) SessionUser(ctx context.Context, idHash string, now time.Time) (User, AuthSession, error) {
	var a AuthSession
	u, err := scanUser(s.db.QueryRowContext(ctx, `
		SELECT `+userCols+`, a.id_hash, a.user_id, a.expires_at, a.created_at, a.last_seen_at
		FROM auth_sessions a JOIN users u ON u.id = a.user_id
		WHERE a.id_hash = $1 AND a.expires_at > $2 AND NOT u.disabled`, idHash, now).Scan,
		&a.IDHash, &a.UserID, &a.ExpiresAt, &a.CreatedAt, &a.LastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, a, ErrNotFound
	}
	return u, a, err
}

// TouchSession records activity on a session.
func (s *Store) TouchSession(ctx context.Context, idHash string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE auth_sessions SET last_seen_at = $1 WHERE id_hash = $2`, at, idHash)
	return err
}

// DeleteSession revokes a session (no error if it doesn't exist).
func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE id_hash = $1`, idHash)
	return err
}

// RateLimits returns the stored buckets among keys (missing keys are absent).
func (s *Store) RateLimits(ctx context.Context, keys ...string) (map[string]RateLimit, error) {
	out := map[string]RateLimit{}
	for _, k := range keys {
		rl, err := scanRateLimit(s.db.QueryRowContext(ctx,
			`SELECT scope_key, failures, blocked_until, last_failure_at FROM login_rate_limits WHERE scope_key = $1`, k).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[k] = rl
	}
	return out, nil
}

func scanRateLimit(scan func(...any) error) (RateLimit, error) {
	var rl RateLimit
	var blocked, last sql.NullTime
	if err := scan(&rl.Key, &rl.Failures, &blocked, &last); err != nil {
		return rl, err
	}
	if blocked.Valid {
		rl.BlockedUntil = blocked.Time.UTC()
	}
	if last.Valid {
		rl.LastFailureAt = last.Time.UTC()
	}
	return rl, nil
}

// UpdateRateLimit applies update to a bucket atomically: the row is locked
// (created if missing) for the duration, so concurrent failures all count.
func (s *Store) UpdateRateLimit(ctx context.Context, key string, update func(RateLimit) RateLimit) (RateLimit, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RateLimit{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO login_rate_limits (scope_key) VALUES ($1) ON CONFLICT (scope_key) DO NOTHING`, key); err != nil {
		return RateLimit{}, err
	}
	rl, err := scanRateLimit(tx.QueryRowContext(ctx,
		`SELECT scope_key, failures, blocked_until, last_failure_at FROM login_rate_limits WHERE scope_key = $1 FOR UPDATE`, key).Scan)
	if err != nil {
		return RateLimit{}, err
	}
	rl = update(rl)
	if _, err := tx.ExecContext(ctx, `
		UPDATE login_rate_limits SET failures = $1, blocked_until = $2, last_failure_at = $3, updated_at = now()
		WHERE scope_key = $4`,
		rl.Failures, nullTime(rl.BlockedUntil), nullTime(rl.LastFailureAt), key); err != nil {
		return RateLimit{}, err
	}
	return rl, tx.Commit()
}

func nullTime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: !t.IsZero()} }

// ClearRateLimit resets a bucket.
func (s *Store) ClearRateLimit(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_rate_limits WHERE scope_key = $1`, key)
	return err
}
