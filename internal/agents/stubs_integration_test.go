//go:build integration

package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/fsbrowse"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/testenv"
	"hostbud/internal/tmux"
)

// hookSink records the stubs' hook calls: they run the real injected hook
// command (curl) on the target, which POSTs here.
type hookSink struct {
	mu    sync.Mutex
	calls map[string][]sinkCall // by run id
	url   string
}

type sinkCall struct {
	event string
	body  []byte
}

func newHookSink(t *testing.T, token string) *hookSink {
	t.Helper()
	ip := ""
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
			ip = n.IP.String()
		}
	}
	if ip == "" {
		t.Skip("no non-loopback address for the hook sink")
	}
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", ip+":0")
	if err != nil {
		t.Fatal(err)
	}
	s := &hookSink{calls: map[string][]sinkCall{}, url: "http://" + ln.Addr().String()}
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/hooks/"), "/")
		body, _ := io.ReadAll(r.Body)
		if len(parts) != 2 || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s.mu.Lock()
		s.calls[parts[0]] = append(s.calls[parts[0]], sinkCall{parts[1], body})
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return s
}

func (s *hookSink) events(run string) []sinkCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls[run])
}

func (s *hookSink) wait(t *testing.T, run string, done func([]sinkCall) bool) []sinkCall {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		calls := s.events(run)
		if done(calls) {
			return calls
		}
		if time.Now().After(deadline) {
			var got []string
			for _, c := range calls {
				got = append(got, c.event)
			}
			t.Fatalf("run %s: hooks %v never reached the expected state", run, got)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func count(calls []sinkCall, event string) int {
	n := 0
	for _, c := range calls {
		if c.event == event {
			n++
		}
	}
	return n
}

type stubCase struct {
	behavior string
	// ready says the hooks so far are enough to judge the goal state.
	ready func([]sinkCall) bool
	want  string
}

const stubToken = "stub-drift-token"

// startStub starts the adapter's command in a tmux session on the target,
// with the run env and a per-condition stub behavior.
func startStub(t *testing.T, c *sshx.Client, a Adapter, sink *hookSink, run, condition, behavior string) {
	t.Helper()
	sum := sha256.Sum256([]byte(condition))
	testenv.Sh(t, c, fmt.Sprintf("mkdir -p ~/.hostbud-stubs/goal && echo %s > ~/.hostbud-stubs/goal/%s", sshx.Quote(behavior+" delay=0.2 verdict_delay=0.4"), hex.EncodeToString(sum[:])))
	argv, err := a.BuildCommand(store.QueueItem{Agent: a.Kind(), Instruction: "/goal " + condition}, store.Run{ID: run})
	if err != nil {
		t.Fatal(err)
	}
	args, script, err := tmux.NewSessionScript(tmux.NewSession{
		Name: "stub-" + strings.ToLower(run), Path: "/home/dev/stubs-it",
		Env:       map[string]string{"HOSTBUD_URL": sink.url, "HOSTBUD_RUN_ID": run, "HOSTBUD_RUN_TOKEN": stubToken},
		StartArgv: argv,
	}, tmux.Version{Major: 3, Minor: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExecInput(context.Background(), sshx.HostMachineID, script, args...); err != nil {
		t.Fatal(err)
	}
}

func claudeCases() map[string]stubCase {
	has := func(event string, n int) func([]sinkCall) bool {
		return func(c []sinkCall) bool { return count(c, event) >= n }
	}
	return map[string]stubCase{
		"achieve:2":             {"achieve:2", has(EventTurnEnd, 2), Achieved},
		"decoy":                 {"decoy", has(EventTurnEnd, 1), Pending},
		"fail":                  {"fail", has(EventTurnEnd, 1), Failed},
		"exit":                  {"exit", has(EventSessionEnd, 1), Pending},
		"silent":                {"silent", has(EventSessionStart, 1), Pending},
		"silent-then-achieve:1": {"silent-then-achieve:1", has(EventSessionStart, 1), Achieved},
		"pending":               {"pending", has(EventTurnEnd, 1), Pending},
	}
}

// V2-M1 T7: the stubs and the adapters can't drift. Each stub behavior
// runs on the target, its injected hooks reach a sink with the run's token,
// and the adapter parses its hook bodies and reads its goal state.
func TestIntegrationStubsAgainstAdapters(t *testing.T) {
	ctx := context.Background()
	c := testenv.Connected(t, testenv.SSHD)
	testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -rf ~/.hostbud-stubs ~/stubs-it ~/.codex/stub-goals.json ~/.claude/projects/-home-dev-stubs-it; mkdir -p ~/stubs-it")
	t.Cleanup(func() {
		testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -rf ~/.hostbud-stubs ~/stubs-it ~/.codex ~/.claude/projects/-home-dev-stubs-it")
	})
	files := fsbrowse.New(c, sshx.HostMachineID, time.Minute, 5*time.Second)
	t.Cleanup(func() { _ = files.Close() })
	sink := newHookSink(t, stubToken)
	claude := NewClaude(c, func(string) Files { return files }, func(string) string { return "/home/dev" })
	codex := NewCodex(c, 10*time.Second)
	started := time.Now().Add(-2 * time.Second)

	t.Run("claude", func(t *testing.T) {
		var wg sync.WaitGroup
		for name, tc := range claudeCases() {
			run, condition := "CL"+strings.ToUpper(strings.NewReplacer(":", "", "-", "").Replace(name)), "stub claude goal "+name
			startStub(t, c, claude, sink, run, condition, tc.behavior)
			wg.Add(1)
			go func() {
				defer wg.Done()
				calls := sink.wait(t, run, tc.ready)
				b, err := claude.ParseHook(EventSessionStart, calls[0].body)
				if err != nil || calls[0].event != EventSessionStart {
					t.Errorf("%s: first hook %s: %v", name, calls[0].event, err)
					return
				}
				deadline := time.Now().Add(10 * time.Second)
				for {
					st, err := claude.ReadGoalState(ctx, sshx.HostMachineID, b, store.Run{StartedAt: started}, condition)
					if err == nil && st.Status == tc.want && (tc.want != Pending || time.Now().After(deadline.Add(-8*time.Second))) {
						return
					}
					if time.Now().After(deadline) {
						t.Errorf("%s: goal state %+v, %v; want %s", name, st, err, tc.want)
						return
					}
					time.Sleep(200 * time.Millisecond)
				}
			}()
		}
		wg.Wait()
	})

	t.Run("claude clear", func(t *testing.T) {
		run, condition := "CLCLEAR", "stub claude goal clear"
		startStub(t, c, claude, sink, run, condition, "clear")
		calls := sink.wait(t, run, func(c []sinkCall) bool { return count(c, EventSessionStart) >= 2 })
		first, _ := claude.ParseHook(EventSessionStart, calls[0].body)
		var end, second Binding
		for _, call := range calls[1:] {
			switch call.event {
			case EventSessionEnd:
				end, _ = claude.ParseHook(call.event, call.body)
			case EventSessionStart:
				second, _ = claude.ParseHook(call.event, call.body)
			}
		}
		if end.Reason != "clear" || end.SessionID != first.SessionID || second.SessionID == first.SessionID || second.Source != "clear" {
			t.Fatalf("clear: first %+v end %+v second %+v", first, end, second)
		}
	})

	t.Run("codex", func(t *testing.T) {
		cases := map[string]stubCase{
			"achieve:2":             {"achieve:2", func(c []sinkCall) bool { return count(c, EventTurnEnd) >= 2 }, Achieved},
			"fail":                  {"fail", func(c []sinkCall) bool { return count(c, EventTurnEnd) >= 1 }, Failed},
			"decoy":                 {"decoy", func(c []sinkCall) bool { return count(c, EventTurnEnd) >= 1 }, Pending},
			"pending":               {"pending", func(c []sinkCall) bool { return count(c, EventTurnEnd) >= 1 }, Pending},
			"silent-then-achieve:1": {"silent-then-achieve:1", func(c []sinkCall) bool { return count(c, EventSessionStart) >= 1 }, Achieved},
		}
		var wg sync.WaitGroup
		for name, tc := range cases {
			run, condition := "CX"+strings.ToUpper(strings.NewReplacer(":", "", "-", "").Replace(name)), "stub codex goal "+name
			startStub(t, c, codex, sink, run, condition, tc.behavior)
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := sink.wait(t, run, func(c []sinkCall) bool { return count(c, EventSessionStart) >= 1 })
				b, err := codex.ParseHook(EventSessionStart, start[0].body)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					return
				}
				// What the dispatcher does on session_start.
				if err := codex.Arm(ctx, sshx.HostMachineID, b, store.Run{StartedAt: started}, condition); err != nil {
					t.Errorf("%s: arm: %v", name, err)
					return
				}
				sink.wait(t, run, tc.ready)
				deadline := time.Now().Add(10 * time.Second)
				for {
					st, err := codex.ReadGoalState(ctx, sshx.HostMachineID, b, store.Run{StartedAt: started}, condition)
					if err == nil && st.Status == tc.want {
						return
					}
					if time.Now().After(deadline) {
						t.Errorf("%s: goal state %+v, %v; want %s", name, st, err, tc.want)
						return
					}
					time.Sleep(200 * time.Millisecond)
				}
			}()
		}
		wg.Wait()
	})

	t.Run("codex without arming never achieves", func(t *testing.T) {
		run, condition := "CXNOARM", "stub codex goal never armed"
		startStub(t, c, codex, sink, run, condition, "achieve:1")
		calls := sink.wait(t, run, func(c []sinkCall) bool { return count(c, EventTurnEnd) >= 1 })
		b, _ := codex.ParseHook(EventSessionStart, calls[0].body)
		if st, err := codex.ReadGoalState(ctx, sshx.HostMachineID, b, store.Run{StartedAt: started}, condition); err != nil || st.Status != Unknown {
			t.Fatalf("unarmed: %+v, %v", st, err)
		}
	})

	// V2-M1 T6: reads never change Codex's state, and a missing codex is an
	// actionable error.
	t.Run("codex reader", func(t *testing.T) {
		before := testenv.Sh(t, c, "sha256sum ~/.codex/stub-goals.json")
		for range 3 {
			if _, err := codex.ReadGoalState(ctx, sshx.HostMachineID, Binding{SessionID: "no-such-thread"}, store.Run{}, "x"); err != nil {
				t.Fatal(err)
			}
		}
		if after := testenv.Sh(t, c, "sha256sum ~/.codex/stub-goals.json"); after != before {
			t.Fatal("reading changed the Codex goal state")
		}
		testenv.Sh(t, c, "touch ~/.hostbud-stubs/missing-codex")
		if _, err := codex.ReadGoalState(ctx, sshx.HostMachineID, Binding{SessionID: "t"}, store.Run{}, "x"); err == nil || err.Error() != "codex not found on the host — install Codex first" {
			t.Fatalf("missing codex: %v", err)
		}
		if _, err := codex.CheckVersion(ctx, sshx.HostMachineID); err == nil || err.Error() != "codex not found on the host — install Codex first" {
			t.Fatalf("missing codex version: %v", err)
		}
		testenv.Sh(t, c, "rm -f ~/.hostbud-stubs/missing-codex; touch ~/.hostbud-stubs/old-version")
		if _, err := codex.CheckVersion(ctx, sshx.HostMachineID); err == nil || !strings.Contains(err.Error(), "found 0.150.0") {
			t.Fatalf("old codex: %v", err)
		}
		testenv.Sh(t, c, "rm -f ~/.hostbud-stubs/old-version")
	})

	// Every stub logged its argv with the token only as a hash.
	t.Run("argv logs", func(t *testing.T) {
		logs := testenv.Sh(t, c, "cat ~/.hostbud-stubs/log/*.json")
		if strings.Contains(logs, stubToken) || !strings.Contains(logs, "HOSTBUD_RUN_TOKEN_sha256") || strings.Contains(logs, "notify") {
			t.Fatalf("stub logs: %s", logs)
		}
	})
}
