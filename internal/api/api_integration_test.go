//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/testenv"
)

type createBarrierExecutor struct {
	inner   inventory.Executor
	name    string
	mu      sync.Mutex
	arrived int
	ready   chan struct{}
}

func (e *createBarrierExecutor) Exec(ctx context.Context, machine string, args ...string) ([]byte, error) {
	if len(args) > 4 && args[0] == "tmux" && args[1] == "new-session" && slices.Contains(args, "-s") && slices.Contains(args, e.name) {
		e.mu.Lock()
		e.arrived++
		if e.arrived == 2 {
			close(e.ready)
		}
		e.mu.Unlock()
		select {
		case <-e.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return e.inner.Exec(ctx, machine, args...)
}

// Two creates racing for one typed name both succeed: tmux refuses the
// second, and the session service retries it as name-1 (M8 T10 rule).
func TestIntegrationConcurrentTypedSessionCreateNumbersTheLoser(t *testing.T) {
	client := testenv.Connected(t, testenv.SSHD)
	_, _ = client.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-server")
	t.Cleanup(func() { _, _ = client.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-server") })
	tracker := host()
	const name = "concurrent-create"
	executor := &createBarrierExecutor{inner: client, name: name, ready: make(chan struct{})}
	sessions := session.New(executor, session.Trackers{sshx.HostMachineID: tracker}, nil)
	handler := New(Config{
		Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055), Bus: events.NewBus(),
		Machines: []Snapshotter{tracker}, Sessions: sessions, Auth: &fakeAuth{},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	type result struct {
		status int
		name   string
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/api/machines/host/sessions", strings.NewReader(`{"name":"`+name+`","path":"/home/dev"}`))
			if err != nil {
				results <- result{}
				return
			}
			req.Header.Set("Origin", origin)
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
			response, err := server.Client().Do(req)
			if err != nil {
				results <- result{}
				return
			}
			defer func() { _ = response.Body.Close() }()
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(response.Body).Decode(&body)
			results <- result{response.StatusCode, body.Name}
		}()
	}
	a, b := <-results, <-results
	names := []string{a.name, b.name}
	slices.Sort(names)
	if a.status != http.StatusCreated || b.status != http.StatusCreated || !slices.Equal(names, []string{name, name + "-1"}) {
		t.Fatalf("concurrent creates = %+v, %+v; want 201 for %q and %q", a, b, name, name+"-1")
	}
}
