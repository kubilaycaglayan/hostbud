//go:build integration

package queue

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/testenv"
)

// itEnv is a queue integration environment: the test/sshd target with a
// fresh tmux server, the real session service, and an isolated store schema.
type itEnv struct {
	c        *sshx.Client
	st       *store.Store
	inv      *inventory.Inventory
	sessions *session.Service
	bus      *events.Bus
	project  store.Project
	queue    store.Queue
}

const itProjectPath = "/home/dev/runs-it/app"

func newITEnv(t *testing.T) *itEnv {
	t.Helper()
	ctx := context.Background()
	c := testenv.Connected(t, testenv.SSHD)
	testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -rf ~/runs-it; mkdir -p "+itProjectPath)
	bus := events.NewBus()
	inv := inventory.New(c, bus, inventory.Options{MachineID: sshx.HostMachineID, Interval: 500 * time.Millisecond})
	invCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { inv.Run(invCtx); close(done) }()
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-password" //nolint:gosec // disposable integration database
	}
	schema := fmt.Sprintf("queue_it_%x", sha256.Sum256([]byte(t.TempDir())))[:24]
	st, err := store.Open(ctx, store.Config{Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: password, SSLMode: "disable", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -rf ~/runs-it")
		cancel()
		<-done
		_ = st.Close()
	})
	rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
	defer rcancel()
	if err := inv.Refresh(rctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	p, err := st.CreateProject(ctx, store.HostMachineID, itProjectPath, "app")
	if err != nil {
		t.Fatal(err)
	}
	q, err := st.CreateQueue(ctx, p.ID, "Milestones")
	if err != nil {
		t.Fatal(err)
	}
	return &itEnv{c: c, st: st, inv: inv, sessions: session.New(c, session.Trackers{sshx.HostMachineID: inv}, nil), bus: bus, project: p, queue: q}
}

