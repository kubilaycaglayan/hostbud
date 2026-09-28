//go:build integration

package queue

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/agents"
	"hostbud/internal/events"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/testenv"
)

// V2-M4 T2: the verify runner against test/sshd.

const verifyDir = "/home/dev/verify-it/app"

// recordingExec records every sshx call of the verifier.
type recordingExec struct {
	c     *sshx.Client
	mu    sync.Mutex
	calls [][]string
}

func (r *recordingExec) ExecTo(ctx context.Context, machine string, w io.Writer, args ...string) error {
	r.mu.Lock()
	r.calls = append(r.calls, args)
	r.mu.Unlock()
	return r.c.ExecTo(ctx, machine, w, args...)
}

func verifyTarget(t *testing.T) *sshx.Client {
	t.Helper()
	c := testenv.Connected(t, testenv.SSHD)
	testenv.Sh(t, c, "rm -rf ~/verify-it && mkdir -p "+verifyDir)
	t.Cleanup(func() { testenv.Sh(t, c, "rm -rf ~/verify-it") })
	return c
}

func TestIntegrationVerifyRunsInTheProjectDirectory(t *testing.T) {
	c := verifyTarget(t)
	rec := &recordingExec{c: c}
	v := NewVerifier(rec, 30*time.Second)
	ctx := context.Background()
	res, ok := v.Run(ctx, sshx.HostMachineID, verifyDir, "pwd")
	if !ok || !res.Passed() || res.Output != verifyDir+"\n" {
		t.Fatalf("pwd: %+v", res)
	}
	res, _ = v.Run(ctx, sshx.HostMachineID, verifyDir, `sh -c 'echo out; echo err >&2; exit 3'`)
	if res.Outcome != VerifyFailed || *res.ExitCode != 3 || res.Output != "out\nerr\n" || res.Detail != "verify failed (exit 3)" {
		t.Fatalf("exit 3 with combined output: %+v", res)
	}
	for _, call := range rec.calls {
		if slices.ContainsFunc(call, func(a string) bool { return strings.Contains(a, "tmux") }) {
			t.Fatalf("verify called tmux: %q", call)
		}
	}
}

func TestIntegrationVerifyKeepsShellSyntaxLiteral(t *testing.T) {
	c := verifyTarget(t)
	v := NewVerifier(c, 30*time.Second)
	// Every word is one literal argument of touch: "&&" and "touch" become
	// file names too, and nothing is expanded or run by a shell.
	res, ok := v.Run(context.Background(), sshx.HostMachineID, verifyDir, `touch 'x; touch pwned' '$(touch pwned2)' && touch pwned3`)
	if !ok || !res.Passed() {
		t.Fatalf("result %+v", res)
	}
	listing := strings.Split(strings.TrimSpace(testenv.Sh(t, c, "cd "+verifyDir+" && ls -1A")), "\n")
	slices.Sort(listing)
	want := []string{"$(touch pwned2)", "&&", "pwned3", "touch", "x; touch pwned"}
	if !slices.Equal(listing, want) {
		t.Fatalf("files %q, want %q", listing, want)
	}
}

func TestIntegrationVerifyTimeoutLeavesNoProcess(t *testing.T) {
	c := verifyTarget(t)
	v := NewVerifier(c, 2*time.Second)
	start := time.Now()
	res, ok := v.Run(context.Background(), sshx.HostMachineID, verifyDir, `sh -c 'echo started; sleep 31.5'`)
	if !ok || res.Outcome != VerifyTimedOut || res.Detail != "verify timed out after 2s" || res.Output != "started\n" {
		t.Fatalf("result %+v", res)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("timeout took %s", took)
	}
	if out := testenv.Sh(t, c, "pgrep -f 'sleep 3[1].5' || true"); strings.TrimSpace(out) != "" {
		t.Fatalf("the timed-out command is still running: %q", out)
	}
}

