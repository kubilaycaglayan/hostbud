package store

import (
	"context"
	"errors"
	"testing"
	"time"
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

func mustSubscribe(t *testing.T, s *Store, user, endpoint string) PushSubscription {
	t.Helper()
	sub, err := s.SavePushSubscription(context.Background(), user, endpoint, "BKey"+endpoint, "auth")
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func outboxKeys(t *testing.T, s *Store) map[string][]string {
	t.Helper()
	rows, err := s.db.QueryContext(context.Background(), `SELECT user_id, dedupe_key FROM notification_outbox ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]string{}
	for rows.Next() {
		var u, k string
		_ = rows.Scan(&u, &k)
		out[u] = append(out[u], k)
	}
	return out
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// V2-M3 T2: a transition writes one outbox row per opted-in account whose
// event choice matches, and one delivery per subscription, in its own
// transaction; a duplicate signal (lost guarded update or repeated key)
// writes nothing more.
func TestTransitionFansOutNotices(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	for _, id := range []string{"user_a", "user_b", "user_c"} {
		if err := s.CreateUser(ctx, User{ID: id, Email: id + "@example.com", EmailNormalized: id + "@example.com", PasswordHash: "hash"}); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.PutNotificationPrefs(ctx, "user_a", NotificationPrefs{Enabled: true, OnDone: true, OnAttention: true, OnFinished: true})
	_ = s.PutNotificationPrefs(ctx, "user_b", NotificationPrefs{Enabled: true, OnDone: false, OnAttention: true, OnFinished: true})
	_ = s.PutNotificationPrefs(ctx, "user_c", NotificationPrefs{Enabled: false, OnDone: true, OnAttention: true, OnFinished: true})
	mustSubscribe(t, s, "user_a", "https://push.example.com/a1")
	mustSubscribe(t, s, "user_a", "https://push.example.com/a2")
	mustSubscribe(t, s, "user_b", "https://push.example.com/b1")
	mustSubscribe(t, s, "user_c", "https://push.example.com/c1")

	q, err := s.CreateQueue(ctx, p.ID, "Q")
	if err != nil {
		t.Fatal(err)
	}
	it, err := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransitionQueueItem(ctx, it.ID, []string{ItemQueued}, ItemRunning); err != nil {
		t.Fatal(err)
	}
	done := &Notice{Kind: "done", Key: "run:r1:done", Payload: []byte(`{"v":1}`)}
	if _, err := s.TransitionQueueItemNotify(ctx, it.ID, []string{ItemRunning}, ItemDone, done); err != nil {
		t.Fatal(err)
	}
	// A duplicate signal loses the guarded update: no second row.
	if _, err := s.TransitionQueueItemNotify(ctx, it.ID, []string{ItemRunning}, ItemDone, done); !errors.Is(err, ErrConflict) {
		t.Fatalf("second transition: %v", err)
	}
	if got := outboxKeys(t, s); len(got) != 1 || len(got["user_a"]) != 1 {
		t.Fatalf("outbox after done = %v; only user_a chose done", got)
	}
	if n := countRows(t, s, "notification_deliveries"); n != 2 {
		t.Fatalf("deliveries = %d, want user_a's two devices", n)
	}
	if _, err := s.TransitionQueue(ctx, q.ID, []string{QueueIdle}, QueueRunning); err != nil {
		t.Fatal(err)
	}
	fin := &Notice{Kind: "finished", Key: "queue:q:finished:r1", Payload: []byte(`{"v":1}`)}
	if _, err := s.TransitionQueueNotify(ctx, q.ID, []string{QueueRunning}, QueueFinished, fin); err != nil {
		t.Fatal(err)
	}
	if got := outboxKeys(t, s); len(got["user_a"]) != 2 || len(got["user_b"]) != 1 || got["user_c"] != nil {
		t.Fatalf("outbox after finished = %v", got)
	}
	if n := countRows(t, s, "notification_deliveries"); n != 5 {
		t.Fatalf("deliveries = %d, want 2 + 2 + 1", n)
	}
	// The same key again (a restart replaying a transition) adds nothing.
	if _, err := s.TransitionQueue(ctx, q.ID, []string{QueueFinished}, QueueRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransitionQueueNotify(ctx, q.ID, []string{QueueRunning}, QueueFinished, fin); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "notification_outbox"); n != 3 {
		t.Fatalf("outbox rows = %d after a repeated key", n)
	}
	// A failed transaction leaves no rows: an unknown kind rolls back.
	if _, err := s.TransitionQueue(ctx, q.ID, []string{QueueFinished}, QueueRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransitionQueueNotify(ctx, q.ID, []string{QueueRunning}, QueuePaused, &Notice{Kind: "bogus", Key: "k"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if got, _ := s.Queue(ctx, q.ID); got.Status != QueueRunning {
		t.Fatalf("the transition committed without its notice: %s", got.Status)
	}
}

// V2-M3 T2: an endpoint moves to the account that subscribes it last,
// dropping the old account's pending deliveries; delete is per account.
func TestPushSubscriptionMovesAndDeletes(t *testing.T) {
	ctx := context.Background()
	s := notifyFixture(t, "user_a", "user_b")
	a := mustSubscribe(t, s, "user_a", "https://push.example.com/shared")
	_ = s.PutNotificationPrefs(ctx, "user_a", NotificationPrefs{Enabled: true, OnDone: true, OnAttention: true, OnFinished: true})
	if err := s.EnqueueTestNotification(ctx, "user_a", a.Endpoint, Notice{Kind: "done", Key: "test:1", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	b := mustSubscribe(t, s, "user_b", "https://push.example.com/shared")
	if b.ID != a.ID || b.UserID != "user_b" {
		t.Fatalf("moved subscription = %+v (was %+v)", b, a)
	}
	if subs, _ := s.PushSubscriptions(ctx, "user_a"); len(subs) != 0 {
		t.Fatalf("user_a still has %d subscriptions", len(subs))
	}
	if n := countRows(t, s, "notification_deliveries"); n != 0 {
		t.Fatalf("user_a's pending delivery followed the device: %d", n)
	}
	if gone, err := s.DeletePushSubscription(ctx, "user_a", b.Endpoint); err != nil || gone {
		t.Fatalf("user_a deleted user_b's device: %v %v", gone, err)
	}
	if gone, err := s.DeletePushSubscription(ctx, "user_b", b.Endpoint); err != nil || !gone {
		t.Fatalf("delete own: %v %v", gone, err)
	}
	if _, err := s.SavePushSubscription(ctx, "nobody", "https://push.example.com/x", "k", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
	if err := s.EnqueueTestNotification(ctx, "user_b", "https://push.example.com/none", Notice{Kind: "done", Key: "test:2", Payload: []byte(`{}`)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("test to a device the account doesn't have: %v", err)
	}
}

// V2-M3 T3: a delivery is claimed once (never handed out again, even to a
// new sender after a restart); 404/410 removal takes pending deliveries
// with it; pruning drops rows older than the cutoff.
func TestDeliveriesAreClaimedOnceAndPruned(t *testing.T) {
	ctx := context.Background()
	s := notifyFixture(t, "user_a")
	one := mustSubscribe(t, s, "user_a", "https://push.example.com/1")
	two := mustSubscribe(t, s, "user_a", "https://push.example.com/2")
	for i, endpoint := range []string{one.Endpoint, two.Endpoint, one.Endpoint} {
		if err := s.EnqueueTestNotification(ctx, "user_a", endpoint, Notice{Kind: "done", Key: "test:" + string(rune('a'+i)), Payload: []byte(`{"v":1}`)}); err != nil {
			t.Fatal(err)
		}
	}
	d1, err := s.ClaimDelivery(ctx)
	if err != nil || d1.Endpoint != one.Endpoint || d1.Key != "test:a" || string(d1.Payload) != `{"v":1}` {
		t.Fatalf("first claim = %+v, %v", d1, err)
	}
	if err := s.FinishDelivery(ctx, d1.OutboxID, d1.SubscriptionID, DeliverySent); err != nil {
		t.Fatal(err)
	}
	d2, _ := s.ClaimDelivery(ctx) // claimed, then the sender "crashes" before finishing
	if d2.Key != "test:b" {
		t.Fatalf("second claim = %+v", d2)
	}
	// Device one expires: its pending delivery (test:c) goes with it.
	if err := s.DeletePushSubscriptionByID(ctx, one.ID); err != nil {
		t.Fatal(err)
	}
	if d, err := s.ClaimDelivery(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after the claims and the removal: %+v, %v; want none (claimed rows are never re-sent)", d, err)
	}
	if err := s.FinishDelivery(ctx, 1, "x", "bogus"); err == nil {
		t.Fatal("bogus status accepted")
	}
	if n, err := s.PruneNotifications(ctx, time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("prune of fresh rows = %d, %v", n, err)
	}
	if n, err := s.PruneNotifications(ctx, time.Now().Add(time.Hour)); err != nil || n != 3 {
		t.Fatalf("prune = %d, %v; want every outbox row", n, err)
	}
	if n := countRows(t, s, "notification_deliveries"); n != 0 {
		t.Fatalf("deliveries left after prune: %d", n)
	}
}
