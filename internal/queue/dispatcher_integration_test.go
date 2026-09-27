//go:build integration

package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
