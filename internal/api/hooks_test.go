package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"hostbud/internal/events"
	"hostbud/internal/queue"
	"hostbud/internal/store"
)

type fakeHooks struct{ calls int }

func (f *fakeHooks) Receive(context.Context, string, string, string, io.Reader) error {
	f.calls++
	return nil
}

type hookRunStore struct{ runs map[string]store.Run }

func (s *hookRunStore) Run(_ context.Context, id string) (store.Run, error) {
	r, ok := s.runs[id]
	if !ok {
		return r, store.ErrNotFound
	}
	return r, nil
}

func (s *hookRunStore) AppendRunEvent(_ context.Context, runID, source, kind string, payload []byte) (store.RunEvent, error) {
	return store.RunEvent{RunID: runID, Source: source, Kind: kind, Payload: payload}, nil
}

// The whole middleware chain: the hook route needs no cookie and ignores the
// Origin header and the Tailscale gate; the bearer token decides.
func TestHookRouteThroughMiddleware(t *testing.T) {
	const run = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	token, hash, err := queue.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	ended, endedHash, _ := queue.NewToken()
	st := &hookRunStore{runs: map[string]store.Run{
		run:                          {ID: run, TokenHash: hash, Status: store.RunRunning},
		"01ARZ3NDEKTSV4RRFFQ69G5FAW": {ID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", TokenHash: endedHash, Status: store.RunAchieved},
	}}
	h := New(Config{
		Log: slog.New(slog.DiscardHandler), Dist: fstest.MapFS{}, Origins: AllowedOrigins("hostbud.example.com", 9055),
		Bus: events.NewBus(), Auth: &fakeAuth{}, Hooks: queue.NewHooks(st, nil, nil),
		// The Tailscale gate is on and has no identity for this caller.
		AllowedTSUsers: "owner@example.com",
	})
	post := func(path, auth, origin, contentType, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	body := `{"session_id":"s1"}`
	for _, c := range []struct {
		name, path, auth, origin, contentType, body string
		want                                        int
	}{
		{"accepted without cookie or Origin", "/api/hooks/" + run + "/session_start", "Bearer " + token, "", "application/json", body, 204},
		{"foreign Origin doesn't matter", "/api/hooks/" + run + "/turn_end", "Bearer " + token, "http://evil.example.com", "application/json", body, 204},
		{"no Content-Type still needs JSON", "/api/hooks/" + run + "/turn_end", "Bearer " + token, "", "", body, 204},
		{"no token", "/api/hooks/" + run + "/turn_end", "", "", "application/json", body, 401},
		{"ended run", "/api/hooks/01ARZ3NDEKTSV4RRFFQ69G5FAW/turn_end", "Bearer " + ended, "", "application/json", body, 410},
		{"unknown run", "/api/hooks/01ARZ3NDEKTSV4RRFFQ69G5FAX/turn_end", "Bearer " + token, "", "application/json", body, 404},
		{"unknown event", "/api/hooks/" + run + "/stop", "Bearer " + token, "", "application/json", body, 404},
		{"65 KiB", "/api/hooks/" + run + "/turn_end", "Bearer " + token, "", "application/json", `{"a":"` + strings.Repeat("x", 65<<10) + `"}`, 413},
		{"not JSON", "/api/hooks/" + run + "/turn_end", "Bearer " + token, "", "text/plain", "hello", 400},
	} {
		if rec := post(c.path, c.auth, c.origin, c.contentType, c.body); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		} else if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control %q", c.name, rec.Header().Get("Cache-Control"))
		}
	}
	// Other routes still need the cookie and the Origin: the exemption is
	// the hook route alone.
	if rec := post("/api/projects", "Bearer "+token, "", "application/json", `{}`); rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/projects with a run token = %d, want 403", rec.Code)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/hooks/"+run+"/turn_end", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusNoContent {
		t.Error("GET on the hook path is accepted")
	}
}
