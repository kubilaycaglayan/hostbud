package store

import (
	"context"
	"errors"
	"testing"
)

func notifyFixture(t *testing.T, users ...string) *Store {
	t.Helper()
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	for _, id := range users {
		if err := s.CreateUser(ctx, User{ID: id, Email: id + "@example.com", EmailNormalized: id + "@example.com", PasswordHash: "hash"}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// V2-M3 T0: no row = off with every event chosen; one account's settings
// never change another's; NotificationsEnabled follows the switches.
func TestNotificationPrefsPerAccount(t *testing.T) {
	ctx := context.Background()
	s := notifyFixture(t, "user_a", "user_b")
	p, err := s.NotificationPrefs(ctx, "user_a")
	if err != nil || p != DefaultNotificationPrefs || p.Enabled {
		t.Fatalf("fresh account = %+v, %v; want off", p, err)
	}
	if on, err := s.NotificationsEnabled(ctx); err != nil || on {
		t.Fatalf("NotificationsEnabled with no rows = %v, %v", on, err)
	}
	want := NotificationPrefs{Enabled: true, OnDone: false, OnAttention: true, OnFinished: false}
	if err := s.PutNotificationPrefs(ctx, "user_a", want); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.NotificationPrefs(ctx, "user_a"); got != want {
		t.Fatalf("user_a = %+v, want %+v", got, want)
	}
	if got, _ := s.NotificationPrefs(ctx, "user_b"); got != DefaultNotificationPrefs {
		t.Fatalf("user_b changed: %+v", got)
	}
	if on, _ := s.NotificationsEnabled(ctx); !on {
		t.Fatal("NotificationsEnabled = false with an account on")
	}
	want.Enabled = false
	if err := s.PutNotificationPrefs(ctx, "user_a", want); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.NotificationsEnabled(ctx); on {
		t.Fatal("NotificationsEnabled = true after the only account turned it off")
	}
	if err := s.PutNotificationPrefs(ctx, "nobody", want); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
}
