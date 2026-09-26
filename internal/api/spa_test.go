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
		".gitkeep":             {},
		"index.html":           {Data: []byte("<!doctype html><title>hostbud</title>")},
		"assets/app-abc.js":    {Data: []byte("console.log(1)")},
		"favicon.svg":          {Data: []byte("<svg/>")},
		"manifest.webmanifest": {Data: []byte(`{"name":"hostbud"}`)},
		"icons/icon-192.png":   {Data: []byte("png")},
		"sw.js":                {Data: []byte("// service worker")},
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
		{"favicon is not cached", "/favicon.svg", 200, "<svg/>", "no-cache"},
		{"manifest is not cached", "/manifest.webmanifest", 200, `{"name":"hostbud"}`, "no-cache"},
		{"icon is not cached", "/icons/icon-192.png", 200, "png", "no-cache"},
		{"service worker is not cached", "/sw.js", 200, "service worker", "no-cache"},
		{"missing asset is 404", "/assets/missing.js", 404, "", ""},
		{"unknown icon is 404", "/icons/missing.png", 404, "", ""},
		{"unknown root script is 404", "/missing.js", 404, "", ""},
		// Without a session, /api and /ws answer 401 before routing (fail closed).
		{"unknown api needs a session", "/api/nope", 401, "", "no-store"},
		{"unknown ws needs a session", "/ws/nope", 401, "", "no-store"},
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

func TestSPAManifestContentType(t *testing.T) {
	h := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: builtDist()})
	rec := serve(t, h, http.MethodGet, "/manifest.webmanifest")
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Fatalf("Content-Type = %q, want application/manifest+json", got)
	}
}

func TestSPASwContentType(t *testing.T) {
	h := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: builtDist()})
	rec := serve(t, h, http.MethodGet, "/sw.js")
	if got := rec.Header().Get("Content-Type"); got != "text/javascript" {
		t.Fatalf("Content-Type = %q, want text/javascript", got)
	}
}

func TestSPAStaticContentTypes(t *testing.T) {
	h := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: builtDist()})
	tests := map[string]string{
		"/manifest.webmanifest": "application/manifest+json",
		"/icons/icon-192.png":   "image/png",
		"/favicon.svg":          "image/svg+xml",
		"/assets/app-abc.js":    "text/javascript",
	}
	for target, want := range tests {
		t.Run(target, func(t *testing.T) {
			rec := serve(t, h, http.MethodGet, target)
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, want) {
				t.Fatalf("Content-Type = %q, want prefix %q", got, want)
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

func TestUnknownAPIRouteIs404WithSession(t *testing.T) {
	h := New(Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: builtDist(), Auth: &fakeAuth{}})
	for _, target := range []string{"/api/nope", "/ws/nope"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", target, rec.Code)
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
