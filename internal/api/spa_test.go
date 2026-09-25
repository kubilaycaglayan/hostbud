package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func serve(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return rec
}

func builtDist() fstest.MapFS {
	return fstest.MapFS{
		".gitkeep":          {},
		"index.html":        {Data: []byte("<!doctype html><title>hostbud</title>")},
		"assets/app-abc.js": {Data: []byte("console.log(1)")},
		"favicon.svg":       {Data: []byte("<svg/>")},
	}
}

func TestSPA(t *testing.T) {
	h := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: builtDist()})

	tests := []struct {
		name, target string
		status       int
		body         string
		cache        string
	}{
		{"root serves index", "/", 200, "<title>hostbud</title>", "no-cache"},
		{"index.html serves index", "/index.html", 200, "<title>hostbud</title>", "no-cache"},
		{"client route falls back", "/sessions/main", 200, "<title>hostbud</title>", "no-cache"},
		{"hashed asset is immutable", "/assets/app-abc.js", 200, "console.log(1)", "public, max-age=31536000, immutable"},
		{"other static file", "/favicon.svg", 200, "<svg/>", ""},
		{"missing asset is 404", "/assets/missing.js", 404, "", ""},
		{"unknown api is 404", "/api/nope", 404, "", ""},
		{"unknown ws is 404", "/ws/nope", 404, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, h, http.MethodGet, tt.target)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if tt.body != "" && !strings.Contains(rec.Body.String(), tt.body) {
				t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), tt.body)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.cache {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.cache)
			}
		})
	}
}

func TestSPAPlaceholderWhenNotBuilt(t *testing.T) {
	h := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: fstest.MapFS{".gitkeep": {}}})

	for _, target := range []string{"/", "/sessions/main"} {
		rec := serve(t, h, http.MethodGet, target)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "web UI was not built") {
			t.Fatalf("%s: status = %d, body = %q", target, rec.Code, rec.Body.String())
		}
	}
}

func TestSPARejectsPost(t *testing.T) {
	h := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: builtDist(),
		Origins: AllowedOrigins("", 9055)})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
	req.Header.Set("Origin", "http://localhost:9055")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
