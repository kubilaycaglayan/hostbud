package sshx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"":            `''`,
		"plain":       `'plain'`,
		"with space":  `'with space'`,
		"it's":        `'it'\''s'`,
		"$HOME `x`":   "'$HOME `x`'",
		"a\nb":        "'a\nb'",
		`"dq" \ ;|&*`: `'"dq" \ ;|&*'`,
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}

// Round trip through a real shell: every hostile argument arrives verbatim.
func TestCommandRoundTripsThroughShell(t *testing.T) {
	args := []string{"printf", `%s\0`, "it's", "$HOME", "a b", "x\ny", "`id`", ";", "'", ""}
	out := runShell(t, Command(args...))
	got := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	want := args[2:]
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func writeKeys(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const (
	edPub    = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample root@server-a\n"
	ecdsaPub = "ecdsa-sha2-nistp256 AAAAE2VjZHNhExample root@server-a\n"
)

func TestKnownHostsFromPubFilesOnly(t *testing.T) {
	dir := writeKeys(t, map[string]string{
		"ssh_host_ed25519_key.pub": edPub,
		"ssh_host_ecdsa_key.pub":   ecdsaPub,
		"ssh_host_ed25519_key":     "private key material: secret\n",
		"README":                   "not a key",
	})
	got, err := knownHosts(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "hostbud-host ecdsa-sha2-nistp256 AAAAE2VjZHNhExample\n" +
		"hostbud-host ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample\n"
	if got != want {
		t.Fatalf("known_hosts:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "root@") {
		t.Fatal("known_hosts leaked a private key or comment")
	}
}

func TestKnownHostsErrors(t *testing.T) {
	if _, err := knownHosts(t.TempDir()); err == nil || !strings.Contains(err.Error(), "ssh_host_*_key.pub") {
		t.Fatalf("missing keys: %v", err)
	}
	dir := writeKeys(t, map[string]string{"x.pub": "private key material\n"})
	if _, err := knownHosts(dir); err == nil {
		t.Fatal("accepted a non-key .pub file")
	}
}

func testConfig(t *testing.T) Config {
	return Config{
		Dir:         filepath.Join(t.TempDir(), "ssh"),
		HostKeysDir: writeKeys(t, map[string]string{"ssh_host_ed25519_key.pub": edPub}),
		HostAddr:    "host.docker.internal",
		HostUser:    "dev",
	}
}

func TestNewWritesConfigInOrder(t *testing.T) {
	cfg := testConfig(t)
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(c.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	conf := string(data)
	order := []string{
		"Host hostbud-host", "HostName host.docker.internal", "User dev", "HostKeyAlias hostbud-host",
		"Host *", "ControlMaster auto", "ControlPath " + cfg.Dir + "/cm/%C",
		"UserKnownHostsFile " + cfg.Dir + "/known_hosts", "StrictHostKeyChecking yes", "BatchMode yes",
	}
	pos := -1
	for _, want := range order {
		i := strings.Index(conf, want)
		if i < 0 {
			t.Fatalf("config lacks %q:\n%s", want, conf)
		}
		if i < pos {
			t.Fatalf("%q out of order:\n%s", want, conf)
		}
		pos = i
	}
	if strings.Contains(conf, "Port ") {
		t.Fatal("Port written without HostPort")
	}
	for _, p := range []string{cfg.Dir, filepath.Join(cfg.Dir, "cm")} {
		st, err := os.Stat(p)
		if err != nil || st.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %v, %v; want 0700", p, st.Mode().Perm(), err)
		}
	}
	for _, f := range []string{"config", "known_hosts"} {
		st, err := os.Stat(filepath.Join(cfg.Dir, f))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode wrong: %v", f, err)
		}
	}

	cfg.HostPort = 2222
	if !strings.Contains(renderConfig(cfg), "  Port 2222\n") {
		t.Fatal("HostPort not rendered")
	}
}

func TestNewFailsWithoutHostKeys(t *testing.T) {
	cfg := testConfig(t)
	cfg.HostKeysDir = t.TempDir()
	if _, err := New(cfg); err == nil {
		t.Fatal("New succeeded without host keys")
	}
}

func TestArgs(t *testing.T) {
	c, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Args("host", []string{"-tt"}, "tmux", "attach-session", "-t", "=my session")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-F", c.ConfigPath(), "-tt", "hostbud-host", "--", `'tmux' 'attach-session' '-t' '=my session'`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Args = %q\nwant  %q", got, want)
	}
	if _, err := c.Args("server-a", nil, "true"); err == nil {
		t.Fatal("unknown machine accepted")
	}
}

// fakeSSH installs a shell script as the ssh binary.
func fakeSSH(t *testing.T, script string) *Client {
	t.Helper()
	cfg := testConfig(t)
	bin := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o700); err != nil { //nolint:gosec // test fake
		t.Fatal(err)
	}
	cfg.SSHBinary = bin
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c.agent = func() agentState { return agentOK }
	return c
}

