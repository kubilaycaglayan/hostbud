// Package config loads hostbud's configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"hostbud/internal/auth"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config is the runtime configuration. Every field comes from an env var;
// see .env.example for documentation.
type Config struct {
	Listen              string        // HOSTBUD_LISTEN
	DataDir             string        // HOSTBUD_DATA_DIR
	PollInterval        time.Duration // HOSTBUD_POLL_INTERVAL
	ExecTimeout         time.Duration // HOSTBUD_EXEC_TIMEOUT
	SFTPTimeout         time.Duration // HOSTBUD_SFTP_TIMEOUT
	UploadTimeout       time.Duration // HOSTBUD_UPLOAD_TIMEOUT
	MaxTerminalsPerUser int           // HOSTBUD_MAX_TERMINALS_PER_USER
	MaxTerminals        int           // HOSTBUD_MAX_TERMINALS
	LogLevel            slog.Level    // HOSTBUD_LOG_LEVEL
	LocalPort           int           // HOSTBUD_LOCAL_PORT
	Domain              string        // HOSTBUD_DOMAIN (optional until M2)
	HostSSHUser         string        // HOST_SSH_USER
	HostAddr            string        // HOSTBUD_HOST_ADDR
	HostLabel           string        // HOSTBUD_HOST_LABEL
	DBHost              string        // HOSTBUD_DB_HOST
	DBPort              int           // HOSTBUD_DB_PORT
	DBName              string        // HOSTBUD_DB_NAME
	DBUser              string        // HOSTBUD_DB_USER
	DBPassword          string        // HOSTBUD_DB_PASSWORD
	DBSSLMode           string        // HOSTBUD_DB_SSLMODE
	DBLocalPort         int           // HOSTBUD_DB_LOCAL_PORT

	// Authentication (docs/ARCHITECTURE.md §8).
	SessionTTL          time.Duration // HOSTBUD_SESSION_TTL
	LoginMaxFailures    int           // HOSTBUD_LOGIN_MAX_FAILURES
	RegisterMaxFailures int           // HOSTBUD_REGISTER_MAX_FAILURES
	IPMaxFailures       int           // HOSTBUD_IP_MAX_FAILURES
	LoginBlockBase      time.Duration // HOSTBUD_LOGIN_BLOCK_BASE
	LoginBlockMax       time.Duration // HOSTBUD_LOGIN_BLOCK_MAX
	LoginBlockFactor    float64       // HOSTBUD_LOGIN_BLOCK_MULTIPLIER
	LoginFailureWindow  time.Duration // HOSTBUD_LOGIN_FAILURE_WINDOW
	TrustedProxies      string        // HOSTBUD_TRUSTED_PROXIES (CIDRs, comma-separated)
	AllowedTSUsers      string        // HOSTBUD_ALLOWED_TS_USERS (optional, comma-separated)
	TailscaleSocket     string        // TAILSCALED_SOCKET (optional, required when allowlist is enabled)
	HookBaseURL         string        // HOSTBUD_HOOK_BASE_URL (optional: HOSTBUD_URL inside run sessions)
	RunStaleAfter       time.Duration // HOSTBUD_RUN_STALE_AFTER (v2: no-signal window before a run is stale)
	ParallelQueues      bool          // HOSTBUD_PARALLEL_QUEUES (V2-M2 opt-in: several queues, parallel runs)
}

// HookURL is HOSTBUD_URL for run sessions (v2): the override, or Caddy's
// loopback site on the host.
func (c Config) HookURL() string {
	if c.HookBaseURL != "" {
		return c.HookBaseURL
	}
	return fmt.Sprintf("http://127.0.0.1:%d", c.LocalPort)
}

