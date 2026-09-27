//go:build integration

package session_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/testenv"
	"hostbud/internal/tmux"
)

func setup(t *testing.T) (*session.Service, *sshx.Client) {
	t.Helper()
	c := testenv.Connected(t, testenv.SSHD)
	testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -rf ~/sess-it; mkdir -p ~/sess-it/proj.one")
	inv := inventory.New(c, events.NewBus(), inventory.Options{MachineID: sshx.HostMachineID, Interval: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { inv.Run(ctx); close(done) }()
	t.Cleanup(func() {
		testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -rf ~/sess-it")
		cancel()
		<-done
	})
	rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rcancel()
	if err := inv.Refresh(rctx); err != nil {
		t.Fatal(err)
	}
	return session.New(c, map[string]session.Tracker{sshx.HostMachineID: inv}, nil), c
}

func display(t *testing.T, c *sshx.Client, name, format string) string {
	t.Helper()
	out, err := c.Exec(context.Background(), sshx.HostMachineID, "tmux", "display-message", "-p", "-t", "="+name+":", format)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestIntegrationCreateWithDefaults(t *testing.T) {
	svc, c := setup(t)
	name, err := svc.Create(context.Background(), session.Spec{Machine: sshx.HostMachineID, Path: "~/sess-it/proj.one"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "proj-one" {
		t.Fatalf("name = %q", name)
	}
	if p := display(t, c, name, "#{session_path}"); p != "/home/dev/sess-it/proj.one" {
		t.Fatalf("session_path = %q", p)
	}
	// Same directory again: numbered from -1.
	again, err := svc.Create(context.Background(), session.Spec{Machine: sshx.HostMachineID, Path: "~/sess-it/proj.one"})
	if err != nil || again != "proj-one-1" {
		t.Fatalf("second create: %q, %v", again, err)
	}
}

func TestIntegrationSessionNameBoundariesAndUnicodePath(t *testing.T) {
	svc, c := setup(t)
	ctx := context.Background()
	unicodePath := "/home/dev/sess-it/space λ"
	if _, err := c.Exec(ctx, sshx.HostMachineID, "mkdir", "-p", unicodePath); err != nil {
		t.Fatal(err)
	}
	short, err := svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "a", Path: unicodePath})
	if err != nil || short != "a" {
		t.Fatalf("one-character session name = %q, %v", short, err)
	}
	longName := strings.Repeat("x", 64)
	long, err := svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: longName, Path: "/home/dev/sess-it"})
	if err != nil || long != longName {
		t.Fatalf("64-character session name = %q, %v", long, err)
	}
	if got := display(t, c, long, "#{session_path}"); got != "/home/dev/sess-it" {
		t.Fatalf("session path = %q, want /home/dev/sess-it", got)
	}
}

type killBeforeRename struct {
	inner inventory.Executor
	name  string
}

func (e killBeforeRename) Exec(ctx context.Context, machine string, args ...string) ([]byte, error) {
	if len(args) > 1 && args[0] == "tmux" && args[1] == "rename-session" {
		if _, err := e.inner.Exec(ctx, machine, "tmux", "kill-session", "-t", "="+e.name); err != nil {
			return nil, err
		}
	}
	return e.inner.Exec(ctx, machine, args...)
}

func TestIntegrationRenameRacesKilledSession(t *testing.T) {
	_, c := setup(t)
	const name = "rename-during-kill"
	if _, err := c.Exec(context.Background(), sshx.HostMachineID, "tmux", "new-session", "-d", "-s", name, "-c", "/home/dev"); err != nil {
		t.Fatal(err)
	}
	service := session.New(killBeforeRename{inner: c, name: name}, map[string]session.Tracker{sshx.HostMachineID: &fakeTrackerForRename{c: c}}, nil)
	err := service.Rename(context.Background(), sshx.HostMachineID, name, "renamed-after-kill")
	var serviceErr *session.Error
	if !errors.As(err, &serviceErr) || serviceErr.Code != session.CodeNotFound {
		t.Fatalf("rename during kill = %v; want not-found", err)
	}
}

type fakeTrackerForRename struct{ c *sshx.Client }

func (fakeTrackerForRename) Snapshot() (inventory.Machine, []tmux.Session) {
	return inventory.Machine{ID: sshx.HostMachineID, Status: inventory.StatusOK,
			Capabilities: inventory.Capabilities{Home: "/home/dev", TmuxVersion: "3.4"}},
		[]tmux.Session{{Name: "rename-during-kill"}}
}
func (fakeTrackerForRename) Refresh(context.Context) error { return nil }

func TestIntegrationCreateWithStartCommand(t *testing.T) {
	svc, c := setup(t)
	name, err := svc.Create(context.Background(), session.Spec{
		Machine: sshx.HostMachineID, Name: "sess-htop", Path: "~/sess-it", StartCommand: "htop",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for display(t, c, name, "#{pane_current_command}") != "htop" {
		if time.Now().After(deadline) {
			t.Fatalf("pane runs %q, want htop", display(t, c, name, "#{pane_current_command}"))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if p := display(t, c, name, "#{pane_current_path}"); p != "/home/dev/sess-it" {
		t.Fatalf("pane path = %q", p)
	}
}

func TestIntegrationCopyModeActions(t *testing.T) {
	svc, c := setup(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "scroll-it", Path: "~/sess-it"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "send-keys", "-t", "=scroll-it:", "seq 1 300", "Enter"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for display(t, c, "scroll-it", "#{history_size}") == "0" {
		if time.Now().After(deadline) {
			t.Fatal("history did not fill")
		}
		time.Sleep(50 * time.Millisecond)
	}
	state, err := svc.CopyMode(ctx, sshx.HostMachineID, "scroll-it", "enter", 0)
	if err != nil || !state.InMode || state.ScrollPosition == 0 {
		t.Fatalf("enter: %+v %v", state, err)
	}
	prior := state.ScrollPosition
	state, err = svc.CopyMode(ctx, sshx.HostMachineID, "scroll-it", "page-up", 0)
	if err != nil || !state.InMode || state.ScrollPosition <= prior {
		t.Fatalf("page-up: %+v %v", state, err)
	}
	state, err = svc.CopyMode(ctx, sshx.HostMachineID, "scroll-it", "scroll-down", 500)
	if err != nil || state.InMode {
		t.Fatalf("scroll-down to bottom: %+v %v", state, err)
	}
	state, err = svc.CopyMode(ctx, sshx.HostMachineID, "scroll-it", "exit", 0)
	if err != nil || state.InMode {
		t.Fatalf("exit: %+v %v", state, err)
	}
	state, err = svc.CopyMode(ctx, sshx.HostMachineID, "scroll-it", "exit", 0)
	if err != nil || state.InMode {
		t.Fatalf("exit again: %+v %v", state, err)
	}
	if display(t, c, "scroll-it", "#{pane_in_mode}") != "0" {
		t.Fatal("pane remained in copy mode")
	}
	if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "send-keys", "-t", "=scroll-it:", "echo ready", "Enter"); err != nil {
		t.Fatalf("shell did not accept input: %v", err)
	}
}

func TestIntegrationCopyModeTmuxMissing(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHDNoTmux)
	inv := inventory.New(c, events.NewBus(), inventory.Options{MachineID: sshx.HostMachineID, Interval: time.Second})
	runCtx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { inv.Run(runCtx); close(done) }()
	t.Cleanup(func() { stop(); <-done })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := session.New(c, map[string]session.Tracker{sshx.HostMachineID: inv}, nil).CopyMode(ctx, sshx.HostMachineID, "work", "enter", 0)
	var e *session.Error
	if !errors.As(err, &e) || e.Code != session.CodeTmuxMissing || !strings.Contains(e.Hint, "apt install tmux") {
		t.Fatalf("missing tmux error = %+v", err)
	}
}

func TestIntegrationWindowsAndPanes(t *testing.T) {
	svc, c := setup(t)
	ctx := context.Background()
	name := "win-api"
	if _, err := svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: name, Path: "~/sess-it"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "split-window", "-d", "-t", "="+name+":0"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "send-keys", "-t", "="+name+":0.1", "vim", "Enter"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "display-message", "-p", "-t", "="+name+":0.1", "#{pane_current_command}")
		if err == nil && strings.TrimSpace(string(out)) == "vim" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("split pane did not start vim: %q %v", out, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "new-window", "-d", "-t", "="+name+":", "-n", "space λ"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "new-window", "-d", "-t", "="+name+":", "-n", "third"); err != nil {
		t.Fatal(err)
	}
	state, err := svc.ListWindows(ctx, sshx.HostMachineID, name)
	if err != nil {
		args, _ := tmux.ListWindowsArgs(name)
		out, execErr := c.Exec(ctx, sshx.HostMachineID, args...)
		t.Fatalf("list windows: %v; raw exec error=%v output=%q", err, execErr, string(out))
	}
	if state.Truncated || len(state.Windows) != 3 || len(state.Windows[0].Panes) != 2 || state.Windows[1].Name != "space λ" {
		t.Fatalf("windows = %+v", state)
	}
	for i, w := range state.Windows {
		if w.Index != i {
			t.Errorf("window order: index at %d = %d", i, w.Index)
		}
	}
	directWindows, err := c.Exec(ctx, sshx.HostMachineID, "env", "LC_ALL=C.UTF-8", "tmux", "list-windows", "-t", "="+name,
		"-F", "W\t#{window_id}\t#{window_index}\t#{window_active}\t#{window_panes}\t#{window_name}")
	if err != nil {
		t.Fatal(err)
	}
	directPanes, err := c.Exec(ctx, sshx.HostMachineID, "env", "LC_ALL=C.UTF-8", "tmux", "list-panes", "-s", "-t", "="+name,
		"-F", "P\t#{window_id}\t#{pane_id}\t#{pane_index}\t#{pane_active}\t#{pane_width}\t#{pane_height}\t#{pane_current_command}")
	if err != nil {
		t.Fatal(err)
	}
	directState, truncated, err := tmux.ParseWindows(string(directWindows) + string(directPanes))
	if err != nil || truncated || !reflect.DeepEqual(state.Windows, directState) {
		t.Fatalf("service listing differs from direct tmux output: state=%+v direct=%+v truncated=%v err=%v", state.Windows, directState, truncated, err)
	}
	if state.Windows[0].Panes[0].ID == state.Windows[0].Panes[1].ID {
		t.Fatal("split panes have duplicate ids")
	}
	if state.Windows[0].Panes[1].Command != "vim" {
		t.Fatalf("pane command = %q, want vim", state.Windows[0].Panes[1].Command)
	}
	if _, err := svc.ListWindows(ctx, sshx.HostMachineID, "missing"); err == nil {
		t.Fatal("unknown session returned a window listing")
	} else {
		var serviceErr *session.Error
		if !errors.As(err, &serviceErr) || serviceErr.Code != session.CodeNotFound {
			t.Fatalf("unknown session error: %v", err)
		}
	}
	if _, err := svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "win-other", Path: "~/sess-it"}); err != nil {
		t.Fatal(err)
	}
	foreign := display(t, c, "win-other", "#{pane_id}")
	selected, err := svc.SelectWindow(ctx, sshx.HostMachineID, name, state.Windows[1].ID, "")
	if err != nil || selected.Windows[1].Active == false || display(t, c, name, "#{window_index}") != "1" {
		t.Fatalf("select window: state=%+v err=%v active=%s", selected, err, display(t, c, name, "#{window_index}"))
	}
	pane := state.Windows[0].Panes[1]
	selected, err = svc.SelectWindow(ctx, sshx.HostMachineID, name, state.Windows[0].ID, pane.ID)
	if err != nil || display(t, c, name, "#{window_index} #{pane_index}") != "0 1" || !selected.Windows[0].Panes[1].Active {
		t.Fatalf("select pane: state=%+v err=%v", selected, err)
	}
	if _, err := svc.SelectWindow(ctx, sshx.HostMachineID, name, state.Windows[0].ID, foreign); err == nil {
		t.Fatal("foreign pane id was selected")
	} else {
		var serviceErr *session.Error
		if !errors.As(err, &serviceErr) || serviceErr.Code != session.CodeNotFound {
			t.Fatalf("foreign pane id: %v", err)
		}
	}
	if got := display(t, c, name, "#{window_index} #{pane_index}"); got != "0 1" {
		t.Fatalf("foreign pane selection changed target: %s", got)
	}
}

type killBeforeSelect struct {
	inner  inventory.Executor
	name   string
	killed bool
}

func (e *killBeforeSelect) Exec(ctx context.Context, machine string, args ...string) ([]byte, error) {
	if i := slices.Index(args, "tmux"); !e.killed && i >= 0 && len(args) > i+1 && args[i+1] == "select-window" {
		e.killed = true
		_, _ = e.inner.Exec(ctx, machine, "tmux", "kill-session", "-t", "="+e.name)
	}
	return e.inner.Exec(ctx, machine, args...)
}

func TestIntegrationSessionEndsBetweenListingAndSelect(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	testenv.Sh(t, c, "tmux kill-server 2>/dev/null || true")
	inv := inventory.New(c, events.NewBus(), inventory.Options{MachineID: sshx.HostMachineID, Interval: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { inv.Run(ctx); close(done) }()
	t.Cleanup(func() {
		testenv.Sh(t, c, "tmux kill-server 2>/dev/null || true")
		cancel()
		<-done
	})
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	name := "win-race"
	if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "new-session", "-d", "-s", name, "-c", "/home/dev"); err != nil {
		t.Fatal(err)
	}
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	exec := &killBeforeSelect{inner: c, name: name}
	svc := session.New(exec, map[string]session.Tracker{sshx.HostMachineID: inv}, nil)
	listed, err := svc.ListWindows(ctx, sshx.HostMachineID, name)
	if err != nil || len(listed.Windows) != 1 {
		t.Fatalf("pre-race listing = %+v %v", listed, err)
	}
	_, err = svc.SelectWindow(ctx, sshx.HostMachineID, name, listed.Windows[0].ID, "")
	var serviceErr *session.Error
	if !errors.As(err, &serviceErr) || serviceErr.Code != session.CodeNotFound {
		t.Fatalf("selection after session ended = %v", err)
	}
}

func TestIntegrationWindowsTmuxMissing(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHDNoTmux)
	inv := inventory.New(c, events.NewBus(), inventory.Options{MachineID: sshx.HostMachineID, Interval: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { inv.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := session.New(c, map[string]session.Tracker{sshx.HostMachineID: inv}, nil).ListWindows(ctx, sshx.HostMachineID, "work")
	var serviceErr *session.Error
	if !errors.As(err, &serviceErr) || serviceErr.Code != session.CodeTmuxMissing || !strings.Contains(serviceErr.Hint, "apt install tmux") {
		t.Fatalf("missing tmux error = %+v", err)
	}
}

func TestIntegrationErrorsMapped(t *testing.T) {
	svc, c := setup(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "dup"}); err != nil {
		t.Fatal(err)
	}
	var e *session.Error
	second, err := svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "dup"})
	if err != nil || second != "dup-1" {
		t.Fatalf("duplicate create = %q, %v; want dup-1", second, err)
	}
	if got := display(t, c, second, "#{session_name}"); got != second {
		t.Fatalf("numbered session name = %q; want %q", got, second)
	}
	_, err = svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "nopath", Path: "~/sess-it/nope"})
	if !errors.As(err, &e) || e.Code != session.CodePathNotFound || !strings.Contains(e.Message, "/home/dev/sess-it/nope") {
		t.Fatalf("missing path: %+v", err)
	}
	if err := svc.Rename(ctx, sshx.HostMachineID, "dup", "dup2"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Kill(ctx, sshx.HostMachineID, "dup"); !errors.As(err, &e) || e.Code != session.CodeNotFound {
		t.Fatalf("kill renamed-away session: %+v", err)
	}
	if err := svc.Kill(ctx, sshx.HostMachineID, "dup2"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Kill(ctx, sshx.HostMachineID, "dup-1"); err != nil {
		t.Fatal(err)
	}
}
