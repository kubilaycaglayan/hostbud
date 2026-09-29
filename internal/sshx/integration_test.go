//go:build integration

package sshx_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
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

func TestIntegrationConcurrentExecsShareControlMaster(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	const count = 8
	results := make(chan string, count)
	errs := make(chan error, count)
	for i := range count {
		go func(i int) {
			want := strconv.Itoa(i)
			out, err := c.Exec(context.Background(), sshx.HostMachineID, "printf", "%s", want)
			if err != nil {
				errs <- err
				return
			}
			results <- string(out)
		}(i)
	}
	got := make(map[string]int, count)
	for range count {
		select {
		case err := <-errs:
			t.Fatal(err)
		case value := <-results:
			got[value]++
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent ssh exec timed out")
		}
	}
	for i := range count {
		if got[strconv.Itoa(i)] != 1 {
			t.Errorf("result %q appeared %d times", strconv.Itoa(i), got[strconv.Itoa(i)])
		}
	}
}

// More open channels than sshd's MaxSessions (default 10) must not push
// short commands off the command master: they would each pay a handshake.
func TestIntegrationLongLivedChannelsKeepCommandMasterFree(t *testing.T) {
	c := testenv.Connected(t, testenv.SSHD)
	conn := func() string {
		t.Helper()
		out, err := c.Exec(context.Background(), sshx.HostMachineID, "printenv", "SSH_CONNECTION")
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	before := conn()
	for range 12 {
		stream, err := c.Stream(context.Background(), sshx.HostMachineID, "cat")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = stream.Close() })
		// A round trip proves the channel is open before the next one.
		if _, err := stream.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(stream, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
	}
	if after := conn(); after != before {
		t.Fatalf("exec left the command master: connection %q, then %q", before, after)
	}
}

func TestIntegrationPinnedKeyMismatchRefused(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{HostKeysDir: testenv.WrongHostKeys(t)})
	_, err := c.Exec(context.Background(), sshx.HostMachineID, "true")
	if !sshx.IsKind(err, sshx.KindHostKey) {
		t.Fatalf("got %v; want a host-key error", err)
	}
	var detail *sshx.Error
	if !errors.As(err, &detail) || detail.Message != "The host's SSH key changed." ||
		!strings.Contains(detail.Hint, "If you expected this") {
		t.Fatalf("host-key error is not actionable: %+v", err)
	}
	entries, readErr := os.ReadDir(filepath.Join(filepath.Dir(c.ConfigPath()), "cm"))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("host-key mismatch created a ControlMaster socket: %v", entries)
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

func TestIntegrationTmuxStallTimesOutAndRecovers(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{Timeout: 500 * time.Millisecond})
	deadline := time.Now().Add(5 * time.Second).Unix()
	testenv.Sh(t, c, "mkdir -p /home/dev/.hostbud-stall && printf '%d\\n' "+strconv.FormatInt(deadline, 10)+" >/home/dev/.hostbud-stall/tmux")
	t.Cleanup(func() {
		_, _ = c.Exec(context.Background(), sshx.HostMachineID, "rm", "-f", "/home/dev/.hostbud-stall/tmux")
	})
	start := time.Now()
	_, err := c.Exec(context.Background(), sshx.HostMachineID, "tmux", "-V")
	if !sshx.IsKind(err, sshx.KindTimeout) || time.Since(start) > 2500*time.Millisecond {
		t.Fatalf("stalled tmux = %v after %v", err, time.Since(start))
	}
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		argv, readErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if readErr != nil {
			continue
		}
		parts := strings.Split(string(argv), "\x00")
		for i := 0; i+1 < len(parts); i++ {
			if parts[i] == "-F" && parts[i+1] == c.ConfigPath() {
				t.Fatalf("ssh child survived timeout: pid=%s argv=%q", entry.Name(), parts)
			}
		}
	}
	testenv.Sh(t, c, "rm -f /home/dev/.hostbud-stall/tmux")
	if _, err := c.Exec(context.Background(), sshx.HostMachineID, "tmux", "-V"); err != nil {
		t.Fatalf("tmux after stall: %v", err)
	}
}

func TestIntegrationEveryTmuxCommandTimesOut(t *testing.T) {
	testenv.Agent(t, true)
	setup := testenv.Client(t, testenv.SSHD, testenv.Options{Timeout: 5 * time.Second})
	testenv.Sh(t, setup, "tmux new-session -d -s timeout-it -c /home/dev 2>/dev/null || true")
	testenv.Sh(t, setup, "mkdir -p /home/dev/.hostbud-stall && printf '%d\\n' "+strconv.FormatInt(time.Now().Add(60*time.Second).Unix(), 10)+" >/home/dev/.hostbud-stall/tmux")
	c := testenv.Client(t, testenv.SSHD, testenv.Options{Timeout: 250 * time.Millisecond})
	t.Cleanup(func() {
		testenv.Sh(t, setup, "rm -f /home/dev/.hostbud-stall/tmux; tmux kill-session -t =timeout-it 2>/dev/null || true")
	})
	commands := [][]string{
		{"tmux", "list-sessions"},
		{"tmux", "new-session", "-d", "-s", "timeout-new", "-c", "/home/dev"},
		{"tmux", "rename-session", "-t", "=timeout-it", "timeout-renamed"},
		{"tmux", "kill-session", "-t", "=timeout-it"},
		{"tmux", "copy-mode", "-e", "-u", "-t", "=timeout-it:"},
		{"tmux", "list-windows", "-t", "=timeout-it"},
		{"tmux", "select-window", "-t", "=timeout-it:@1"},
	}
	for _, args := range commands {
		start := time.Now()
		_, err := c.Exec(context.Background(), sshx.HostMachineID, args...)
		if !sshx.IsKind(err, sshx.KindTimeout) || time.Since(start) > 2*time.Second {
			t.Fatalf("%q: got %v after %v", args, err, time.Since(start))
		}
		if _, err := c.Exec(context.Background(), sshx.HostMachineID, "true"); err != nil {
			t.Fatalf("success reset after %q: %v", args, err)
		}
	}
}

func TestIntegrationStoppedControlMasterRecovers(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{Timeout: 500 * time.Millisecond})
	if _, err := c.Exec(context.Background(), sshx.HostMachineID, "true"); err != nil {
		t.Fatal(err)
	}
	check := exec.CommandContext(context.Background(), "ssh", "-F", c.ConfigPath(), "-O", "check", sshx.HostAlias)
	out, err := check.CombinedOutput()
	if err != nil {
		t.Fatalf("check master: %v %s", err, out)
	}
	match := regexp.MustCompile(`pid=([0-9]+)`).FindSubmatch(out)
	var pid int
	if len(match) == 2 {
		pid, _ = strconv.Atoi(string(match[1]))
	}
	if pid <= 1 {
		t.Fatalf("missing master pid in %q", out)
	}
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(pid, syscall.SIGCONT) }()
	for range 2 {
		if _, err := c.Exec(context.Background(), sshx.HostMachineID, "true"); !sshx.IsKind(err, sshx.KindTimeout) {
			t.Fatalf("timeout = %v", err)
		}
	}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.Exec(context.Background(), sshx.HostMachineID, "true"); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	check = exec.CommandContext(context.Background(), "ps", "-ef")
	ps, _ := check.CombinedOutput()
	entries, _ := os.ReadDir(filepath.Join(filepath.Dir(c.ConfigPath()), "cm"))
	t.Fatalf("ControlMaster did not recover; sockets=%v; config=%s; ps=%s", entries, c.ConfigPath(), ps)
}
