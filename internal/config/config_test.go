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
		HostAddr:     "host.docker.internal",
		HostLabel:    "Host machine",
		DBHost:       "hostbud-postgres",
		DBPort:       5432,
		DBName:       "hostbud",
		DBUser:       "hostbud",
		DBSSLMode:    "disable",
		DBLocalPort:  9543,
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
		"HOSTBUD_HOST_ADDR":     "hostbud-e2e-target",
		"HOSTBUD_DB_HOST":       "server-a",
		"HOSTBUD_DB_PORT":       "55432",
		"HOSTBUD_DB_NAME":       "example",
		"HOSTBUD_DB_USER":       "dev",
		"HOSTBUD_DB_PASSWORD":   "secret-placeholder",
		"HOSTBUD_DB_SSLMODE":    "require",
		"HOSTBUD_DB_LOCAL_PORT": "19543",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":9000" || cfg.DataDir != "/tmp/hb" || cfg.PollInterval != 1500*time.Millisecond ||
		cfg.LogLevel != slog.LevelDebug || cfg.LocalPort != 9100 || cfg.Domain != "hostbud.example.com" ||
		cfg.HostSSHUser != "dev" || cfg.HostLabel != "server-a" || cfg.HostAddr != "hostbud-e2e-target" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.DBHost != "server-a" || cfg.DBPort != 55432 || cfg.DBName != "example" || cfg.DBUser != "dev" ||
		cfg.DBPassword != "secret-placeholder" || cfg.DBSSLMode != "require" || cfg.DBLocalPort != 19543 {
		t.Fatalf("unexpected database config: %+v", cfg)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := Load(envFrom(map[string]string{
		"HOSTBUD_POLL_INTERVAL": "soon",
		"HOSTBUD_LOG_LEVEL":     "loud",
		"HOSTBUD_LOCAL_PORT":    "70000",
		"HOSTBUD_DOMAIN":        "https://hostbud.example.com",
		"HOSTBUD_HOST_ADDR":     "dev@server-a",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, key := range []string{"HOSTBUD_POLL_INTERVAL", "HOSTBUD_LOG_LEVEL", "HOSTBUD_LOCAL_PORT", "HOSTBUD_DOMAIN", "HOSTBUD_HOST_ADDR", "HOST_SSH_USER"} {
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