// DefaultTrustedProxies are the private ranges Docker networks use: hostbud
// publishes no ports, so its only peer is Caddy on the Compose network.
const DefaultTrustedProxies = "127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

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
		HostAddr:    get("HOSTBUD_HOST_ADDR", "host.docker.internal"),
		HostLabel:   get("HOSTBUD_HOST_LABEL", "Host machine"),
		DBHost:      get("HOSTBUD_DB_HOST", "hostbud-postgres"),
		DBName:      get("HOSTBUD_DB_NAME", "hostbud"),
		DBUser:      get("HOSTBUD_DB_USER", "hostbud"),
		DBPassword:  getenv("HOSTBUD_DB_PASSWORD"),
		DBSSLMode:   get("HOSTBUD_DB_SSLMODE", "disable"),

		TrustedProxies:  get("HOSTBUD_TRUSTED_PROXIES", DefaultTrustedProxies),
		AllowedTSUsers:  strings.TrimSpace(getenv("HOSTBUD_ALLOWED_TS_USERS")),
		TailscaleSocket: strings.TrimSpace(getenv("TAILSCALED_SOCKET")),
	}

	duration := func(key, def string, minimum time.Duration) time.Duration {
		d, err := time.ParseDuration(get(key, def))
		if err != nil || d < minimum {
			errs = append(errs, fmt.Errorf("%s: must be a duration of at least %s (like %s)", key, minimum, def))
		}
		return d
	}
	durationRange := func(key, def string, minimum, maximum time.Duration) time.Duration {
		d, err := time.ParseDuration(get(key, def))
		if err != nil || d < minimum || d > maximum {
			errs = append(errs, fmt.Errorf("%s: must be a duration between %s and %s (like %s)", key, minimum, maximum, def))
		}
		return d
	}
	count := func(key, def string) int {
		n, err := strconv.Atoi(get(key, def))
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("%s: must be a whole number of at least 1", key))
		}
		return n
	}
	countRange := func(key, def string, minimum, maximum int) int {
		n, err := strconv.Atoi(get(key, def))
		if err != nil || n < minimum || n > maximum {
			errs = append(errs, fmt.Errorf("%s: must be a whole number between %d and %d", key, minimum, maximum))
		}
		return n
	}
	cfg.ExecTimeout = durationRange("HOSTBUD_EXEC_TIMEOUT", "10s", 2*time.Second, 2*time.Minute)
	cfg.SFTPTimeout = durationRange("HOSTBUD_SFTP_TIMEOUT", "10s", 2*time.Second, 2*time.Minute)
	cfg.UploadTimeout = durationRange("HOSTBUD_UPLOAD_TIMEOUT", "5m", 30*time.Second, 10*time.Minute)
	cfg.RunStaleAfter = durationRange("HOSTBUD_RUN_STALE_AFTER", "2h", 10*time.Second, 24*time.Hour)
	cfg.MaxTerminalsPerUser = countRange("HOSTBUD_MAX_TERMINALS_PER_USER", "32", 1, 256)
	cfg.MaxTerminals = countRange("HOSTBUD_MAX_TERMINALS", "128", 1, 1024)
	cfg.SessionTTL = duration("HOSTBUD_SESSION_TTL", "720h", time.Minute)
	cfg.LoginMaxFailures = count("HOSTBUD_LOGIN_MAX_FAILURES", "5")
	cfg.RegisterMaxFailures = count("HOSTBUD_REGISTER_MAX_FAILURES", "10")
	cfg.IPMaxFailures = count("HOSTBUD_IP_MAX_FAILURES", "20")
	cfg.LoginBlockBase = duration("HOSTBUD_LOGIN_BLOCK_BASE", "30s", time.Second)
	cfg.LoginBlockMax = duration("HOSTBUD_LOGIN_BLOCK_MAX", "1h", time.Second)
	cfg.LoginFailureWindow = duration("HOSTBUD_LOGIN_FAILURE_WINDOW", "1h", time.Second)
	if cfg.LoginBlockMax < cfg.LoginBlockBase {
		errs = append(errs, errors.New("HOSTBUD_LOGIN_BLOCK_MAX: must not be below HOSTBUD_LOGIN_BLOCK_BASE"))
	}
	factor, err := strconv.ParseFloat(get("HOSTBUD_LOGIN_BLOCK_MULTIPLIER", "2"), 64)
	if err != nil || factor < 1 || factor > 10 {
		errs = append(errs, errors.New("HOSTBUD_LOGIN_BLOCK_MULTIPLIER: must be a number between 1 and 10"))
	}
	cfg.LoginBlockFactor = factor
	if _, err := auth.ParsePrefixes(cfg.TrustedProxies); err != nil {
		errs = append(errs, fmt.Errorf("HOSTBUD_TRUSTED_PROXIES: %w (use CIDRs like 172.16.0.0/12)", err))
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

	dbPort, err := strconv.Atoi(get("HOSTBUD_DB_PORT", "5432"))
	if err != nil || dbPort < 1 || dbPort > 65535 {
		errs = append(errs, errors.New("HOSTBUD_DB_PORT: must be a port number between 1 and 65535"))
	}
	cfg.DBPort = dbPort

	dbLocalPort, err := strconv.Atoi(get("HOSTBUD_DB_LOCAL_PORT", "9543"))
	if err != nil || dbLocalPort < 1 || dbLocalPort > 65535 {
		errs = append(errs, errors.New("HOSTBUD_DB_LOCAL_PORT: must be a port number between 1 and 65535"))
	}
	cfg.DBLocalPort = dbLocalPort
	if cfg.DBHost == "" || strings.ContainsAny(cfg.DBHost, " \t/@") {
		errs = append(errs, errors.New("HOSTBUD_DB_HOST: must be a bare hostname"))
	}
	if cfg.DBName == "" || strings.ContainsAny(cfg.DBName, " \t/@") {
		errs = append(errs, errors.New("HOSTBUD_DB_NAME: must be a plain database name"))
	}
	if cfg.DBUser == "" || strings.ContainsAny(cfg.DBUser, " \t/@") {
		errs = append(errs, errors.New("HOSTBUD_DB_USER: must be a plain database user"))
	}
	if cfg.DBSSLMode == "" {
		errs = append(errs, errors.New("HOSTBUD_DB_SSLMODE: must not be empty"))
	}

	if strings.Contains(cfg.Domain, "/") || strings.Contains(cfg.Domain, ":") {
		errs = append(errs, errors.New("HOSTBUD_DOMAIN: must be a bare hostname like hostbud.example.com (no scheme or port)"))
	}
	if strings.ContainsAny(cfg.HostAddr, " \t/@") {
		errs = append(errs, errors.New("HOSTBUD_HOST_ADDR: must be a bare hostname or IP like host.docker.internal"))
	}
	if strings.ContainsAny(cfg.HostSSHUser, " \t@") {
		errs = append(errs, errors.New("HOST_SSH_USER: must be a plain user name"))
	}
	if cfg.HostSSHUser == "" {
		errs = append(errs, errors.New("HOST_SSH_USER: required — the user hostbud logs in as on the host (see .env.example)"))
	}
	cfg.HookBaseURL = strings.TrimSpace(getenv("HOSTBUD_HOOK_BASE_URL"))
	if err := validHookBaseURL(cfg.HookBaseURL); err != nil {
		errs = append(errs, fmt.Errorf("HOSTBUD_HOOK_BASE_URL: %w", err))
	}
	cfg.HookBaseURL = strings.TrimSuffix(cfg.HookBaseURL, "/")
	switch get("HOSTBUD_PARALLEL_QUEUES", "false") {
	case "true":
		cfg.ParallelQueues = true
	case "false":
	default:
		errs = append(errs, errors.New("HOSTBUD_PARALLEL_QUEUES: must be true or false"))
	}

	return cfg, errors.Join(errs...)
}

// validHookBaseURL accepts "" or an absolute http(s) URL with a host and no
// path, query, fragment or credentials.
func validHookBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	bad := errors.New("must be empty or a base URL like http://127.0.0.1:9055 (http or https, no path, query or fragment)")
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.HasSuffix(raw, "?") || strings.HasSuffix(raw, "#") {
		return bad
	}
	return nil
}
