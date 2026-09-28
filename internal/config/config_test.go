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
		Listen:              ":8080",
		DataDir:             "/data",
		PollInterval:        3 * time.Second,
		ExecTimeout:         10 * time.Second,
		SFTPTimeout:         10 * time.Second,
		UploadTimeout:       5 * time.Minute,
		RunStaleAfter:       2 * time.Hour,
		MaxTerminalsPerUser: 32,
		MaxTerminals:        128,
		LogLevel:            slog.LevelInfo,
		LocalPort:           9055,
		HostSSHUser:         "dev",
		HostAddr:            "host.docker.internal",
		HostLabel:           "Host machine",
		DBHost:              "hostbud-postgres",
		DBPort:              5432,
		DBName:              "hostbud",
		DBUser:              "hostbud",
		DBSSLMode:           "disable",
		DBLocalPort:         9543,

		SessionTTL:          720 * time.Hour,
		LoginMaxFailures:    5,
		RegisterMaxFailures: 10,
		IPMaxFailures:       20,
		LoginBlockBase:      30 * time.Second,
		LoginBlockMax:       time.Hour,
		LoginBlockFactor:    2,
		LoginFailureWindow:  time.Hour,
		TrustedProxies:      DefaultTrustedProxies,
	}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadHardeningBounds(t *testing.T) {
	cases := []struct {
		name    string
		vars    map[string]string
		wantErr string
	}{
		{name: "valid minimum and maximum", vars: map[string]string{
			"HOST_SSH_USER": "dev", "HOSTBUD_EXEC_TIMEOUT": "2s", "HOSTBUD_SFTP_TIMEOUT": "2m",
			"HOSTBUD_MAX_TERMINALS_PER_USER": "256", "HOSTBUD_MAX_TERMINALS": "1024",
		}},
		{name: "exec below range", vars: map[string]string{"HOSTBUD_EXEC_TIMEOUT": "1999ms"}, wantErr: "HOSTBUD_EXEC_TIMEOUT"},
		{name: "exec above range", vars: map[string]string{"HOSTBUD_EXEC_TIMEOUT": "121s"}, wantErr: "HOSTBUD_EXEC_TIMEOUT"},
		{name: "exec malformed", vars: map[string]string{"HOSTBUD_EXEC_TIMEOUT": "soon"}, wantErr: "HOSTBUD_EXEC_TIMEOUT"},
		{name: "sftp below range", vars: map[string]string{"HOSTBUD_SFTP_TIMEOUT": "1s"}, wantErr: "HOSTBUD_SFTP_TIMEOUT"},
		{name: "sftp above range", vars: map[string]string{"HOSTBUD_SFTP_TIMEOUT": "3m"}, wantErr: "HOSTBUD_SFTP_TIMEOUT"},
		{name: "sftp malformed", vars: map[string]string{"HOSTBUD_SFTP_TIMEOUT": "soon"}, wantErr: "HOSTBUD_SFTP_TIMEOUT"},
		{name: "upload below range", vars: map[string]string{"HOSTBUD_UPLOAD_TIMEOUT": "29s"}, wantErr: "HOSTBUD_UPLOAD_TIMEOUT"},
		{name: "upload above range", vars: map[string]string{"HOSTBUD_UPLOAD_TIMEOUT": "11m"}, wantErr: "HOSTBUD_UPLOAD_TIMEOUT"},
		{name: "upload malformed", vars: map[string]string{"HOSTBUD_UPLOAD_TIMEOUT": "soon"}, wantErr: "HOSTBUD_UPLOAD_TIMEOUT"},
		{name: "per user below range", vars: map[string]string{"HOSTBUD_MAX_TERMINALS_PER_USER": "0"}, wantErr: "HOSTBUD_MAX_TERMINALS_PER_USER"},
		{name: "per user above range", vars: map[string]string{"HOSTBUD_MAX_TERMINALS_PER_USER": "257"}, wantErr: "HOSTBUD_MAX_TERMINALS_PER_USER"},
		{name: "per user malformed", vars: map[string]string{"HOSTBUD_MAX_TERMINALS_PER_USER": "many"}, wantErr: "HOSTBUD_MAX_TERMINALS_PER_USER"},
		{name: "global below range", vars: map[string]string{"HOSTBUD_MAX_TERMINALS": "0"}, wantErr: "HOSTBUD_MAX_TERMINALS"},
		{name: "global above range", vars: map[string]string{"HOSTBUD_MAX_TERMINALS": "1025"}, wantErr: "HOSTBUD_MAX_TERMINALS"},
		{name: "global malformed", vars: map[string]string{"HOSTBUD_MAX_TERMINALS": "many"}, wantErr: "HOSTBUD_MAX_TERMINALS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vars := map[string]string{"HOST_SSH_USER": "dev"}
			for k, v := range tc.vars {
				vars[k] = v
			}
			cfg, err := Load(envFrom(vars))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Load error = %v, want error naming %s", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ExecTimeout != 2*time.Second || cfg.SFTPTimeout != 2*time.Minute || cfg.MaxTerminalsPerUser != 256 || cfg.MaxTerminals != 1024 {
				t.Fatalf("unexpected limits: %+v", cfg)
			}
		})
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

