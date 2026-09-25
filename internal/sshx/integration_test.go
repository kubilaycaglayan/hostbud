//go:build integration

package sshx_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hostbud/internal/sshx"
	"hostbud/internal/testenv"
)

func TestIntegrationExecEcho(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	out, err := c.Exec(context.Background(), sshx.HostMachineID, "echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello\n" {
		t.Fatalf("got %q", out)
	}
}

func TestIntegrationQuotingRoundTrip(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	hostile := []string{"it's", "$HOME", "a  b", "x\ny", "`id`", "$(id)", ";rm -rf /", "*", "'\"", ""}
	args := append([]string{"printf", `%s\0`}, hostile...)
	out, err := c.Exec(context.Background(), sshx.HostMachineID, args...)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(got) != len(hostile) {
		t.Fatalf("got %q", got)
	}
	for i := range hostile {
		if got[i] != hostile[i] {
			t.Errorf("arg %d: got %q, want %q", i, got[i], hostile[i])
		}
	}
}

func TestIntegrationControlMasterReuse(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	ctx := context.Background()
	if _, err := c.Exec(ctx, sshx.HostMachineID, "true"); err != nil {
		t.Fatal(err)
	}
	check := exec.CommandContext(ctx, "ssh", "-F", c.ConfigPath(), "-O", "check", sshx.HostAlias)
	if out, err := check.CombinedOutput(); err != nil || !strings.Contains(string(out), "Master running") {
		t.Fatalf("no ControlMaster after first Exec: %v %s", err, out)
	}
	// Later calls go through the master: the remote sees the same sshd
	// connection (same SSH_CONNECTION client port) every time.
	first, err := c.Exec(ctx, sshx.HostMachineID, "sh", "-c", "echo $SSH_CONNECTION")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		again, err := c.Exec(ctx, sshx.HostMachineID, "sh", "-c", "echo $SSH_CONNECTION")
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatalf("new connection instead of the master: %q vs %q", again, first)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(filepath.Dir(c.ConfigPath()), "cm"))
	if len(entries) != 1 {
		t.Fatalf("want one control socket, got %d", len(entries))
	}
}

func TestIntegrationPinnedKeyMismatchRefused(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{HostKeysDir: testenv.WrongHostKeys(t)})
	_, err := c.Exec(context.Background(), sshx.HostMachineID, "true")
	if !sshx.IsKind(err, sshx.KindHostKey) {
		t.Fatalf("got %v; want a host-key error", err)
	}
}

func TestIntegrationConnectionRefused(t *testing.T) {
	// Nothing listens on 2222 in the target: what a stopped sshd looks like.
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{Port: 2222})
	_, err := c.Exec(context.Background(), sshx.HostMachineID, "true")
	if !sshx.IsKind(err, sshx.KindUnreachable) {
		t.Fatalf("got %v", err)
	}
	var e *sshx.Error
	if !errors.As(err, &e) || !strings.Contains(e.Hint, "sshd") {
		t.Fatalf("hint: %+v", e)
	}
}

func TestIntegrationUnresolvableHost(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, "hostbud-test-no-such-host", testenv.Options{})
	_, err := c.Exec(context.Background(), sshx.HostMachineID, "true")
	if !sshx.IsKind(err, sshx.KindUnreachable) {
		t.Fatalf("got %v", err)
	}
}

func TestIntegrationAgentMissing(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "gone.sock"))
	c := testenv.Client(t, testenv.SSHD, testenv.Options{})
	_, err := c.Exec(context.Background(), sshx.HostMachineID, "true")
	var e *sshx.Error
	if !errors.As(err, &e) || e.Kind != sshx.KindAgent || !strings.Contains(e.Hint, "HOST_SSH_AUTH_SOCK") {
		t.Fatalf("got %+v", err)
	}
}

func TestIntegrationAgentEmpty(t *testing.T) {
	testenv.Agent(t, false)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{})
	_, err := c.Exec(context.Background(), sshx.HostMachineID, "true")
	var e *sshx.Error
	if !errors.As(err, &e) || e.Kind != sshx.KindAgent || !strings.Contains(e.Hint, "ssh-add") {
		t.Fatalf("got %+v", err)
	}
}

func TestIntegrationRemoteExitCode(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	_, err := c.Exec(context.Background(), sshx.HostMachineID, "sh", "-c", "echo oops >&2; exit 3")
	var e *sshx.Error
	if !errors.As(err, &e) || e.Kind != sshx.KindRemote || e.ExitCode != 3 || !strings.Contains(e.Stderr, "oops") {
		t.Fatalf("got %+v", err)
	}
}

func TestIntegrationTimeoutKillsRemoteCall(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{Timeout: 500 * time.Millisecond})
	start := time.Now()
	_, err := c.Exec(context.Background(), sshx.HostMachineID, "sleep", "30")
	if !sshx.IsKind(err, sshx.KindTimeout) || time.Since(start) > 5*time.Second {
		t.Fatalf("got %v after %v", err, time.Since(start))
	}
}
