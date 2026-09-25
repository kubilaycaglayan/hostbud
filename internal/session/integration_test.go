//go:build integration

package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/testenv"
)

func setup(t *testing.T) (*session.Service, *sshx.Client) {
	t.Helper()
	c := testenv.Connected(t, testenv.SSHD)
	testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -rf ~/sess-it; mkdir -p ~/sess-it/proj.one")
	inv := inventory.New(c, events.NewBus(), inventory.Options{MachineID: sshx.HostMachineID, Interval: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { inv.Run(ctx); close(done) }()
	t.Cleanup(func() {
		testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -rf ~/sess-it")
		cancel()
		<-done
	})
	rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rcancel()
	if err := inv.Refresh(rctx); err != nil {
		t.Fatal(err)
	}
	return session.New(c, map[string]session.Tracker{sshx.HostMachineID: inv}, nil), c
}

func display(t *testing.T, c *sshx.Client, name, format string) string {
	t.Helper()
	out, err := c.Exec(context.Background(), sshx.HostMachineID, "tmux", "display-message", "-p", "-t", "="+name+":", format)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestIntegrationCreateWithDefaults(t *testing.T) {
	svc, c := setup(t)
	name, err := svc.Create(context.Background(), session.Spec{Machine: sshx.HostMachineID, Path: "~/sess-it/proj.one"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "proj-one" {
		t.Fatalf("name = %q", name)
	}
	if p := display(t, c, name, "#{session_path}"); p != "/home/dev/sess-it/proj.one" {
		t.Fatalf("session_path = %q", p)
	}
	// Same directory again: suffixed name.
	again, err := svc.Create(context.Background(), session.Spec{Machine: sshx.HostMachineID, Path: "~/sess-it/proj.one"})
	if err != nil || again != "proj-one-2" {
		t.Fatalf("second create: %q, %v", again, err)
	}
}

func TestIntegrationCreateWithStartCommand(t *testing.T) {
	svc, c := setup(t)
	name, err := svc.Create(context.Background(), session.Spec{
		Machine: sshx.HostMachineID, Name: "sess-htop", Path: "~/sess-it", StartCommand: "htop",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for display(t, c, name, "#{pane_current_command}") != "htop" {
		if time.Now().After(deadline) {
			t.Fatalf("pane runs %q, want htop", display(t, c, name, "#{pane_current_command}"))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if p := display(t, c, name, "#{pane_current_path}"); p != "/home/dev/sess-it" {
		t.Fatalf("pane path = %q", p)
	}
}

func TestIntegrationErrorsMapped(t *testing.T) {
	svc, _ := setup(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "dup"}); err != nil {
		t.Fatal(err)
	}
	var e *session.Error
	_, err := svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "dup"})
	if !errors.As(err, &e) || e.Code != session.CodeDuplicate || !strings.Contains(e.Message, "dup") {
		t.Fatalf("duplicate: %+v", err)
	}
	_, err = svc.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "nopath", Path: "~/sess-it/nope"})
	if !errors.As(err, &e) || e.Code != session.CodePathNotFound || !strings.Contains(e.Message, "/home/dev/sess-it/nope") {
		t.Fatalf("missing path: %+v", err)
	}
	if err := svc.Rename(ctx, sshx.HostMachineID, "dup", "dup2"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Kill(ctx, sshx.HostMachineID, "dup"); !errors.As(err, &e) || e.Code != session.CodeNotFound {
		t.Fatalf("kill renamed-away session: %+v", err)
	}
	if err := svc.Kill(ctx, sshx.HostMachineID, "dup2"); err != nil {
		t.Fatal(err)
	}
}