func TestLoadAuthSettings(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{
		"HOST_SSH_USER":                  "dev",
		"HOSTBUD_SESSION_TTL":            "12h",
		"HOSTBUD_LOGIN_MAX_FAILURES":     "3",
		"HOSTBUD_REGISTER_MAX_FAILURES":  "4",
		"HOSTBUD_IP_MAX_FAILURES":        "6",
		"HOSTBUD_LOGIN_BLOCK_BASE":       "1s",
		"HOSTBUD_LOGIN_BLOCK_MAX":        "8s",
		"HOSTBUD_LOGIN_BLOCK_MULTIPLIER": "1.5",
		"HOSTBUD_LOGIN_FAILURE_WINDOW":   "10m",
		"HOSTBUD_TRUSTED_PROXIES":        "172.29.55.0/24",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionTTL != 12*time.Hour || cfg.LoginMaxFailures != 3 || cfg.RegisterMaxFailures != 4 ||
		cfg.IPMaxFailures != 6 || cfg.LoginBlockBase != time.Second || cfg.LoginBlockMax != 8*time.Second ||
		cfg.LoginBlockFactor != 1.5 || cfg.LoginFailureWindow != 10*time.Minute || cfg.TrustedProxies != "172.29.55.0/24" {
		t.Fatalf("got %+v", cfg)
	}

	_, err = Load(envFrom(map[string]string{
		"HOST_SSH_USER":                  "dev",
		"HOSTBUD_SESSION_TTL":            "soon",
		"HOSTBUD_LOGIN_MAX_FAILURES":     "0",
		"HOSTBUD_LOGIN_BLOCK_BASE":       "10s",
		"HOSTBUD_LOGIN_BLOCK_MAX":        "5s",
		"HOSTBUD_LOGIN_BLOCK_MULTIPLIER": "0.5",
		"HOSTBUD_TRUSTED_PROXIES":        "not-a-cidr",
	}))
	for _, key := range []string{"HOSTBUD_SESSION_TTL", "HOSTBUD_LOGIN_MAX_FAILURES", "HOSTBUD_LOGIN_BLOCK_MAX",
		"HOSTBUD_LOGIN_BLOCK_MULTIPLIER", "HOSTBUD_TRUSTED_PROXIES"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestLoadHookBaseURL(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_LOCAL_PORT": "9123"}))
	if err != nil || cfg.HookBaseURL != "" || cfg.HookURL() != "http://127.0.0.1:9123" {
		t.Fatalf("default: %q %q %v", cfg.HookBaseURL, cfg.HookURL(), err)
	}
	for raw, want := range map[string]string{
		"http://hostbud-e2e-caddy:9055": "http://hostbud-e2e-caddy:9055",
		"https://hostbud.example.com/":  "https://hostbud.example.com",
		" http://127.0.0.1:9055 ":       "http://127.0.0.1:9055",
		"http://[::1]:9055":             "http://[::1]:9055",
	} {
		cfg, err := Load(envFrom(map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_HOOK_BASE_URL": raw}))
		if err != nil || cfg.HookURL() != want {
			t.Errorf("%q: %q, %v; want %q", raw, cfg.HookURL(), err, want)
		}
	}
	for _, raw := range []string{
		"hostbud-e2e-caddy:9055", "ftp://example.com", "http://", "http://example.com/api", "http://example.com?x=1",
		"http://example.com#frag", "http://user:pw@example.com", "/relative", "http://example.com/?",
	} {
		_, err := Load(envFrom(map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_HOOK_BASE_URL": raw}))
		if err == nil || !strings.Contains(err.Error(), "HOSTBUD_HOOK_BASE_URL") {
			t.Errorf("%q accepted: %v", raw, err)
		}
	}
}

func TestLoadRunStaleAfter(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_RUN_STALE_AFTER": "90m"}))
	if err != nil || cfg.RunStaleAfter != 90*time.Minute {
		t.Fatalf("90m: %v, %v", cfg.RunStaleAfter, err)
	}
	for _, bad := range []string{"5s", "25h", "soon", "-1h"} {
		if _, err := Load(envFrom(map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_RUN_STALE_AFTER": bad})); err == nil || !strings.Contains(err.Error(), "HOSTBUD_RUN_STALE_AFTER") {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
}

// V2-M2 T2: the parallel-queues switch is off by default and only takes
// true or false.
func TestLoadParallelQueues(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{"HOST_SSH_USER": "dev"}))
	if err != nil || cfg.ParallelQueues {
		t.Fatalf("default: %v, %v; want off", cfg.ParallelQueues, err)
	}
	cfg, err = Load(envFrom(map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_PARALLEL_QUEUES": "true"}))
	if err != nil || !cfg.ParallelQueues {
		t.Fatalf("true: %v, %v", cfg.ParallelQueues, err)
	}
	cfg, err = Load(envFrom(map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_PARALLEL_QUEUES": "false"}))
	if err != nil || cfg.ParallelQueues {
		t.Fatalf("false: %v, %v", cfg.ParallelQueues, err)
	}
	for _, bad := range []string{"1", "yes", "TRUE", "on", "maybe"} {
		if _, err := Load(envFrom(map[string]string{"HOST_SSH_USER": "dev", "HOSTBUD_PARALLEL_QUEUES": bad})); err == nil || !strings.Contains(err.Error(), "HOSTBUD_PARALLEL_QUEUES") {
			t.Errorf("%q: err=%v, want a HOSTBUD_PARALLEL_QUEUES error", bad, err)
		}
	}
}
