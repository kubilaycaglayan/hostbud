//go:build integration

package tmux_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"hostbud/internal/sshx"
	"hostbud/internal/testenv"
	"hostbud/internal/tmux"
)

func run(t *testing.T, c *sshx.Client, args []string) (string, error) {
	t.Helper()
	out, err := c.Exec(context.Background(), sshx.HostMachineID, args...)
	return string(out), err
}

func list(t *testing.T, c *sshx.Client) []tmux.Session {
	t.Helper()
	out, err := run(t, c, tmux.ListSessions())
	var e *sshx.Error
	if errors.As(err, &e) && e.Kind == sshx.KindRemote && tmux.NoServer(e.Stderr) {
		return nil
	}
	if err != nil {
		t.Fatalf("list-sessions: %v", err)
	}
	s, err := tmux.ParseSessions(out)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func find(ss []tmux.Session, name string) (tmux.Session, bool) {
	for _, s := range ss {
		if s.Name == name {
			return s, true
		}
	}
	return tmux.Session{}, false
}

func version(t *testing.T, c *sshx.Client) tmux.Version {
	t.Helper()
	out, err := run(t, c, tmux.VersionArgs())
	if err != nil {
		t.Fatal(err)
	}
	v, err := tmux.ParseVersion(out)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// resetServer kills the whole tmux server (throwaway target only).
func resetServer(t *testing.T, c *sshx.Client) {
	t.Helper()
	_, _ = run(t, c, []string{"tmux", "kill-server"})
}

func TestIntegrationListNoServerIsEmpty(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	resetServer(t, c)
	if got := list(t, c); len(got) != 0 {
		t.Fatalf("want empty list, got %+v", got)
	}
}

func TestIntegrationLifecycle(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	resetServer(t, c)
	t.Cleanup(func() { resetServer(t, c) })
	v := version(t, c)

	args, err := tmux.NewSessionArgs(tmux.NewSession{
		Name: "it-life", Path: "/home/dev", Env: map[string]string{"HOSTBUD_TEST": "x y"},
	}, v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, c, args); err != nil {
		t.Fatalf("new-session: %v", err)
	}

	s, ok := find(list(t, c), "it-life")
	if !ok || s.Path != "/home/dev" || s.Windows != 1 || s.Attached != 0 || s.ID == "" || s.Created.IsZero() {
		t.Fatalf("after create: %+v (found %v)", s, ok)
	}
	env, err := run(t, c, []string{"tmux", "show-environment", "-t", "=it-life", "HOSTBUD_TEST"})
	if err != nil || strings.TrimSpace(env) != "HOSTBUD_TEST=x y" {
		t.Fatalf("-e not applied: %q %v", env, err)
	}

	if _, err := run(t, c, []string{"tmux", "new-window", "-t", "=it-life:"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := find(list(t, c), "it-life"); s.Windows != 2 {
		t.Fatalf("windows = %d, want 2", s.Windows)
	}

	// A real client attached through a PTY (ssh -tt) shows as attached.
	detach := attachClient(t, c, "it-life")
	waitFor(t, "attached", func() bool { s, _ := find(list(t, c), "it-life"); return s.Attached == 1 })
	detach()
	waitFor(t, "detached", func() bool { s, _ := find(list(t, c), "it-life"); return s.Attached == 0 })

	args, _ = tmux.RenameSessionArgs("it-life", "it-renamed")
	if _, err := run(t, c, args); err != nil {
		t.Fatalf("rename: %v", err)
	}
	ss := list(t, c)
	if _, ok := find(ss, "it-life"); ok {
		t.Fatal("old name still listed")
	}
	if _, ok := find(ss, "it-renamed"); !ok {
		t.Fatal("new name not listed")
	}

	args, _ = tmux.KillSessionArgs("it-renamed")
	if _, err := run(t, c, args); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if _, ok := find(list(t, c), "it-renamed"); ok {
		t.Fatal("killed session still listed")
	}
}

func TestIntegrationExactTargets(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	resetServer(t, c)
	t.Cleanup(func() { resetServer(t, c) })
	v := version(t, c)
	args, _ := tmux.NewSessionArgs(tmux.NewSession{Name: "it-exact-long", Path: "/home/dev"}, v)
	if _, err := run(t, c, args); err != nil {
		t.Fatal(err)
	}
	// "=it-exact" must not match "it-exact-long" by prefix.
	args, _ = tmux.KillSessionArgs("it-exact")
	if _, err := run(t, c, args); err == nil {
		t.Fatal("kill of a prefix matched another session")
	}
	if _, ok := find(list(t, c), "it-exact-long"); !ok {
		t.Fatal("prefix kill removed the longer session")
	}
}

// attachClient runs `tmux attach` over ssh -tt (a real PTY on the target)
// and returns a func that ends it.
func attachClient(t *testing.T, c *sshx.Client, name string) func() {
	t.Helper()
	attach, _ := tmux.AttachArgs(name, tmux.Version{})
	argv, err := c.Args(sshx.HostMachineID, []string{"-tt"}, attach...)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), c.Binary(), argv...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	var stderr strings.Builder
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = io.Discard, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := false
	stop := func() {
		if done {
			return
		}
		done = true
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	t.Cleanup(func() {
		stop()
		if t.Failed() {
			t.Logf("attach client stderr: %s", stderr.String())
		}
	})
	return stop
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
