package sshx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realEd is a syntactically valid ed25519 public key (a throwaway fixture).
const realEd = "AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"

func TestValidateConnectionFields(t *testing.T) {
	for _, h := range []string{"server-a.example.com", "server-a", "10.0.0.5", "fd7a:115c:a1e0::1", "a"} {
		if err := ValidateHostName(h); err != nil {
			t.Errorf("host %q: %v", h, err)
		}
	}
	for _, h := range []string{"", "-oProxyCommand=x", "a b", "a\nb", "server-a.", "a..b", "x;y", "fe80::1%eth0", strings.Repeat("a", 254)} {
		if ValidateHostName(h) == nil {
			t.Errorf("host %q accepted", h)
		}
	}
	for _, u := range []string{"dev", "_svc", "dev.user", "dev-1"} {
		if err := ValidateUser(u); err != nil {
			t.Errorf("user %q: %v", u, err)
		}
	}
	for _, u := range []string{"", "-l", "a b", "root\n", "1dev", strings.Repeat("a", 33)} {
		if ValidateUser(u) == nil {
			t.Errorf("user %q accepted", u)
		}
	}
	for _, p := range []int{0, -1, 65536} {
		if ValidatePort(p) == nil {
			t.Errorf("port %d accepted", p)
		}
	}
}

func TestParseHostKeyFingerprint(t *testing.T) {
	k, err := ParseHostKey("ssh-ed25519", realEd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k.Fingerprint, "SHA256:") || len(k.Fingerprint) != len("SHA256:")+43 || strings.HasSuffix(k.Fingerprint, "=") {
		t.Fatalf("fingerprint = %q", k.Fingerprint)
	}
	for _, bad := range [][2]string{{"ssh-rsa", realEd}, {"ssh-ed25519", "not base64!"}, {"bogus", realEd}, {"ssh-ed25519", "AAAA"}} {
		if _, err := ParseHostKey(bad[0], bad[1]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func testTarget() Target {
	k, _ := ParseHostKey("ssh-ed25519", realEd)
	return Target{ID: "s-abc123", Alias: "hostbud-custom-s-abc123", HostName: "server-a.example.com", Port: 2222, User: "dev", Keys: []HostKey{k}}
}

func TestSetTargetsRewritesConfigAndKnownHosts(t *testing.T) {
	c, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetTargets([]Target{testTarget()}); err != nil {
		t.Fatal(err)
	}
	conf, _ := os.ReadFile(c.ConfigPath())
	order := []string{"Host hostbud-host", "Host hostbud-custom-s-abc123", "HostName server-a.example.com", "Port 2222", "User dev",
		"HostKeyAlias hostbud-custom-s-abc123", "Host *"}
	pos := 0
	for _, want := range order {
		i := strings.Index(string(conf)[pos:], want)
		if i < 0 {
			t.Fatalf("%q missing or out of order:\n%s", want, conf)
		}
		pos += i + len(want)
	}
	known, _ := os.ReadFile(filepath.Join(c.cfg.Dir, "known_hosts"))
	if !strings.Contains(string(known), "hostbud-host ssh-ed25519 ") || !strings.Contains(string(known), "hostbud-custom-s-abc123 ssh-ed25519 "+realEd+"\n") {
		t.Fatalf("known_hosts:\n%s", known)
	}
	if alias, err := c.Alias("s-abc123"); err != nil || alias != "hostbud-custom-s-abc123" {
		t.Fatalf("alias = %q, %v", alias, err)
	}
	argv, err := c.Args("s-abc123", nil, "true")
	if err != nil || argv[2] != "hostbud-custom-s-abc123" {
		t.Fatalf("args = %v, %v", argv, err)
	}

	// An invalid target changes nothing.
	bad := testTarget()
	bad.ID, bad.Alias = "s-zzz999", "hostbud-custom-s-zzz999"
	bad.HostName = "-oProxyCommand=evil"
	if err := c.SetTargets([]Target{testTarget(), bad}); err == nil {
		t.Fatal("invalid target accepted")
	}
	if again, _ := os.ReadFile(c.ConfigPath()); string(again) != string(conf) {
		t.Fatal("config changed after a rejected update")
	}
	for _, mutate := range []func(*Target){
		func(t *Target) { t.Alias = "other" },
		func(t *Target) { t.Keys = nil },
		func(t *Target) { t.User = "a b" },
		func(t *Target) { t.Port = 0 },
		func(t *Target) { t.ID, t.Alias = "S-X", "hostbud-custom-S-X" },
	} {
		tt := testTarget()
		mutate(&tt)
		if ValidateTarget(tt) == nil {
			t.Errorf("accepted %+v", tt)
		}
	}

	if err := c.SetTargets(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Alias("s-abc123"); err == nil {
		t.Fatal("removed target still resolves")
	}
	if conf, _ := os.ReadFile(c.ConfigPath()); strings.Contains(string(conf), "hostbud-custom") {
		t.Fatal("removed target still in config")
	}
}

func TestScanParsesKeyscanOutput(t *testing.T) {
	cfg := testConfig(t)
	bin := filepath.Join(t.TempDir(), "ssh-keyscan")
	script := "#!/bin/sh\necho \"$@\" > " + filepath.Join(filepath.Dir(bin), "args") + "\n" +
		"echo '# server-a:2222 SSH-2.0-OpenSSH' >&2\n" +
		"echo 'server-a ssh-ed25519 " + realEd + "'\n" +
		"echo 'server-a ssh-ed25519 " + realEd + "'\n" +
		"echo 'server-a ssh-rsa bogus'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { //nolint:gosec // test fake
		t.Fatal(err)
	}
	cfg.KeyscanBinary = bin
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := c.Scan(context.Background(), "server-a", 2222)
	if err != nil || len(keys) != 1 || keys[0].Type != "ssh-ed25519" || keys[0].Fingerprint == "" {
		t.Fatalf("scan = %+v, %v", keys, err)
	}
	args, _ := os.ReadFile(filepath.Join(filepath.Dir(bin), "args"))
	if string(args) != "-T 5 -p 2222 server-a\n" {
		t.Fatalf("args = %q", args)
	}
	if _, err := c.Scan(context.Background(), "-oX", 22); err == nil {
		t.Fatal("bad host scanned")
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil { //nolint:gosec // test fake
		t.Fatal(err)
	}
	if _, err := c.Scan(context.Background(), "server-a", 22); !IsKind(err, KindUnreachable) {
		t.Fatalf("no keys: %v", err)
	}
}

func TestServerErrorsNameTheServer(t *testing.T) {
	ok := func(context.Context) agentState { return agentOK }
	for _, stderr := range []string{
		"Host key verification failed.", "ssh: connect to host h port 22: Connection refused",
		"ssh: Could not resolve hostname h: Name or service not known", "dev@h: Permission denied (publickey).", "odd",
	} {
		host := classify(context.Background(), stderr, ok)
		server := forMachine("s-abc123", classify(context.Background(), stderr, ok))
		if host.Kind != server.Kind || strings.Contains(server.Hint, "HOSTBUD_HOST_ADDR") || strings.Contains(server.Message, "the host") {
			t.Errorf("%q: server error %+v", stderr, server)
		}
	}
	if e := forMachine(HostMachineID, &Error{Kind: KindAuth, Message: "m"}); e.Message != "m" {
		t.Fatal("host error rewritten")
	}
}
