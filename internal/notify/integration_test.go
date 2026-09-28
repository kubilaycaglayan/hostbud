//go:build integration

package notify

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"hostbud/internal/store"
)

func itStore(t *testing.T) *store.Store {
	t.Helper()
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-password" //nolint:gosec // disposable integration database
	}
	schema := fmt.Sprintf("notify_it_%x", sha256.Sum256([]byte(t.TempDir())))[:24]
	st, err := store.Open(context.Background(), store.Config{Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: password, SSLMode: "disable", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

type itFixture struct {
	st    *store.Store
	fake  *pushFake
	devs  map[string]device // path → device
	queue store.Queue
	item  store.QueueItem
}

// newITFixture: account a (on, every event, two devices), account b (on,
// done not chosen, one device), account c (off, one device); a queue with
// one running item.
func newITFixture(t *testing.T) *itFixture {
	t.Helper()
	ctx := context.Background()
	f := &itFixture{st: itStore(t), fake: newPushFake(t), devs: map[string]device{}}
	if _, err := f.st.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	prefs := map[string]store.NotificationPrefs{
		"user_a": {Enabled: true, OnDone: true, OnAttention: true, OnFinished: true},
		"user_b": {Enabled: true, OnDone: false, OnAttention: true, OnFinished: true},
		"user_c": {Enabled: false, OnDone: true, OnAttention: true, OnFinished: true},
	}
	for user, p := range prefs {
		if err := f.st.CreateUser(ctx, store.User{ID: user, Email: user + "@example.com", EmailNormalized: user + "@example.com", PasswordHash: "hash"}); err != nil {
			t.Fatal(err)
		}
		if err := f.st.PutNotificationPrefs(ctx, user, p); err != nil {
			t.Fatal(err)
		}
	}
	for path, user := range map[string]string{"/a1": "user_a", "/a2": "user_a", "/b1": "user_b", "/c1": "user_c"} {
		dev := newDevice(t)
		f.devs[path] = dev
		if _, err := f.st.SavePushSubscription(ctx, user, f.fake.srv.URL+path, dev.p256dh(), dev.secret()); err != nil {
			t.Fatal(err)
		}
	}
	p, err := f.st.CreateProject(ctx, store.HostMachineID, "/home/dev/app", "app")
	if err != nil {
		t.Fatal(err)
	}
	if f.queue, err = f.st.CreateQueue(ctx, p.ID, "Q"); err != nil {
		t.Fatal(err)
	}
	if f.item, err = f.st.AddQueueItem(ctx, f.queue.ID, "claude", "--secret-flag", "/goal secret instruction"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.TransitionQueueItem(ctx, f.item.ID, []string{store.ItemQueued}, store.ItemRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.TransitionQueue(ctx, f.queue.ID, []string{store.QueueIdle}, store.QueueRunning); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *itFixture) notice(t *testing.T, kind string) *store.Notice {
	t.Helper()
	p, err := Build(Event{Kind: kind, RunID: "01RUN", QueueID: f.queue.ID, ItemID: f.item.ID, Project: "app", Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := p.JSON()
	return &store.Notice{Kind: kind, Key: p.Key, Payload: b}
}

func (f *itFixture) payloads(t *testing.T, path string) []Payload {
	t.Helper()
	var out []Payload
	for _, r := range f.fake.posts(path) {
		var p Payload
		if err := json.Unmarshal(f.devs[path].decrypt(t, r.body), &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// V2-M3 T2: through the real store, a done transition reaches only the
// accounts that chose done (both of a's devices), and queue finished
// reaches three devices with each account's own choice; every POST
// decrypts to the allowlisted payload.
func TestIntegrationPushFanOutToDevicesAndAccounts(t *testing.T) {
	ctx := context.Background()
	f := newITFixture(t)
	s, _ := newTestSender(t, f.st, f.fake, nil)
	if _, err := f.st.TransitionQueueItemNotify(ctx, f.item.ID, []string{store.ItemRunning}, store.ItemDone, f.notice(t, KindDone)); err != nil {
		t.Fatal(err)
	}
	s.Drain(ctx)
	for path, n := range map[string]int{"/a1": 1, "/a2": 1, "/b1": 0, "/c1": 0} {
		if got := len(f.fake.posts(path)); got != n {
			t.Fatalf("done: %s got %d POSTs, want %d", path, got, n)
		}
	}
	if _, err := f.st.TransitionQueueNotify(ctx, f.queue.ID, []string{store.QueueRunning}, store.QueueFinished, f.notice(t, KindFinished)); err != nil {
		t.Fatal(err)
	}
	s.Drain(ctx)
	for path, n := range map[string]int{"/a1": 2, "/a2": 2, "/b1": 1, "/c1": 0} {
		if got := len(f.fake.posts(path)); got != n {
			t.Fatalf("finished: %s got %d POSTs, want %d", path, got, n)
		}
	}
	for _, path := range []string{"/a1", "/a2", "/b1"} {
		for _, p := range f.payloads(t, path) {
			raw, _ := json.Marshal(p)
			if p.Project != "app" || p.URL != "/queues/"+f.queue.ID+"?item="+f.item.ID || containsAny(string(raw), "secret", "/home/dev", "@example.com") {
				t.Fatalf("%s payload %s", path, raw)
			}
		}
	}
}

// V2-M3 T3: a 500 then 201 is one notification; a 410 removes the device
// and its pending deliveries, with no retry.
func TestIntegrationPushRetryAndExpiry(t *testing.T) {
	ctx := context.Background()
	f := newITFixture(t)
	f.fake.answers["/a1"] = []int{500, 201}
	f.fake.answers["/a2"] = []int{410}
	s, sleeps := newTestSender(t, f.st, f.fake, nil)
	if _, err := f.st.TransitionQueueItemNotify(ctx, f.item.ID, []string{store.ItemRunning}, store.ItemDone, f.notice(t, KindDone)); err != nil {
		t.Fatal(err)
	}
	s.Drain(ctx)
	if len(f.payloads(t, "/a1")) != 2 || len(f.fake.posts("/a2")) != 1 || len(sleeps.got) != 1 {
		t.Fatalf("POSTs a1=%d a2=%d, sleeps %v", len(f.fake.posts("/a1")), len(f.fake.posts("/a2")), sleeps.got)
	}
	subs, _ := f.st.PushSubscriptions(ctx, "user_a")
	if len(subs) != 1 || subs[0].Endpoint != f.fake.srv.URL+"/a1" {
		t.Fatalf("user_a subscriptions after 410: %+v", subs)
	}
	// The next event doesn't reach the removed device.
	if _, err := f.st.TransitionQueueNotify(ctx, f.queue.ID, []string{store.QueueRunning}, store.QueueFinished, f.notice(t, KindFinished)); err != nil {
		t.Fatal(err)
	}
	s.Drain(ctx)
	if len(f.fake.posts("/a2")) != 1 || len(f.fake.posts("/a1")) != 3 {
		t.Fatalf("after removal: a1=%d a2=%d", len(f.fake.posts("/a1")), len(f.fake.posts("/a2")))
	}
}

// V2-M3 T3: after a restart, unclaimed deliveries are sent once and a
// delivery claimed before the crash is never sent again.
func TestIntegrationPushRestartSendsEachKeyOnce(t *testing.T) {
	ctx := context.Background()
	f := newITFixture(t)
	if _, err := f.st.TransitionQueueItemNotify(ctx, f.item.ID, []string{store.ItemRunning}, store.ItemDone, f.notice(t, KindDone)); err != nil {
		t.Fatal(err)
	}
	// The old process claimed one delivery and died before its POST.
	claimed, err := f.st.ClaimDelivery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newTestSender(t, f.st, f.fake, nil) // the new process
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { s.Run(runCtx); close(done) }()
	s.Wake()
	s.Wake()
	s.Drain(ctx) // settle: nothing left to claim after Run's own drain
	cancel()
	<-done
	s.Drain(ctx)
	got := map[string]int{}
	for _, path := range []string{"/a1", "/a2"} {
		got[path] = len(f.fake.posts(path))
	}
	claimedPath := claimed.Endpoint[len(f.fake.srv.URL):]
	for path, n := range got {
		want := 1
		if path == claimedPath {
			want = 0
		}
		if n != want {
			t.Fatalf("POSTs %v (claimed before the crash: %s)", got, claimedPath)
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
