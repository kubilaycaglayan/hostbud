package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{"HOST_SSH_USER": "dev"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Listen:       ":8080",
		DataDir:      "/data",
		PollInterval: 3 * time.Second,
		LogLevel:     slog.LevelInfo,
		LocalPort:    9055,
		HostSSHUser:  "dev",
		HostLabel:    "Host machine",
	}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{
		"HOSTBUD_LISTEN":        ":9000",
		"HOSTBUD_DATA_DIR":      "/tmp/hb",
		"HOSTBUD_POLL_INTERVAL": "1500ms",
		"HOSTBUD_LOG_LEVEL":     "debug",
		"HOSTBUD_LOCAL_PORT":    "9100",
		"HOSTBUD_DOMAIN":        "hostbud.example.com",
		"HOST_SSH_USER":         " dev ",
		"HOSTBUD_HOST_LABEL":    "server-a",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":9000" || cfg.DataDir != "/tmp/hb" || cfg.PollInterval != 1500*time.Millisecond ||
		cfg.LogLevel != slog.LevelDebug || cfg.LocalPort != 9100 || cfg.Domain != "hostbud.example.com" ||
		cfg.HostSSHUser != "dev" || cfg.HostLabel != "server-a" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := Load(envFrom(map[string]string{
		"HOSTBUD_POLL_INTERVAL": "soon",
		"HOSTBUD_LOG_LEVEL":     "loud",
		"HOSTBUD_LOCAL_PORT":    "70000",
		"HOSTBUD_DOMAIN":        "https://hostbud.example.com",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, key := range []string{"HOSTBUD_POLL_INTERVAL", "HOSTBUD_LOG_LEVEL", "HOSTBUD_LOCAL_PORT", "HOSTBUD_DOMAIN", "HOST_SSH_USER"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestLoadRejectsTinyPollInterval(t *testing.T) {
	_, err := Load(envFrom(map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_POLL_INTERVAL": "10ms"}))
	if err == nil || !strings.Contains(err.Error(), "at least 500ms") {
		t.Fatalf("expected minimum interval error, got %v", err)
	}
}
