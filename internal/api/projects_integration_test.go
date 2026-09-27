//go:build integration

package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/projects"
	"hostbud/internal/store"
)

func TestIntegrationDeleteProjectRouteUsesPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	schema := fmt.Sprintf("project_api_it_%x", sha256.Sum256([]byte(root)))[:24]
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-password" //nolint:gosec // disposable integration database
	}
	repo, err := store.Open(ctx, store.Config{Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: password, SSLMode: "disable", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	if _, err := repo.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	project, err := repo.CreateProject(ctx, "host", "/home/dev/project-api-delete", "Delete me")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RememberRecentCommand(ctx, project.ID, "echo disposable"); err != nil {
		t.Fatal(err)
	}
	service := projects.New(repo, events.NewBus(), nil)
	e := newEnv(t)
	e.h = New(Config{Dist: fstest.MapFS{}, Origins: AllowedOrigins("hostbud.example.com", 9055), Auth: &fakeAuth{}, Projects: service})

	response := e.do(t, "DELETE", "/api/projects/"+project.ID, "", nil)
	if response.Code != 204 {
		t.Fatalf("DELETE route = %d %s; want 204", response.Code, response.Body)
	}
	if _, err := repo.Project(ctx, project.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("project after routed delete = %v; want not found", err)
	}
	if commands, err := repo.RecentCommands(ctx, project.ID); err != nil || len(commands) != 0 {
		t.Fatalf("recent commands after routed delete = %+v, %v; want empty", commands, err)
	}
}