func (e *itEnv) display(t *testing.T, name, format string) string {
	t.Helper()
	out, err := e.c.Exec(context.Background(), sshx.HostMachineID, "tmux", "display-message", "-p", "-t", "="+name+":", format)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func (e *itEnv) waitPane(t *testing.T, name, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := e.c.Exec(context.Background(), sshx.HostMachineID, "tmux", "capture-pane", "-p", "-J", "-t", "="+name+":")
		if err == nil && strings.Contains(string(out), want) {
			return string(out)
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %q never showed %q: %q %v", name, want, out, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// fixtureClientConfigs writes stand-in client config files on the target
// and returns their checksum, to prove runs never change them.
func fixtureClientConfigs(t *testing.T, c *sshx.Client) func() string {
	t.Helper()
	testenv.Sh(t, c, `mkdir -p ~/.claude ~/.codex && printf '{"hooks":{"Stop":[]}}\n' > ~/.claude/settings.json && printf 'model = "x"\n' > ~/.codex/config.toml && printf '{"hooks":{}}\n' > ~/.codex/hooks.json`)
	return func() string {
		return testenv.Sh(t, c, "sha256sum ~/.claude/settings.json ~/.codex/config.toml ~/.codex/hooks.json")
	}
}

// procCmdlines returns the command lines of local processes (the ssh
// client side).
func procCmdlines() string {
	paths, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	var b strings.Builder
	for _, p := range paths {
		data, _ := os.ReadFile(p) //nolint:gosec // /proc/<pid>/cmdline from a fixed glob
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

// V2-M1 T4: a run's session is created through the single session service
// in the project path, named <project>-q<pos> (collision-suffixed), with
// the three HOSTBUD_* variables that its first process sees. The token
// never shows in a process list on either side, and client configs don't
// change.
func TestIntegrationRunSessionCreation(t *testing.T) {
	e := newITEnv(t)
	ctx := context.Background()
	checksum := fixtureClientConfigs(t, e.c)
	before := checksum()
	item, err := e.st.AddQueueItem(ctx, e.queue.ID, "claude", "", "/goal ship")
	if err != nil {
		t.Fatal(err)
	}
	// A session with the run's name exists already: the run gets app-q1-1.
	testenv.Sh(t, e.c, "tmux new-session -d -s app-q1 -c /home/dev")
	if err := e.inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	agent := &fakeAgent{version: "2.1.283", argv: []string{"sh", "-c", `echo "ENV[$HOSTBUD_URL|$HOSTBUD_RUN_ID|${#HOSTBUD_RUN_TOKEN}]"; exec sleep 300`}}
	starter := NewStarter(e.st, e.sessions, "http://hostbud-e2e-caddy:9055", nil)

	// Sample both process lists while the session is created.
	var wg sync.WaitGroup
	var local strings.Builder
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				local.WriteString(procCmdlines())
			}
		}
	}()
	var remote []byte
	go func() {
		defer wg.Done()
		remote, _ = e.c.Exec(ctx, sshx.HostMachineID, "sh", "-c", `end=$(($(date +%s)+3)); while [ "$(date +%s)" -lt "$end" ]; do ps -eo args; done`)
	}()
	time.Sleep(300 * time.Millisecond)
	run, err := starter.Start(ctx, store.SourceUser, e.project, "", item, agent)
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
	if err != nil || run.Status != store.RunStarting || run.SessionName != "app-q1-1" {
		t.Fatalf("Start = %+v, %v", run, err)
	}

	out, err := e.c.Exec(ctx, sshx.HostMachineID, "tmux", "show-environment", "-t", "="+run.SessionName)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && strings.HasPrefix(k, "HOSTBUD_") {
			vars[k] = v
		}
	}
	if len(vars) != 3 || vars[EnvURL] != "http://hostbud-e2e-caddy:9055" || vars[EnvRunID] != run.ID || !TokenMatches(vars[EnvToken], run.TokenHash) {
		t.Fatalf("session env %v", vars)
	}
	e.waitPane(t, run.SessionName, fmt.Sprintf("ENV[http://hostbud-e2e-caddy:9055|%s|43]", run.ID))
	if p := e.display(t, run.SessionName, "#{pane_current_path}"); p != itProjectPath {
		t.Fatalf("pane path %q", p)
	}
	token := vars[EnvToken]
	if !strings.Contains(string(remote), "sshd") || !strings.Contains(local.String(), "ssh") {
		t.Fatalf("the samplers saw nothing (remote %d bytes, local %d bytes)", len(remote), local.Len())
	}
	if strings.Contains(string(remote), token) || strings.Contains(local.String(), token) {
		t.Fatal("the run token showed in a process list")
	}
	if after := checksum(); after != before {
		t.Fatalf("client configs changed:\n%s\n%s", before, after)
	}
}

// V2-M2 T3: runs created at the same moment whose session names collide
// (a second queue "Docs" on project app, and the first queue of a project
// named app-Docs, both at position 1) get distinct sessions: the session
// service retries the next suffix when tmux reports a duplicate.
func TestIntegrationConcurrentRunSessionsGetDistinctNames(t *testing.T) {
	e := newITEnv(t)
	ctx := context.Background()
	testenv.Sh(t, e.c, "mkdir -p ~/runs-it/app-docs")
	other, err := e.st.CreateProject(ctx, store.HostMachineID, "/home/dev/runs-it/app-docs", "app-Docs")
	if err != nil {
		t.Fatal(err)
	}
	docs, err := e.st.CreateQueue(ctx, e.project.ID, "Docs")
	if err != nil {
		t.Fatal(err)
	}
	otherQueue, err := e.st.CreateQueue(ctx, other.ID, "Queue")
	if err != nil {
		t.Fatal(err)
	}
	type start struct {
		project store.Project
		label   string
		queue   store.Queue
	}
	starts := []start{{e.project, "Docs", docs}, {other, "", otherQueue}, {e.project, "Docs", docs}, {other, "", otherQueue}}
	items := make([]store.QueueItem, len(starts))
	for i, s := range starts {
		if items[i], err = e.st.AddQueueItem(ctx, s.queue.ID, "claude", "", fmt.Sprintf("/goal concurrent %d", i)); err != nil {
			t.Fatal(err)
		}
		items[i].Position = 1 // every run asks for <...>-q1
	}
	if got := RunSessionName(e.project.Name, "Docs", 1); got != RunSessionName(other.Name, "", 1) {
		t.Fatalf("the fixture names don't collide: %q", got)
	}
	agent := &fakeAgent{version: "2.1.283", argv: []string{"sleep", "300"}}
	starter := NewStarter(e.st, e.sessions, "http://hostbud-e2e-caddy:9055", nil)
	names := make([]string, len(starts))
	errs := make([]error, len(starts))
	var wg sync.WaitGroup
	for i, s := range starts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := starter.Start(ctx, store.SourceUser, s.project, s.label, items[i], agent)
			names[i], errs[i] = run.SessionName, err
			if err == nil && run.Status != store.RunStarting {
				errs[i] = fmt.Errorf("run %s: %s", run.Status, run.Detail)
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, name := range names {
		if errs[i] != nil {
			t.Fatalf("start %d: %v", i, errs[i])
		}
		if !strings.HasPrefix(name, "app-Docs-q1") || seen[name] {
			t.Fatalf("session names %v are not distinct app-Docs-q1[-n]", names)
		}
		seen[name] = true
	}
	out := testenv.Sh(t, e.c, "tmux list-sessions -F '#{session_name}'")
	for name := range seen {
		if !strings.Contains("\n"+out+"\n", "\n"+name+"\n") {
			t.Errorf("session %q missing from tmux: %q", name, out)
		}
	}
}
