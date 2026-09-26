//go:build integration

package projects_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/projects"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/testenv"
)

func projectTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	schema := fmt.Sprintf("project_it_%x", sha256.Sum256([]byte(root)))[:24]
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-password" //nolint:gosec // disposable integration database
	}
	s, err := store.Open(ctx, store.Config{Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: password, SSLMode: "disable", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestIntegrationProjectSessionPlacementRenameEndAndRecreate(t *testing.T) {
	ctx := context.Background()
	client := testenv.Connected(t, testenv.SSHD)
	// A missing server is the normal initial state on a throwaway target.
	_, _ = client.Exec(ctx, sshx.HostMachineID, "tmux", "kill-server")
	if _, err := client.Exec(ctx, sshx.HostMachineID, "mkdir", "-p", "/home/dev/projects-it/app", "/home/dev/projects-it/outside"); err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	projectEvents, stopProjectEvents := bus.Subscribe(16)
	defer stopProjectEvents()
	inv := inventory.New(client, bus, inventory.Options{MachineID: sshx.HostMachineID, Interval: time.Second})
	inventCtx, stopInventory := context.WithCancel(context.Background())
	inventDone := make(chan struct{})
	go func() { inv.Run(inventCtx); close(inventDone) }()
	t.Cleanup(func() {
		_, _ = client.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-server")
		_, _ = client.Exec(context.Background(), sshx.HostMachineID, "rm", "-rf", "/home/dev/projects-it")
		stopInventory()
		<-inventDone
	})
	refreshCtx, cancelRefresh := context.WithTimeout(ctx, 10*time.Second)
	if err := inv.Refresh(refreshCtx); err != nil {
		cancelRefresh()
		t.Fatal(err)
	}
	cancelRefresh()

	repo := projectTestStore(t)
	projectService := projects.New(repo, bus, nil)
	sessions := session.New(client, map[string]session.Tracker{sshx.HostMachineID: inv}, nil, projectService)
	projectService.SetSessionCreator(sessions)
	runCtx, stopProjects := context.WithCancel(ctx)
	runDone := make(chan struct{})
	go func() { projectService.Run(runCtx); close(runDone) }()
	t.Cleanup(func() { stopProjects(); <-runDone })

	p, err := projectService.Create(ctx, sshx.HostMachineID, "/home/dev/projects-it/app/", "App")
	if err != nil {
		t.Fatal(err)
	}
	eventDeadline := time.After(5 * time.Second)
	foundEvent := false
	for !foundEvent {
		select {
		case event := <-projectEvents:
			if event.Type != events.ProjectsChanged {
				continue
			}
			change, ok := event.Payload.(projects.Changed)
			if !ok || event.Machine != sshx.HostMachineID || change.Project.ID != p.ID {
				t.Fatalf("project mutation event = %+v", event)
			}
			foundEvent = true
		case <-eventDeadline:
			t.Fatal("project mutation was not published")
		}
	}
	name, err := projectService.CreateSession(ctx, p.ID, session.Spec{Name: "project-it", StartCommand: "sleep 300"})
	if err != nil || name != "project-it" {
		t.Fatalf("project session = %q, %v", name, err)
	}
	pathOutput, err := client.Exec(ctx, sshx.HostMachineID, "tmux", "display-message", "-p", "-t", "=project-it:", "#{session_path}")
	if err != nil || strings.TrimSpace(string(pathOutput)) != p.Path {
		t.Fatalf("created session path = %q, %v; want %q", strings.TrimSpace(string(pathOutput)), err, p.Path)
	}
	placement, err := projectService.Place(ctx, sshx.HostMachineID, name, p.Path)
	if err != nil || !placement.Matched || placement.ProjectID != p.ID {
		t.Fatalf("linked placement = %+v, %v", placement, err)
	}

	if err := sessions.Rename(ctx, sshx.HostMachineID, name, "project-renamed"); err != nil {
		t.Fatal(err)
	}
	if link, err := repo.SessionLink(ctx, sshx.HostMachineID, "project-renamed"); err != nil || link.ProjectID != p.ID {
		t.Fatalf("renamed link = %+v, %v", link, err)
	}
	if _, err := client.Exec(ctx, sshx.HostMachineID, "tmux", "kill-session", "-t", "=project-renamed"); err != nil {
		t.Fatal(err)
	}
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := repo.SessionLink(ctx, sshx.HostMachineID, "project-renamed")
		if errors.Is(err, store.ErrNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("out-of-band ended session link remains: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := sessions.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "project-it", Path: "/home/dev/projects-it/outside"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SessionLink(ctx, sshx.HostMachineID, "project-it"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unrelated reused session name inherited old link: %v", err)
	}
	placement, err = projectService.Place(ctx, sshx.HostMachineID, "project-it", "/home/dev/projects-it/outside")
	if err != nil || placement.Matched {
		t.Fatalf("recreated unrelated session placement = %+v, %v", placement, err)
	}
	if err := sessions.Kill(ctx, sshx.HostMachineID, "project-it"); err != nil {
		t.Fatal(err)
	}
}
