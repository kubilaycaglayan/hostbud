//go:build integration

package api

import (
	"context"
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

func TestIntegrationConcurrentTypedSessionCreateReturnsConflict(t *testing.T) {
	client := testenv.Connected(t, testenv.SSHD)
	_, _ = client.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-server")
	t.Cleanup(func() { _, _ = client.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-server") })
	tracker := host()
	const name = "concurrent-create"
	executor := &createBarrierExecutor{inner: client, name: name, ready: make(chan struct{})}
	sessions := session.New(executor, map[string]session.Tracker{sshx.HostMachineID: tracker}, nil)
	handler := New(Config{
		Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055), Bus: events.NewBus(),
		Machines: []Snapshotter{tracker}, Sessions: sessions, Auth: &fakeAuth{},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	statuses := make(chan int, 2)
	for range 2 {
		go func() {
			req, err := http.NewRequest(http.MethodPost, server.URL+"/api/machines/host/sessions", strings.NewReader(`{"name":"`+name+`","path":"/home/dev"}`))
			if err != nil {
				statuses <- 0
				return
			}
			req.Header.Set("Origin", origin)
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
			response, err := server.Client().Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			_ = response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	got := []int{<-statuses, <-statuses}
	slices.Sort(got)
	if got[0] != http.StatusCreated || got[1] != http.StatusConflict {
		t.Fatalf("concurrent create statuses = %v, want [%d %d]", got, http.StatusCreated, http.StatusConflict)
	}
}
