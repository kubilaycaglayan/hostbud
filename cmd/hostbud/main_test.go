package main

import (
	"hostbud/internal/config"
	"hostbud/internal/llm"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLLMSupervisorIsNotConstructedUnlessProviderIsValid(t *testing.T) {
	for _, cfg := range []llm.Config{{}, {Provider: "openai", Model: "test-model", QuietAfter: time.Minute, MaxPerRunHour: 2}} {
		supervisor, status := newLLMSupervisor(cfg, nil, nil, nil, nil)
		if supervisor != nil || status.Enabled || status.Reason == "" {
			t.Fatalf("partial config constructed supervisor: supervisor=%v status=%+v", supervisor, status)
		}
	}
	cfg := llm.Config{Provider: "openai", Model: "test-model", APIKey: "test-key", QuietAfter: 20 * time.Minute, MaxPerRunHour: 2, Scrub: true, ScrubValid: true}
	supervisor, status := newLLMSupervisor(cfg, nil, nil, nil, nil)
	if supervisor == nil || !status.Enabled {
		t.Fatalf("valid opt-in config did not construct supervisor: supervisor=%v status=%+v", supervisor, status)
	}
}

func TestBuildDepsWiresRemoteTimeouts(t *testing.T) {
	dataDir, keysDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(keysDir, "ssh_host_ed25519_key.pub"), []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample host\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, err := buildDepsAt(config.Config{
		DataDir: dataDir, HostAddr: "server-a", HostSSHUser: "dev",
		ExecTimeout: 17 * time.Second, SFTPTimeout: 23 * time.Second, UploadTimeout: 37 * time.Second,
	}, keysDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := deps.ssh.Timeout(); got != 17*time.Second {
		t.Errorf("ssh timeout = %s, want 17s", got)
	}
	// Every machine's file browser is built from these options.
	if got := deps.machineOpts.SFTPTimeout; got != 23*time.Second {
		t.Errorf("SFTP timeout = %s, want 23s", got)
	}
	if got := deps.machineOpts.UploadTimeout; got != 37*time.Second {
		t.Errorf("upload timeout = %s, want 37s", got)
	}
}

func TestMakeTSLoginRequiresSocketOnlyWhenEnabled(t *testing.T) {
	if login, err := makeTSLogin(config.Config{}); err != nil || login != nil {
		t.Fatalf("disabled gate: login configured=%t err=%v", login != nil, err)
	}
	_, err := makeTSLogin(config.Config{AllowedTSUsers: "owner@example.com", TailscaleSocket: filepath.Join(t.TempDir(), "missing.sock")})
	if err == nil || !strings.Contains(err.Error(), "TAILSCALED_SOCKET") || !strings.Contains(err.Error(), "deploy/compose.tailscale.yml") {
		t.Fatalf("missing socket error=%v", err)
	}
	socket := filepath.Join(t.TempDir(), "tailscaled.sock")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	if login, err := makeTSLogin(config.Config{AllowedTSUsers: "owner@example.com", TailscaleSocket: socket}); err != nil || login == nil {
		t.Fatalf("enabled gate: login configured=%t err=%v", login != nil, err)
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

// V2-M3 T0: make vapid-keys prints a pair the config accepts.
func TestPrintVAPIDKeysIsAValidPair(t *testing.T) {
	var out strings.Builder
	if err := printVAPIDKeys(&out); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_VAPID_SUBJECT": "mailto:owner@example.com"}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		k, v, _ := strings.Cut(line, "=")
		env[k] = v
	}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if p := cfg.Push(); !p.Available {
		t.Fatalf("printed pair is not accepted: %+v", p)
	}
}
