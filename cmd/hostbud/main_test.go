package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"hostbud/internal/config"
)

func TestBuildDepsWiresRemoteTimeouts(t *testing.T) {
	dataDir, keysDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(keysDir, "ssh_host_ed25519_key.pub"), []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample host\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, err := buildDepsAt(config.Config{
		DataDir: dataDir, HostAddr: "server-a", HostSSHUser: "dev",
		ExecTimeout: 17 * time.Second, SFTPTimeout: 23 * time.Second,
	}, keysDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deps.filesystem.Close() }()
	if got := deps.ssh.Timeout(); got != 17*time.Second {
		t.Errorf("ssh timeout = %s, want 17s", got)
	}
	if got := deps.filesystem.OperationTimeout(); got != 23*time.Second {
		t.Errorf("SFTP timeout = %s, want 23s", got)
	}
}
