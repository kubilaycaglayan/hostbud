// Command hostbud serves the hostbud web app.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"hostbud/internal/agents"
	"hostbud/internal/api"
	"hostbud/internal/auth"
	"hostbud/internal/config"
	"hostbud/internal/events"
	"hostbud/internal/fsbrowse"
	"hostbud/internal/inventory"
	"hostbud/internal/notify"
	"hostbud/internal/projects"
	"hostbud/internal/queue"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/term"
	"hostbud/internal/tmux"
	"hostbud/internal/tsauth"
	"hostbud/web"
)

// hostKeysDir is where compose mounts the host's /etc/ssh/ssh_host_*_key.pub.
const hostKeysDir = "/run/host-keys"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hostbud:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if len(os.Args) != 2 {
			return errors.New("usage: hostbud healthcheck")
		}
		return runHealthcheck()
	}
	if len(os.Args) > 1 && os.Args[1] == "vapid-keys" {
		return printVAPIDKeys(os.Stdout)
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "backup":
			if len(os.Args) != 3 {
				return errors.New("usage: hostbud backup <dest-file>")
			}
			return backup(cfg, os.Args[2])
		case "restore-check":
			if len(os.Args) != 3 {
				return errors.New("usage: hostbud restore-check <dump-file>")
			}
			return restoreCheck(cfg, os.Args[2])
		default:
			return fmt.Errorf("unknown command %q (commands: backup, restore-check, healthcheck, vapid-keys)", os.Args[1])
		}
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)
	tsLogin, err := makeTSLogin(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, store.Config{
		Host: cfg.DBHost, Port: cfg.DBPort, Name: cfg.DBName, User: cfg.DBUser,
		Password: cfg.DBPassword, SSLMode: cfg.DBSSLMode,
	})
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	if _, err := st.EnsureHostMachine(ctx, cfg.HostLabel); err != nil {
		return err
	}

	// Pins the host's key from the read-only mounted public host keys.
	deps, err := buildDeps(cfg)
	if err != nil {
		return err
	}
	ssh := deps.ssh
	log.Debug("ssh config written", "path", ssh.ConfigPath())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = ssh.Close(ctx)
	}()
	filesystem := deps.filesystem
	defer func() { _ = filesystem.Close() }()

	// Track the host's tmux sessions; every change goes out on the bus.
	bus := events.NewBus()
	projectService := projects.New(st, bus, log)
	projectDone := make(chan struct{})
	go func() { projectService.Run(ctx); close(projectDone) }()
	defer func() { <-projectDone }()
	inv := inventory.New(ssh, bus, inventory.Options{
		MachineID: store.HostMachineID, Label: cfg.HostLabel, Interval: cfg.PollInterval,
		Store: capabilityStore{st}, Log: log,
	})
	invDone := make(chan struct{})
	go func() { inv.Run(ctx); close(invDone) }()
	defer func() { <-invDone }()

	sessions := session.New(ssh, map[string]session.Tracker{store.HostMachineID: inv}, log, projectService)
	projectService.SetSessionCreator(sessions)

	authKey, err := loadOrCreateKey(filepath.Join(cfg.DataDir, "auth-key"))
	if err != nil {
		return err
	}
	proxies, err := auth.ParsePrefixes(cfg.TrustedProxies)
	if err != nil {
		return err
	}
	accounts, err := auth.New(st, auth.Config{
		SessionTTL: cfg.SessionTTL,
		Limits: auth.Limits{
			LoginMax: cfg.LoginMaxFailures, RegisterMax: cfg.RegisterMaxFailures, IPMax: cfg.IPMaxFailures,
			BlockBase: cfg.LoginBlockBase, BlockMax: cfg.LoginBlockMax, Multiplier: cfg.LoginBlockFactor,
			Window: cfg.LoginFailureWindow,
		},
		Key: authKey, Log: log,
	})
	if err != nil {
		return err
	}
	origins := api.AllowedOrigins(cfg.Domain, cfg.LocalPort)
	csp, err := api.BuildContentSecurityPolicy(web.Dist(), origins)
	if err != nil {
		return fmt.Errorf("configure Content-Security-Policy: %w", err)
	}

	// v2 agent queue: adapters, the queue service and the run hooks
	// (docs/roadmap-v2/ARCHITECTURE.md).
	hostHome := func(string) string { m, _ := inv.Snapshot(); return m.Home }
	adapters := agents.NewRegistry(
		agents.NewClaude(ssh, func(string) agents.Files { return filesystem }, hostHome),
		agents.NewCodex(ssh, cfg.ExecTimeout),
	)
	queues := queue.NewService(st, adapters, bus)
	queues.SetParallelQueues(cfg.ParallelQueues)
	if err := queues.LoadParallelQueues(ctx); err != nil {
		log.Warn("parallel-queues setting unreadable; using HOSTBUD_PARALLEL_QUEUES", "err", err)
	}
	starter := queue.NewStarter(st, sessions, cfg.HookURL(), log)
	dispatcher := queue.NewDispatcher(st, adapters, starter, queues, bus, cfg.RunStaleAfter, log)
	dispatchDone := make(chan struct{})
	go func() { dispatcher.Run(ctx); close(dispatchDone) }()
	defer func() { <-dispatchDone }()
	hooks := queue.NewHooks(st, dispatcher, log)

	// V2-M3 notifications: missing or invalid VAPID keys only turn push off.
	push := cfg.Push()
	if !push.Available {
		log.Warn("web push is off; in-app notifications still work", "missing", push.Missing, "invalid", push.Invalid, "fix", "make vapid-keys")
	}
	notifier := notify.New(st, push, cfg.VAPIDPublicKey, log)
	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: api.New(api.Config{
			Hooks:         hooks,
			Queues:        queues,
			Notifications: notifier,
			Log:           log, Dist: web.Dist(),
			ExecTimeout:           cfg.ExecTimeout,
			SFTPTimeout:           cfg.SFTPTimeout,
			UploadTimeout:         cfg.UploadTimeout,
			DBPing:                st.Ping,
			Origins:               origins,
			ContentSecurityPolicy: csp,
			Bus:                   bus,
			Machines:              []api.Snapshotter{inv},
			Sessions:              sessions,
			Projects:              projectService,
			FileSystem:            filesystem,
			Terminal:              &term.Handler{SSH: ssh, Log: log, Shutdown: ctx.Done(), MaxPerUser: cfg.MaxTerminalsPerUser, MaxTotal: cfg.MaxTerminals, AttachTimeout: cfg.ExecTimeout, AccountID: api.AuthenticatedUserID, TmuxVersion: hostTmuxVersion(inv)},
			UIState:               st,
			Auth:                  accounts,
			TrustedProxies:        proxies,
			AllowedTSUsers:        cfg.AllowedTSUsers,
			TSLogin:               tsLogin,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    32 << 10,
		IdleTimeout:       2 * time.Minute,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("hostbud listening", "addr", cfg.Listen)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type runtimeDeps struct {
	ssh        *sshx.Client
	filesystem *fsbrowse.Service
}

func makeTSLogin(cfg config.Config) (func(context.Context, string) (string, error), error) {
	if cfg.AllowedTSUsers == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", cfg.TailscaleSocket)
	if err != nil {
		return nil, fmt.Errorf("HOSTBUD_ALLOWED_TS_USERS requires a readable tailscaled socket at TAILSCALED_SOCKET and deploy/compose.tailscale.yml: %w", err)
	}
	_ = conn.Close()
	client := tsauth.New(cfg.TailscaleSocket)
	cache := tsauth.NewCache(client.Login)
	return func(ctx context.Context, ip string) (string, error) {
		return cache.LoginFor(ctx, ip, cfg.AllowedTSUsers)
	}, nil
}

// buildDeps wires the configured remote-call bounds into the host clients.
func buildDeps(cfg config.Config) (runtimeDeps, error) {
	return buildDepsAt(cfg, hostKeysDir)
}

// buildDepsAt keeps the wiring testable with an isolated host-key fixture.
func buildDepsAt(cfg config.Config, keysDir string) (runtimeDeps, error) {
	client, err := sshx.New(sshx.Config{
		Dir: filepath.Join(cfg.DataDir, "ssh"), HostKeysDir: keysDir,
		HostAddr: cfg.HostAddr, HostUser: cfg.HostSSHUser, Timeout: cfg.ExecTimeout,
	})
	if err != nil {
		return runtimeDeps{}, err
	}
	filesystem := fsbrowse.New(client, store.HostMachineID, fsbrowse.DefaultIdleTimeout, cfg.SFTPTimeout, cfg.UploadTimeout)
	return runtimeDeps{ssh: client, filesystem: filesystem}, nil
}

// backup writes a consistent copy of the database to dest (make backup).
func backup(cfg config.Config, dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	st, err := store.Open(ctx, store.Config{
		Host: cfg.DBHost, Port: cfg.DBPort, Name: cfg.DBName, User: cfg.DBUser,
		Password: cfg.DBPassword, SSLMode: cfg.DBSSLMode,
	})
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	return st.Backup(ctx, dest)
}

func restoreCheck(cfg config.Config, dump string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := store.RestoreCheck(ctx, store.Config{Host: cfg.DBHost, Port: cfg.DBPort, Name: cfg.DBName, User: cfg.DBUser, Password: cfg.DBPassword, SSLMode: cfg.DBSSLMode}, dump)
	if err != nil {
		return err
	}
	fmt.Printf("Restore check passed: migration=%d users=%d allowlist=%d projects=%d ui_state=%d\n", result.Version, result.Users, result.Allowlist, result.Projects, result.UIState)
	return nil
}

// capabilityStore saves the inventory's probe results on the machine row.
type capabilityStore struct{ st *store.Store }

func (c capabilityStore) SaveCapabilities(ctx context.Context, id string, caps inventory.Capabilities, seen time.Time) error {
	return c.st.SaveCapabilities(ctx, id, store.Capabilities{
		OS: caps.OS, Home: caps.Home, TmuxVersion: caps.TmuxVersion, TmuxMissing: caps.TmuxMissing,
	}, seen)
}

// loadOrCreateKey returns the install-local secret that keys rate-limit
// bucket hashes, creating it (0600) on first start.
func loadOrCreateKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 { //nolint:gosec // path under the data dir
		return b, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	return b, nil
}

// hostTmuxVersion reports the host's tmux version from the inventory (zero
// for other machines or before the first probe), for the attach flags.
func hostTmuxVersion(inv *inventory.Inventory) func(machine string) tmux.Version {
	return func(machine string) tmux.Version {
		if machine != store.HostMachineID {
			return tmux.Version{}
		}
		m, _ := inv.Snapshot()
		v, _ := tmux.ParseVersion(m.TmuxVersion)
		return v
	}
}