func TestExecPassesQuotedCommand(t *testing.T) {
	c := fakeSSH(t, `for a in "$@"; do printf '[%s]' "$a"; done`)
	out, err := c.Exec(context.Background(), "host", "echo", "a b")
	if err != nil {
		t.Fatal(err)
	}
	want := "[-F][" + c.ConfigPath() + "][hostbud-host][--]['echo' 'a b']"
	if string(out) != want {
		t.Fatalf("got %s\nwant %s", out, want)
	}
}

func TestExecRemoteExitCode(t *testing.T) {
	c := fakeSSH(t, `echo "no server running on /tmp/tmux-1000/default" >&2; exit 1`)
	_, err := c.Exec(context.Background(), "host", "tmux", "ls")
	if !IsKind(err, KindRemote) {
		t.Fatalf("got %v", err)
	}
	var e *Error
	if !errors.As(err, &e) || e.ExitCode != 1 || !strings.Contains(e.Stderr, "no server running") {
		t.Fatalf("got %+v", e)
	}
}

func TestExecTimeoutKillsProcess(t *testing.T) {
	c := fakeSSH(t, `exec sleep 30`)
	c.cfg.Timeout = 200 * time.Millisecond
	start := time.Now()
	_, err := c.Exec(context.Background(), "host", "true")
	if !IsKind(err, KindTimeout) {
		t.Fatalf("got %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Exec took %v; the process wasn't killed", d)
	}
}

func TestExecContextCancel(t *testing.T) {
	c := fakeSSH(t, `exec sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	if _, err := c.Exec(ctx, "host", "true"); err == nil {
		t.Fatal("no error after cancel")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("cancel took %v", d)
	}
}

func TestExecMapsSSHFailure(t *testing.T) {
	c := fakeSSH(t, `echo "ssh: connect to host host.docker.internal port 22: Connection refused" >&2; exit 255`)
	_, err := c.Exec(context.Background(), "host", "true")
	if !IsKind(err, KindUnreachable) {
		t.Fatalf("got %v", err)
	}
}

func TestClassify(t *testing.T) {
	ok := func() agentState { return agentOK }
	cases := []struct {
		name   string
		stderr string
		agent  agentState
		kind   Kind
		hint   string
	}{
		{"refused", "ssh: connect to host h port 22: Connection refused", agentOK, KindUnreachable, "sshd"},
		{"dns", "ssh: Could not resolve hostname h: Name or service not known", agentOK, KindUnreachable, "HOSTBUD_HOST_ADDR"},
		{"timeout", "ssh: connect to host h port 22: Connection timed out", agentOK, KindUnreachable, "sshd"},
		{"denied", "dev@h: Permission denied (publickey).", agentOK, KindAuth, "authorized_keys"},
		{"agent missing", "dev@h: Permission denied (publickey).", agentMissing, KindAgent, "HOST_SSH_AUTH_SOCK"},
		{"agent empty", "dev@h: Permission denied (publickey).", agentEmpty, KindAgent, "ssh-add"},
		{"mismatch", "@@@@@\n@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\nHost key verification failed.", agentOK, KindHostKey, "re-pin"},
		{"unknown key", "No ED25519 host key is known for hostbud-host and you have requested strict checking.\nHost key verification failed.", agentOK, KindHostKey, "HOSTBUD_HOST_ADDR"},
		{"other", "kex_exchange_identification: something odd", agentOK, KindSSH, "debug"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := ok
			if tc.agent != agentOK {
				a := tc.agent
				agent = func() agentState { return a }
			}
			e := classify(tc.stderr, agent)
			if e.Kind != tc.kind || !strings.Contains(e.Hint, tc.hint) || e.Message == "" {
				t.Fatalf("classify = %+v; want kind %s, hint containing %q", e, tc.kind, tc.hint)
			}
			if strings.Contains(e.Error(), "dev@h") {
				t.Fatal("Error() leaks stderr details")
			}
		})
	}
}
