//go:build integration

// Package testenv gives integration tests (`-tags=integration`, run by
// `make test`) access to the test/sshd targets started by
// scripts/test-sshd.sh, their throwaway keys, and a private ssh-agent.
package testenv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hostbud/internal/sshx"
)

// Targets on the hostbud-test network.
const (
	SSHD       = "hostbud-test-sshd"
	SSHDNoTmux = "hostbud-test-sshd-notmux"
	User       = "dev"
)

// KeysDir is .cache/test-sshd/keys (HOSTBUD_TEST_KEYS overrides).
func KeysDir() string {
	if d := os.Getenv("HOSTBUD_TEST_KEYS"); d != "" {
		return d
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", ".cache", "test-sshd", "keys")
}

// shortTemp returns a short temp dir (unix socket paths are limited to ~108
// bytes; t.TempDir paths embed the test name).
func shortTemp(t testing.TB, prefix string) string {
	t.Helper()
	d, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// Agent starts an ssh-agent holding the client key (or no key if empty)
// and points SSH_AUTH_SOCK at it for the rest of the test.
func Agent(t testing.TB, withKey bool) string {
	t.Helper()
	sock := filepath.Join(shortTemp(t, "hba"), "agent.sock")
	cmd := exec.CommandContext(context.Background(), "ssh-agent", "-D", "-a", sock)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start ssh-agent: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for i := 0; ; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if i > 100 {
			t.Fatal("ssh-agent socket never appeared")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Setenv("SSH_AUTH_SOCK", sock)
	if withKey {
		key := filepath.Join(KeysDir(), "client", "id_ed25519")
		if out, err := exec.CommandContext(context.Background(), "ssh-add", "-q", key).CombinedOutput(); err != nil {
			t.Fatalf("ssh-add: %v: %s (did scripts/test-sshd.sh up run?)", err, out)
		}
	}
	return sock
}

// Options adjust Client.
type Options struct {
	HostKeysDir string // default: the target's real public keys
	Port        int
	Timeout     time.Duration
}

// Client returns an sshx client for host with a fresh config dir; its
// ControlMaster is stopped when the test ends. It does not start an agent.
func Client(t testing.TB, host string, o Options) *sshx.Client {
	t.Helper()
	if o.HostKeysDir == "" {
		o.HostKeysDir = filepath.Join(KeysDir(), "hostpub")
	}
	c, err := sshx.New(sshx.Config{
		Dir:         filepath.Join(shortTemp(t, "hbs"), "ssh"),
		HostKeysDir: o.HostKeysDir,
		HostAddr:    host,
		HostPort:    o.Port,
		HostUser:    User,
		Timeout:     o.Timeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	return c
}

// Connected returns a client for host with an agent holding the key.
func Connected(t testing.TB, host string) *sshx.Client {
	t.Helper()
	Agent(t, true)
	return Client(t, host, Options{})
}

// Sh runs a shell command line on the machine behind c (test setup only).
func Sh(t testing.TB, c *sshx.Client, line string) string {
	t.Helper()
	out, err := c.Exec(context.Background(), sshx.HostMachineID, "sh", "-c", line)
	if err != nil {
		var detail string
		var e *sshx.Error
		if errors.As(err, &e) {
			detail = e.Stderr
		}
		t.Fatalf("sh -c %q: %v %s", line, err, strings.TrimSpace(detail))
	}
	return string(out)
}

// WrongHostKeys writes a freshly generated public key (not the target's) to
// a dir and returns it.
func WrongHostKeys(t testing.TB) string {
	t.Helper()
	dir := shortTemp(t, "hbk")
	key := filepath.Join(dir, "k")
	if out, err := exec.CommandContext(context.Background(), "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	pubs := filepath.Join(dir, "pub")
	if err := os.Mkdir(pubs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(key+".pub", filepath.Join(pubs, "ssh_host_ed25519_key.pub")); err != nil {
		t.Fatal(err)
	}
	return pubs
}

// AddServer scans host's keys and pins them on c as the server id (V2-M13),
// the way the UI does after the owner confirms the fingerprints.
func AddServer(t testing.TB, c *sshx.Client, id, host string) sshx.Target {
	t.Helper()
	keys, err := c.Scan(t.Context(), host, 22)
	if err != nil {
		t.Fatalf("scan %s: %v", host, err)
	}
	target := sshx.Target{ID: id, Alias: sshx.TargetAliasPrefix + id, HostName: host, Port: 22, User: User, Keys: keys}
	if err := c.SetTargets(append(c.Targets(), target)); err != nil {
		t.Fatal(err)
	}
	return target
}
