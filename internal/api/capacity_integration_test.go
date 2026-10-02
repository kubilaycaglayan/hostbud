//go:build integration

package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"hostbud/internal/agents"
	"hostbud/internal/events"
	"hostbud/internal/queue"
	"hostbud/internal/store"
)

// V2-M2 T5: the capacity route against the real store: authenticated,
// Origin-checked (a refused write changes nothing), range-checked, and
// every change publishes queue.changed (no new event type).
func TestIntegrationCapacityRouteUsesPostgres(t *testing.T) {
	ctx := context.Background()
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-password" //nolint:gosec // disposable integration database
	}
	schema := fmt.Sprintf("cap_api_%x", sha256.Sum256([]byte(t.TempDir())))[:24]
	repo, err := store.Open(ctx, store.Config{Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: password, SSLMode: "disable", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	if _, err := repo.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	project, err := repo.CreateProject(ctx, store.HostMachineID, "/home/dev/app", "app")
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	changes, cancel := bus.Subscribe(100)
	defer cancel()
	svc := queue.NewService(repo, agents.NewRegistry(agents.NewClaude(nil, nil, nil), agents.NewCodex(nil, 0)), bus)
	svc.SetParallelQueues(true)
	if _, err := svc.Create(ctx, project.ID, "Alpha"); err != nil {
		t.Fatal(err)
	}
	for len(changes) > 0 {
		<-changes
	}
	server := httptest.NewServer(New(Config{
		Log: slog.New(slog.DiscardHandler), Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055), Bus: bus, Auth: &fakeAuth{}, Queues: svc,
	}))
	defer server.Close()
	call := func(method, path, body, originHeader string, signedIn bool) (int, string) {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if originHeader != "" {
			req.Header.Set("Origin", originHeader)
		}
		if signedIn {
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, strings.TrimSpace(string(data))
	}

	if status, body := call("GET", "/api/machines/host/capacity", "", "", true); status != 200 || body != `{"maxConcurrentRuns":null}` {
		t.Fatalf("GET default: %d %s", status, body)
	}
	if status, _ := call("GET", "/api/machines/host/capacity", "", "", false); status != http.StatusUnauthorized {
		t.Fatalf("GET signed out: %d", status)
	}
	if status, body := call("PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":3}`, origin, true); status != 200 || body != `{"maxConcurrentRuns":3}` {
		t.Fatalf("PUT 3: %d %s", status, body)
	}
	if c, _ := repo.MachineCapacity(ctx, store.HostMachineID); c == nil || *c != 3 {
		t.Fatalf("stored cap %v", c)
	}
	published := 0
	for len(changes) > 0 {
		if e := <-changes; e.Type == events.QueueChanged {
			published++
		} else {
			t.Errorf("unexpected event type %s", e.Type)
		}
	}
	if published != 1 {
		t.Fatalf("queue.changed after a cap change: %d, want one per queue (1)", published)
	}
	for _, bad := range []string{`{"maxConcurrentRuns":0}`, `{"maxConcurrentRuns":33}`, `{"maxConcurrentRuns":2.5}`, `{"maxConcurrentRuns":"2"}`, `{}`} {
		if status, body := call("PUT", "/api/machines/host/capacity", bad, origin, true); status != http.StatusBadRequest {
			t.Errorf("PUT %s: %d %s", bad, status, body)
		}
	}
	for _, o := range []string{"http://evil.example.com", ""} {
		if status, _ := call("PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":1}`, o, true); status != http.StatusForbidden {
			t.Errorf("Origin %q: %d", o, status)
		}
	}
	if status, _ := call("PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":1}`, origin, false); status != http.StatusUnauthorized {
		t.Errorf("PUT signed out: %d", status)
	}
	if c, _ := repo.MachineCapacity(ctx, store.HostMachineID); c == nil || *c != 3 {
		t.Fatalf("a refused request changed the cap: %v", c)
	}
	if status, body := call("PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":null}`, origin, true); status != 200 || body != `{"maxConcurrentRuns":null}` {
		t.Fatalf("PUT null: %d %s", status, body)
	}
	if status, body := call("GET", "/api/queues", "", "", true); status != 200 || !strings.Contains(body, `"parallelQueues":true`) {
		t.Fatalf("GET /api/queues: %d %s", status, body)
	}

	// The parallel-queues switch: stored, reported, and a refused request
	// changes nothing.
	if status, _ := call("PUT", "/api/machines/host/parallel-queues", `{"parallelQueues":false}`, "http://evil.example.com", true); status != http.StatusForbidden {
		t.Fatalf("switch with a bad Origin: %d", status)
	}
	if v, _ := repo.ParallelQueuesSetting(ctx, store.HostMachineID); v != nil {
		t.Fatalf("a refused request stored the switch: %v", *v)
	}
	if status, body := call("PUT", "/api/machines/host/parallel-queues", `{"parallelQueues":false}`, origin, true); status != 200 || body != `{"parallelQueues":false}` {
		t.Fatalf("switch off: %d %s", status, body)
	}
	if v, _ := repo.ParallelQueuesSetting(ctx, store.HostMachineID); v == nil || *v {
		t.Fatalf("stored switch %v", v)
	}
	if status, body := call("GET", "/api/queues", "", "", true); status != 200 || !strings.Contains(body, `"parallelQueues":false`) {
		t.Fatalf("GET /api/queues after switch off: %d %s", status, body)
	}
}
