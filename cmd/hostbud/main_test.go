package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestHealthcheckExitConditions(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		defer srv.Close()
		if err := verifyHealth(t.Context(), srv.Client(), srv.URL); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("degraded", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
		defer srv.Close()
		if err := verifyHealth(t.Context(), srv.Client(), srv.URL); err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("error = %v, want HTTP 503", err)
		}
	})
	t.Run("refused", func(t *testing.T) {
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		endpoint := "http://" + listener.Addr().String()
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		if err := verifyHealth(t.Context(), &http.Client{Timeout: time.Second}, endpoint); err == nil {
			t.Fatal("request to a closed port succeeded")
		}
	})
	t.Run("slow", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		if err := verifyHealth(t.Context(), &http.Client{Timeout: 20 * time.Millisecond}, srv.URL); err == nil {
			t.Fatal("slow endpoint succeeded")
		}
	})
}
