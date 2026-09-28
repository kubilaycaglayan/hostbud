//go:build integration

package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"hostbud/internal/agents"
	"hostbud/internal/fsbrowse"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/testenv"
)

// hookServer serves POST /api/hooks/{run}/{event} with the real receiver,
// on an address the target can reach.
func hookServer(t *testing.T, hooks *Hooks) string {
	t.Helper()
	ip := ""
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
			ip = n.IP.String()
		}
	}
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", ip+":0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hooks/{run}/{event}", func(w http.ResponseWriter, r *http.Request) {
		err := hooks.Receive(r.Context(), r.PathValue("run"), r.PathValue("event"), r.Header.Get("Authorization"), r.Body)
		var he *HookError
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.As(err, &he):
			w.WriteHeader(he.Status)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String()
}

func setStubBehavior(t *testing.T, c *sshx.Client, condition, behavior string) {
	t.Helper()
	sum := sha256.Sum256([]byte(condition))
	testenv.Sh(t, c, "mkdir -p ~/.hostbud-stubs/goal && echo "+sshx.Quote(behavior+" delay=0.2 verdict_delay=0.4")+" > ~/.hostbud-stubs/goal/"+hex.EncodeToString(sum[:]))
}

// V2-M1 T9: with the stub clients on test/sshd, a queue runs a Claude item
// then a Codex item strictly in order, advancing only on their achieved
// goal state; a failing item pauses the queue and its session stays alive;
// a replayed hook can't advance twice; client configs never change.
func TestIntegrationDispatcherRunsStubItemsInOrder(t *testing.T) {
	e := newITEnv(t)
	ctx := context.Background()
	testenv.Sh(t, e.c, "rm -rf ~/.hostbud-stubs ~/.codex/stub-goals.json")
	t.Cleanup(func() { testenv.Sh(t, e.c, "rm -rf ~/.hostbud-stubs ~/.codex ~/.claude") })
	checksum := fixtureClientConfigs(t, e.c)
	before := checksum()

	files := fsbrowse.New(e.c, sshx.HostMachineID, time.Minute, 5*time.Second)
	t.Cleanup(func() { _ = files.Close() })
	registry := agents.NewRegistry(
		agents.NewClaude(e.c, func(string) agents.Files { return files }, func(string) string { return "/home/dev" }),
		agents.NewCodex(e.c, 10*time.Second),
	)
	svc := NewService(e.st, registry, e.bus)
	hooks := NewHooks(e.st, nil, nil)
	url := hookServer(t, hooks)
	d := NewDispatcher(e.st, registry, NewStarter(e.st, e.sessions, url, nil), svc, e.bus, time.Hour, nil)
	hooks.SetNotifier(d)
	dctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { d.Run(dctx); close(done) }()
	t.Cleanup(func() { stop(); <-done })

	setStubBehavior(t, e.c, "it claude m1", "achieve:2")
	setStubBehavior(t, e.c, "it codex m2", "achieve:1")
	setStubBehavior(t, e.c, "it claude m3", "fail")
	for _, it := range []struct{ agent, instruction string }{{"claude", "/goal it claude m1"}, {"codex", "/goal it codex m2"}, {"claude", "/goal it claude m3"}, {"claude", "/goal it claude m4"}} {
		if _, err := svc.AddItem(ctx, e.queue.ID, it.agent, "", it.instruction); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Start(ctx, e.queue.ID); err != nil {
		t.Fatal(err)
	}
	statuses := func() []string {
		items, _ := e.st.QueueItems(ctx, e.queue.ID)
		var out []string
		for _, it := range items {
			out = append(out, it.Status)
		}
		return out
	}
	maxActive := 0
	deadline := time.Now().Add(60 * time.Second)
	for {
		runs, _ := e.st.ActiveRuns(ctx)
		maxActive = max(maxActive, len(runs))
		q, _ := e.st.Queue(ctx, e.queue.ID)
		if q.Status == store.QueuePaused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue %s, items %v", q.Status, statuses())
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := statuses(); !slices.Equal(got, []string{store.ItemDone, store.ItemDone, store.ItemNeedsAttention, store.ItemQueued}) {
		t.Fatalf("items %v", got)
	}
	if maxActive > 1 {
		t.Fatalf("%d runs were active at once", maxActive)
	}
	latest, _ := e.st.LatestRuns(ctx, e.queue.ID)
	items, _ := e.st.QueueItems(ctx, e.queue.ID)
	var names []string
	for i, it := range items[:3] {
		r := latest[it.ID]
		names = append(names, r.SessionName)
		want := []string{store.RunAchieved, store.RunAchieved, store.RunFailed}[i]
		if r.Status != want || r.ClientVersion == "" {
			t.Fatalf("item %d run %+v, want %s", i+1, r, want)
		}
	}
	if !slices.Equal(names, []string{"app-q1", "app-q2", "app-q3"}) {
		t.Fatalf("session names %v", names)
	}
	// hostbud never closed a session, including the failed run's.
	sessions := testenv.Sh(t, e.c, "tmux list-sessions -F '#{session_name}'")
	for _, name := range names {
		if !strings.Contains(sessions, name+"\n") {
			t.Fatalf("session %s is gone: %q", name, sessions)
		}
	}
	// A replayed hook for the achieved run is refused (token revoked) and
	// starts nothing.
	env := testenv.Sh(t, e.c, "tmux show-environment -t =app-q1 HOSTBUD_RUN_TOKEN")
	token := strings.TrimSpace(strings.TrimPrefix(env, "HOSTBUD_RUN_TOKEN="))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/hooks/"+latest[items[0].ID].ID+"/turn_end", strings.NewReader(`{"session_id":"x"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	d.Sync()
	if res.StatusCode != http.StatusGone || len(statuses()) != 4 || statuses()[3] != store.ItemQueued {
		t.Fatalf("replayed hook: %d, items %v", res.StatusCode, statuses())
	}
	if after := checksum(); after != before {
		t.Fatalf("client configs changed:\n%s\n%s", before, after)
	}
}

// V2-M1 T9 (restart safety): a goal achieved while the dispatcher is down
// is picked up by the recovery read when it starts again.
func TestIntegrationDispatcherRecoversAfterRestart(t *testing.T) {
	e := newITEnv(t)
	ctx := context.Background()
	testenv.Sh(t, e.c, "rm -rf ~/.hostbud-stubs")
	t.Cleanup(func() { testenv.Sh(t, e.c, "rm -rf ~/.hostbud-stubs ~/.claude") })
	files := fsbrowse.New(e.c, sshx.HostMachineID, time.Minute, 5*time.Second)
	t.Cleanup(func() { _ = files.Close() })
	registry := agents.NewRegistry(agents.NewClaude(e.c, func(string) agents.Files { return files }, func(string) string { return "/home/dev" }))
	svc := NewService(e.st, registry, e.bus)
	hooks := NewHooks(e.st, nil, nil)
	url := hookServer(t, hooks)
	newDispatcher := func() (*Dispatcher, func()) {
		d := NewDispatcher(e.st, registry, NewStarter(e.st, e.sessions, url, nil), svc, e.bus, time.Hour, nil)
		hooks.SetNotifier(d)
		dctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { d.Run(dctx); close(done) }()
		return d, func() { stop(); <-done }
	}
	// silent-then-achieve: SessionStart, then the achieved record with no hook.
	setStubBehavior(t, e.c, "it restart m1", "silent-then-achieve:3")
	_, _ = svc.AddItem(ctx, e.queue.ID, "claude", "", "/goal it restart m1")
	_, _ = svc.AddItem(ctx, e.queue.ID, "claude", "", "/goal it restart m2")
	d, stop := newDispatcher()
	if _, err := svc.Start(ctx, e.queue.ID); err != nil {
		t.Fatal(err)
	}
	first := func() store.Run {
		latest, _ := e.st.LatestRuns(ctx, e.queue.ID)
		items, _ := e.st.QueueItems(ctx, e.queue.ID)
		return latest[items[0].ID]
	}
	deadline := time.Now().Add(20 * time.Second)
	for first().Status != store.RunRunning {
		if time.Now().After(deadline) {
			t.Fatalf("run never bound: %+v", first())
		}
		time.Sleep(100 * time.Millisecond)
	}
	d.Sync()
	stop() // hostbud goes down; the stub achieves its goal meanwhile
	time.Sleep(4 * time.Second)
	if first().Status != store.RunRunning {
		t.Fatal("the run changed while the dispatcher was down")
	}
	_, stop = newDispatcher()
	defer stop()
	deadline = time.Now().Add(20 * time.Second)
	for {
		items, _ := e.st.QueueItems(ctx, e.queue.ID)
		if first().Status == store.RunAchieved && items[0].Status == store.ItemDone && items[1].Status == store.ItemRunning {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after restart: run %s, items %s/%s", first().Status, items[0].Status, items[1].Status)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// slotRig is the real dispatcher with the stub clients on test/sshd and
// the parallel-queues switch on (V2-M2 T4).
type slotRig struct {
	e     *itEnv
	svc   *Service
	d     *Dispatcher
	stop  func()
	url   string
	reg   *agents.Registry
	hooks *Hooks
}

func newSlotRig(t *testing.T) *slotRig {
	t.Helper()
	e := newITEnv(t)
	testenv.Sh(t, e.c, "rm -rf ~/.hostbud-stubs ~/.codex/stub-goals.json")
	t.Cleanup(func() { testenv.Sh(t, e.c, "rm -rf ~/.hostbud-stubs ~/.codex ~/.claude") })
	files := fsbrowse.New(e.c, sshx.HostMachineID, time.Minute, 5*time.Second)
	t.Cleanup(func() { _ = files.Close() })
	reg := agents.NewRegistry(
		agents.NewClaude(e.c, func(string) agents.Files { return files }, func(string) string { return "/home/dev" }),
		agents.NewCodex(e.c, 10*time.Second),
	)
	svc := NewService(e.st, reg, e.bus)
	svc.SetParallelQueues(true)
	hooks := NewHooks(e.st, nil, nil)
	r := &slotRig{e: e, svc: svc, url: hookServer(t, hooks), reg: reg, hooks: hooks}
	r.run(t)
	return r
}

// run starts a dispatcher (again, after a restart) wired to the hooks.
func (r *slotRig) run(t *testing.T) {
	t.Helper()
	r.d = NewDispatcher(r.e.st, r.reg, NewStarter(r.e.st, r.e.sessions, r.url, nil), r.svc, r.e.bus, time.Hour, nil)
	r.hooks.SetNotifier(r.d)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.d.Run(ctx); close(done) }()
	r.stop = func() { cancel(); <-done }
	t.Cleanup(func() { r.stop() })
}

func (r *slotRig) itemStatuses(t *testing.T, queueID string) []string {
	t.Helper()
	items, err := r.e.st.QueueItems(context.Background(), queueID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		out = append(out, it.Status)
	}
	return out
}

func (r *slotRig) waitFor(t *testing.T, what string, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			runs, _ := r.e.st.ActiveRuns(context.Background())
			queues, _ := r.e.st.Queues(context.Background(), store.HostMachineID)
			var state []string
			for _, q := range queues {
				latest, _ := r.e.st.LatestRuns(context.Background(), q.ID)
				for _, run := range latest {
					state = append(state, q.Name+":"+run.Status+":"+run.Detail)
				}
				state = append(state, fmt.Sprintf("%s %s %v", q.Name, q.Status, r.itemStatuses(t, q.ID)))
			}
			t.Fatalf("timed out waiting for %s (%d active; %v)", what, len(runs), state)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// V2-M2 T4: two queues whose runs achieve at the same moment with a cap of
// 1 start exactly one next run; after a restart with that run active the
// count still holds and the other queue keeps waiting.
func TestIntegrationSlotsSimultaneousFinishesAndRestart(t *testing.T) {
	r := newSlotRig(t)
	ctx := context.Background()
	st := r.e.st
	alpha := r.e.queue // the project's first queue ("Milestones")
	beta, err := r.svc.Create(ctx, r.e.project.ID, "Beta")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct{ id, name string }{{alpha.ID, "alpha"}, {beta.ID, "beta"}} {
		setStubBehavior(t, r.e.c, "it slots "+q.name+" 1", "achieve:2")
		setStubBehavior(t, r.e.c, "it slots "+q.name+" 2", "pending")
		for n := 1; n <= 2; n++ {
			if _, err := r.svc.AddItem(ctx, q.id, "claude", "", fmt.Sprintf("/goal it slots %s %d", q.name, n)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []string{alpha.ID, beta.ID} {
		if _, err := r.svc.Start(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	// No cap yet: both first items run at once.
	r.waitFor(t, "both first runs", 20*time.Second, func() bool {
		n, _ := st.CountActiveRuns(ctx, store.HostMachineID)
		return n == 2
	})
	if _, err := r.svc.SetCapacity(ctx, intp(1)); err != nil {
		t.Fatal(err)
	}
	// Both achieve (about 0.4 s apart at most); exactly one second item starts.
	r.waitFor(t, "both first items done", 30*time.Second, func() bool {
		return r.itemStatuses(t, alpha.ID)[0] == store.ItemDone && r.itemStatuses(t, beta.ID)[0] == store.ItemDone
	})
	r.waitFor(t, "one next run", 20*time.Second, func() bool {
		n, _ := st.CountActiveRuns(ctx, store.HostMachineID)
		return n == 1
	})
	for range 20 { // it stays at one
		if n, _ := st.CountActiveRuns(ctx, store.HostMachineID); n != 1 {
			t.Fatalf("%d active runs over cap 1", n)
		}
		time.Sleep(100 * time.Millisecond)
	}
	second := map[string]string{alpha.ID: r.itemStatuses(t, alpha.ID)[1], beta.ID: r.itemStatuses(t, beta.ID)[1]}
	running, waitingID := "", ""
	for id, status := range second {
		switch status {
		case store.ItemRunning:
			running = id
		case store.ItemQueued:
			waitingID = id
		}
	}
	if running == "" || waitingID == "" {
		t.Fatalf("second items %v: want one running, one queued", second)
	}
	v, err := r.svc.Get(ctx, waitingID)
	if err != nil || !v.Items[1].WaitingForSlot {
		t.Fatalf("the other queue isn't waiting for a slot: %+v, %v", v.Items[1], err)
	}

	// Restart with that run active: the count includes it before any
	// dispatch; the other queue keeps waiting.
	r.stop()
	r.run(t)
	time.Sleep(2 * time.Second)
	if n, _ := st.CountActiveRuns(ctx, store.HostMachineID); n != 1 {
		t.Fatalf("after restart: %d active runs, cap 1", n)
	}
	if got := r.itemStatuses(t, waitingID)[1]; got != store.ItemQueued {
		t.Fatalf("after restart the waiting queue's item is %s", got)
	}
	if q, _ := st.Queue(ctx, waitingID); q.WaitingSince == nil {
		t.Fatal("the waiting order was lost across the restart")
	}
	// Raising the cap dispatches at once.
	if _, err := r.svc.SetCapacity(ctx, intp(2)); err != nil {
		t.Fatal(err)
	}
	r.waitFor(t, "the waiting queue's run after raising the cap", 20*time.Second, func() bool {
		return r.itemStatuses(t, waitingID)[1] == store.ItemRunning
	})
}
