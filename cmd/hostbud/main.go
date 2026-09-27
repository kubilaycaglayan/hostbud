// Command hostbud serves the hostbud web app.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"hostbud/internal/api"
	"hostbud/internal/auth"
	"hostbud/internal/config"
	"hostbud/internal/events"
	"hostbud/internal/fsbrowse"
	"hostbud/internal/inventory"
	"hostbud/internal/projects"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/term"
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
		default:
			return fmt.Errorf("unknown command %q (commands: backup)", os.Args[1])
		}
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

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

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: api.New(api.Config{
			Log: log, Dist: web.Dist(),
			ExecTimeout:    cfg.ExecTimeout,
			SFTPTimeout:    cfg.SFTPTimeout,
			Origins:        api.AllowedOrigins(cfg.Domain, cfg.LocalPort),
			Bus:            bus,
			Machines:       []api.Snapshotter{inv},
			Sessions:       sessions,
			Projects:       projectService,
			FileSystem:     filesystem,
			Terminal:       &term.Handler{SSH: ssh, Log: log, Shutdown: ctx.Done(), MaxPerUser: cfg.MaxTerminalsPerUser, MaxTotal: cfg.MaxTerminals, AttachTimeout: cfg.ExecTimeout},
			UIState:        st,
			Auth:           accounts,
			TrustedProxies: proxies,
		}),
		ReadHeaderTimeout: 10 * time.Second,
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
	filesystem := fsbrowse.New(client, store.HostMachineID, fsbrowse.DefaultIdleTimeout, cfg.SFTPTimeout)
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
