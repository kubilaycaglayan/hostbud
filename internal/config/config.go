// Package config loads hostbud's configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Config is the runtime configuration. Every field comes from an env var;
// see .env.example for documentation.
type Config struct {
	Listen       string        // HOSTBUD_LISTEN
	DataDir      string        // HOSTBUD_DATA_DIR
	PollInterval time.Duration // HOSTBUD_POLL_INTERVAL
	LogLevel     slog.Level    // HOSTBUD_LOG_LEVEL
	LocalPort    int           // HOSTBUD_LOCAL_PORT
	Domain       string        // HOSTBUD_DOMAIN (optional until M2)
	HostSSHUser  string        // HOST_SSH_USER
	HostLabel    string        // HOSTBUD_HOST_LABEL
}

// Load reads the configuration using getenv (usually os.Getenv).
// It returns all validation problems at once.
func Load(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}

	var errs []error
	cfg := Config{
		Listen:      get("HOSTBUD_LISTEN", ":8080"),
		DataDir:     get("HOSTBUD_DATA_DIR", "/data"),
		Domain:      get("HOSTBUD_DOMAIN", ""),
		HostSSHUser: get("HOST_SSH_USER", ""),
		HostLabel:   get("HOSTBUD_HOST_LABEL", "Host machine"),
	}

	poll, err := time.ParseDuration(get("HOSTBUD_POLL_INTERVAL", "3s"))
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("HOSTBUD_POLL_INTERVAL: %w (use a duration like 3s)", err))
	case poll < 500*time.Millisecond:
		errs = append(errs, errors.New("HOSTBUD_POLL_INTERVAL: must be at least 500ms"))
	}
	cfg.PollInterval = poll

	if err := cfg.LogLevel.UnmarshalText([]byte(get("HOSTBUD_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("HOSTBUD_LOG_LEVEL: %w (use debug, info, warn or error)", err))
	}

	port, err := strconv.Atoi(get("HOSTBUD_LOCAL_PORT", "9055"))
	if err != nil || port < 1 || port > 65535 {
		errs = append(errs, errors.New("HOSTBUD_LOCAL_PORT: must be a port number between 1 and 65535"))
	}
	cfg.LocalPort = port

	if strings.Contains(cfg.Domain, "/") || strings.Contains(cfg.Domain, ":") {
		errs = append(errs, errors.New("HOSTBUD_DOMAIN: must be a bare hostname like hostbud.example.com (no scheme or port)"))
	}
	if cfg.HostSSHUser == "" {
		errs = append(errs, errors.New("HOST_SSH_USER: required — the user hostbud logs in as on the host (see .env.example)"))
	}

	return cfg, errors.Join(errs...)
}
