// Command hostbud serves the hostbud web app.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hostbud/internal/api"
	"hostbud/internal/config"
	"hostbud/internal/store"
	"hostbud/web"
)

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

	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	if _, err := st.EnsureHostMachine(ctx, cfg.HostLabel); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.New(log, web.Dist()),
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

// backup writes a consistent copy of the database to dest (make backup).
func backup(cfg config.Config, dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	return st.Backup(ctx, dest)
}