func TestIntegrationVerifyOutputTailAndMissingDirectory(t *testing.T) {
	c := verifyTarget(t)
	v := NewVerifier(c, 30*time.Second)
	ctx := context.Background()
	res, _ := v.Run(ctx, sshx.HostMachineID, verifyDir, "head -c 5000000 /dev/urandom")
	if !res.Passed() || !res.Truncated || len(res.Output) > VerifyTail || len(res.Output) < VerifyTail/2 {
		t.Fatalf("5 MB of binary: passed %v truncated %v, %d bytes", res.Passed(), res.Truncated, len(res.Output))
	}
	res, _ = v.Run(ctx, sshx.HostMachineID, verifyDir+"-gone", "true")
	if res.Outcome != VerifyMissingDir || res.ExitCode != nil {
		t.Fatalf("missing directory: %+v", res)
	}
}

func TestIntegrationVerifySSHFailure(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{Port: 2222}) // nothing listens: a stopped sshd
	res, ok := NewVerifier(c, 30*time.Second).Run(context.Background(), sshx.HostMachineID, verifyDir, "true")
	if !ok || res.Outcome != VerifySSHFailed || !strings.HasPrefix(res.Detail, "verify didn't run: ") || !strings.Contains(res.Detail, "sshd") {
		t.Fatalf("result %+v", res)
	}
}

// A restart during verify: the attempt was claimed before it ran, so the
// new process flags the item instead of running the command again.
func TestIntegrationRestartDuringVerifyRunsItOnce(t *testing.T) {
	c := verifyTarget(t)
	ctx := context.Background()
	e := newITEnv(t)
	p, err := e.st.CreateProject(ctx, store.HostMachineID, verifyDir, "verify")
	if err != nil {
		t.Fatal(err)
	}
	q, err := e.st.CreateQueue(ctx, p.ID, "Verify")
	if err != nil {
		t.Fatal(err)
	}
	it, err := e.st.AddQueueItem(ctx, q.ID, "claude", "", "/goal verify once", store.ItemGates{VerifyCommand: `sh -c 'echo x >> count; sleep 3'`})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := e.st.CreateRun(ctx, it.ID, HashToken("verify-it"), time.Now())
	ended := time.Now()
	run, _ = e.st.TransitionRun(ctx, run.ID, store.ActiveRunStatuses, store.RunAchieved, "", &ended)
	// Verifying without an attempt: recovery starts it (once).
	it, _ = e.st.TransitionQueueItem(ctx, it.ID, []string{store.ItemQueued}, store.ItemVerifying)
	_, _ = e.st.TransitionQueue(ctx, q.ID, []string{store.QueueIdle}, store.QueueRunning)

	newDispatcher := func() (*Dispatcher, context.CancelFunc, chan struct{}) {
		bus := events.NewBus()
		svc := NewService(e.st, agents.NewRegistry(agents.NewClaude(nil, nil, nil), agents.NewCodex(nil, 0)), bus)
		d := NewDispatcher(e.st, adapterMap{}, NewStarter(e.st, e.sessions, "", nil), svc, bus, time.Hour, nil)
		d.SetVerifier(NewVerifier(c, 30*time.Second))
		dctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { d.Run(dctx); close(done) }()
		d.Sync()
		return d, cancel, done
	}
	_, cancel, done := newDispatcher()
	deadline := time.Now().Add(10 * time.Second)
	for strings.TrimSpace(testenv.Sh(t, c, "cat "+verifyDir+"/count 2>/dev/null || true")) == "" {
		if time.Now().After(deadline) {
			t.Fatal("verify never started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel() // hostbud stops mid-verify
	<-done
	if got, _ := e.st.QueueItem(ctx, it.ID); got.Status != store.ItemVerifying {
		t.Fatalf("after the stop: %s", got.Status)
	}
	d, cancel, done := newDispatcher()
	defer func() { cancel(); <-done }()
	d.Sync()
	got, _ := e.st.QueueItem(ctx, it.ID)
	r, _ := e.st.Run(ctx, run.ID)
	qq, _ := e.st.Queue(ctx, q.ID)
	if got.Status != store.ItemNeedsAttention || r.Detail != "hostbud restarted during verify — Re-run verify" || qq.Status != store.QueuePaused {
		t.Fatalf("after the restart: item %s, detail %q, queue %s", got.Status, r.Detail, qq.Status)
	}
	time.Sleep(3500 * time.Millisecond)
	if n := strings.Count(testenv.Sh(t, c, "cat "+verifyDir+"/count"), "x"); n != 1 {
		t.Fatalf("verify ran %d times, want 1", n)
	}
}
