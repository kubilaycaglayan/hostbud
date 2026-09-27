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
	if _, err := client.Exec(ctx, sshx.HostMachineID, "mkdir", "-p", "/home/dev/projects-it/app/nested", "/home/dev/projects-it/application", "/home/dev/projects-it/outside"); err != nil {
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
		_, _ = client.Exec(context.Background(), sshx.HostMachineID, "rm", "-rf", "/home/dev/projects-it", "/home/dev/projects-it-unrelated")
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
	parent, err := projectService.Create(ctx, sshx.HostMachineID, "/home/dev/projects-it", "Renamed parent")
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := projectService.Create(ctx, sshx.HostMachineID, "/home/dev/projects-it/application", "Similar display name")
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
	startCommand := `printf '%s\n' 'literal; printf injected'; sleep 300`
	name, err := projectService.CreateSession(ctx, p.ID, session.Spec{Name: "project-it", StartCommand: startCommand})
	if err != nil || name != "project-it" {
		t.Fatalf("project session = %q, %v", name, err)
	}
	numbered, err := projectService.CreateSession(ctx, p.ID, session.Spec{Name: "project-it"})
	if err != nil || numbered != "project-it-1" {
		t.Fatalf("taken project session = %q, %v; want project-it-1", numbered, err)
	}
	if link, err := repo.SessionLink(ctx, sshx.HostMachineID, numbered); err != nil || link.ProjectID != p.ID {
		t.Fatalf("numbered session link = %+v, %v; want project %s", link, err, p.ID)
	}
	pathOutput, err := client.Exec(ctx, sshx.HostMachineID, "tmux", "display-message", "-p", "-t", "=project-it:", "#{session_path}")
	if err != nil || strings.TrimSpace(string(pathOutput)) != p.Path {
		t.Fatalf("created session path = %q, %v; want %q", strings.TrimSpace(string(pathOutput)), err, p.Path)
	}
	commandOutput, err := client.Exec(ctx, sshx.HostMachineID, "tmux", "display-message", "-p", "-t", "=project-it:", "#{pane_current_command}")
	if err != nil || strings.TrimSpace(string(commandOutput)) != "sleep" {
		t.Fatalf("project session command = %q, %v; want sleep", strings.TrimSpace(string(commandOutput)), err)
	}
	captured, err := client.Exec(ctx, sshx.HostMachineID, "tmux", "capture-pane", "-p", "-J", "-t", "=project-it:")
	if err != nil || !strings.Contains(string(captured), "literal; printf injected") {
		t.Fatalf("project command output = %q, %v", strings.TrimSpace(string(captured)), err)
	}
	for _, line := range strings.Split(string(captured), "\n") {
		if strings.TrimSpace(line) == "injected" {
			t.Fatalf("command separator escaped its quoted argument: %q", string(captured))
		}
	}
	recentCommands, err := repo.RecentCommands(ctx, p.ID)
	if err != nil || len(recentCommands) != 1 || recentCommands[0].Command != startCommand {
		t.Fatalf("project recent commands = %+v, %v", recentCommands, err)
	}
	// Explicit links remain authoritative even if another persisted project is
	// a longer path match. This protects a session association from path drift.
	if err := repo.UpsertSessionLink(ctx, sshx.HostMachineID, name, parent.ID); err != nil {
		t.Fatal(err)
	}
	placement, err := projectService.Place(ctx, sshx.HostMachineID, name, p.Path)
	if err != nil || !placement.Matched || placement.ProjectID != parent.ID {
		t.Fatalf("explicit link precedence = %+v, %v; want %s", placement, err, parent.ID)
	}

	// Exercise path-component matching against persisted PostgreSQL projects and
	// real sessions discovered from the throwaway sshd target.
	for _, tc := range []struct {
		name, path, wantProject string
	}{
		{"prefix-nested", "/home/dev/projects-it/app/nested", p.ID},
		{"prefix-sibling", "/home/dev/projects-it/application", sibling.ID},
		{"prefix-parent", "/home/dev/projects-it", parent.ID},
		{"prefix-outside", "/home/dev/projects-it/outside", parent.ID},
	} {
		if _, err := sessions.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: tc.name, Path: tc.path}); err != nil {
			t.Fatal(err)
		}
	}
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path, wantProject string
	}{
		{"prefix-nested", "/home/dev/projects-it/app/nested", p.ID},
		{"prefix-sibling", "/home/dev/projects-it/application", sibling.ID},
		{"prefix-parent", "/home/dev/projects-it", parent.ID},
		{"prefix-outside", "/home/dev/projects-it/outside", parent.ID},
	} {
		got, err := projectService.Place(ctx, sshx.HostMachineID, tc.name, tc.path)
		if err != nil || !got.Matched || got.ProjectID != tc.wantProject {
			t.Errorf("persisted project placement for %s = %+v, %v; want %s", tc.name, got, err, tc.wantProject)
		}
	}

	// Saving an unmatched session as a project only adds metadata; its tmux
	// identity and the inventory's live-session record must remain untouched.
	const savedSessionName = "save-as-project-it"
	const savedSessionPath = "/home/dev/projects-it-recreated"
	if _, err := client.Exec(ctx, sshx.HostMachineID, "mkdir", "-p", savedSessionPath); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: savedSessionName, Path: savedSessionPath}); err != nil {
		t.Fatal(err)
	}
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	_, beforeSessions := inv.Snapshot()
	var beforeSavedSession string
	for _, live := range beforeSessions {
		if live.Name == savedSessionName {
			beforeSavedSession = live.ID + ":" + live.Path
		}
	}
	if beforeSavedSession == "" {
		t.Fatalf("unmatched session %q missing from inventory: %+v", savedSessionName, beforeSessions)
	}
	savedProject, err := projectService.Create(ctx, sshx.HostMachineID, savedSessionPath, "")
	if err != nil {
		t.Fatal(err)
	}
	_, afterSessions := inv.Snapshot()
	var afterSavedSession string
	for _, live := range afterSessions {
		if live.Name == savedSessionName {
			afterSavedSession = live.ID + ":" + live.Path
		}
	}
	if afterSavedSession != beforeSavedSession {
		t.Fatalf("save as project changed live session from %q to %q", beforeSavedSession, afterSavedSession)
	}
	savedPlacement, err := projectService.Place(ctx, sshx.HostMachineID, savedSessionName, savedSessionPath)
	if err != nil || !savedPlacement.Matched || savedPlacement.ProjectID != savedProject.ID {
		t.Fatalf("saved session placement = %+v, %v", savedPlacement, err)
	}
	// A raw string-prefix implementation would incorrectly choose /app for
	// this sibling path; the component boundary must select /application.
	got, err := projectService.Place(ctx, sshx.HostMachineID, "prefix-sibling", "/home/dev/projects-it/application")
	if err != nil || got.ProjectID == p.ID {
		t.Errorf("sibling path matched /app project: %+v, %v", got, err)
	}

	if err := sessions.Rename(ctx, sshx.HostMachineID, name, "project-renamed"); err != nil {
		t.Fatal(err)
	}
	if link, err := repo.SessionLink(ctx, sshx.HostMachineID, "project-renamed"); err != nil || link.ProjectID != parent.ID {
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

	if _, err := client.Exec(ctx, sshx.HostMachineID, "mkdir", "-p", "/home/dev/projects-it-unrelated"); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: "project-it", Path: "/home/dev/projects-it-unrelated"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SessionLink(ctx, sshx.HostMachineID, "project-it"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unrelated reused session name inherited old link: %v", err)
	}
	placement, err = projectService.Place(ctx, sshx.HostMachineID, "project-it", "/home/dev/projects-it-unrelated")
	if err != nil || placement.Matched {
		t.Fatalf("recreated unrelated session placement = %+v, %v", placement, err)
	}
	if err := sessions.Kill(ctx, sshx.HostMachineID, "project-it"); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationDeleteProjectCascadesMetadataAndKeepsLiveSession(t *testing.T) {
	ctx := context.Background()
	client := testenv.Connected(t, testenv.SSHD)
	if _, err := client.Exec(ctx, sshx.HostMachineID, "mkdir", "-p", "/home/dev/project-delete-it/app"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = client.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-session", "-t", "=project-delete-it-session")
		_, _ = client.Exec(context.Background(), sshx.HostMachineID, "rm", "-rf", "/home/dev/project-delete-it")
	}()

	repo := projectTestStore(t)
	parent, err := repo.CreateProject(ctx, sshx.HostMachineID, "/home/dev/project-delete-it", "parent")
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.CreateProject(ctx, sshx.HostMachineID, "/home/dev/project-delete-it/app", "child")
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.CreateProject(ctx, sshx.HostMachineID, "/home/dev", "unrelated")
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	service := projects.New(repo, bus, nil)
	if _, err := client.Exec(ctx, sshx.HostMachineID, "tmux", "new-session", "-d", "-s", "project-delete-it-session", "-c", child.Path); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSessionLink(ctx, sshx.HostMachineID, "project-delete-it-session", child.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.RememberRecentCommand(ctx, child.ID, "sleep 300"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RememberRecentCommand(ctx, other.ID, "echo keep"); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Project(ctx, child.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted project lookup = %v; want not found", err)
	}
	if _, err := repo.SessionLink(ctx, sshx.HostMachineID, "project-delete-it-session"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted project's session link = %v; want cascade", err)
	}
	if commands, err := repo.RecentCommands(ctx, child.ID); err != nil || len(commands) != 0 {
		t.Fatalf("deleted project's recent commands = %+v, %v; want empty", commands, err)
	}
	if commands, err := repo.RecentCommands(ctx, other.ID); err != nil || len(commands) != 1 || commands[0].Command != "echo keep" {
		t.Fatalf("unrelated project's recent commands = %+v, %v", commands, err)
	}
	if _, err := repo.Project(ctx, parent.ID); err != nil {
		t.Fatalf("parent project was changed: %v", err)
	}
	placement, err := service.Place(ctx, sshx.HostMachineID, "project-delete-it-session", child.Path)
	if err != nil || !placement.Matched || placement.ProjectID != parent.ID {
		t.Fatalf("deleted session placement = %+v, %v; want parent %s", placement, err, parent.ID)
	}
	if output, err := client.Exec(ctx, sshx.HostMachineID, "tmux", "display-message", "-p", "-t", "=project-delete-it-session:", "#{session_name}"); err != nil || strings.TrimSpace(string(output)) != "project-delete-it-session" {
		t.Fatalf("live tmux session after project deletion = %q, %v", strings.TrimSpace(string(output)), err)
	}
}
