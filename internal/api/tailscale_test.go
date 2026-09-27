package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/fstest"
)

func TestTailscaleIdentityGatesDomainButExemptsLocalAndHealth(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	calls := 0
	cfg := Config{Log: slog.New(slog.DiscardHandler), AllowedTSUsers: "owner@example.com", TrustedProxies: proxies, TSLogin: func(_ context.Context, ip string) (string, error) {
		calls++
		if ip != "198.51.100.9" {
			t.Fatalf("client ip=%s", ip)
		}
		return "stranger@example.com", nil
	}}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	h := tailscaleIdentity(cfg, next)
	request := func(path, via string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), "GET", path, nil)
		r.RemoteAddr = "127.0.0.1:8000"
		r.Header.Set("X-Hostbud-Via", via)
		r.Header.Set("X-Forwarded-For", "198.51.100.9")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if got := request("/api/machines", "domain"); got.Code != 403 || !strings.Contains(got.Body.String(), "isn't allowed") {
		t.Fatalf("domain response=%d %s", got.Code, got.Body.String())
	}
	if got := request("/", "domain"); got.Code != 403 || !strings.Contains(got.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("SPA response=%d", got.Code)
	}
	if got := request("/api/health", "domain"); got.Code != 204 {
		t.Fatalf("health response=%d", got.Code)
	}
	if got := request("/api/machines", "local"); got.Code != 204 {
		t.Fatalf("local response=%d", got.Code)
	}
	if calls != 2 {
		t.Fatalf("whois calls=%d", calls)
	}
}

func TestTailscaleUntrustedHeaderIsIgnored(t *testing.T) {
	cfg := Config{Log: slog.New(slog.DiscardHandler), AllowedTSUsers: "owner@example.com", TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, TSLogin: func(context.Context, string) (string, error) { return "stranger@example.com", nil }}
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	r.RemoteAddr = "192.0.2.1:5000"
	r.Header.Set("X-Hostbud-Via", "local")
	w := httptest.NewRecorder()
	tailscaleIdentity(cfg, http.NotFoundHandler()).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("untrusted local header bypassed check: %d", w.Code)
	}
}

func TestTailscaleMissingSiteMarkerDefaultsToDomain(t *testing.T) {
	cfg := Config{Log: slog.New(slog.DiscardHandler), AllowedTSUsers: "owner@example.com", TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, TSLogin: func(context.Context, string) (string, error) { return "stranger@example.com", nil }}
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/api/machines", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	w := httptest.NewRecorder()
	tailscaleIdentity(cfg, http.NotFoundHandler()).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("missing marker bypassed identity check: %d", w.Code)
	}
}

func TestTailscaleRunsBeforeAuthAndOrigin(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	h := New(Config{Log: logger, Dist: fstest.MapFS{}, AllowedTSUsers: "owner@example.com", TSLogin: func(context.Context, string) (string, error) { return "stranger@example.com", nil }, Origins: []string{"https://hostbud.example.com"}})
	r := httptest.NewRequestWithContext(t.Context(), "POST", "/api/auth/logout", strings.NewReader(`{}`))
	r.RemoteAddr = "192.0.2.10:1234"
	r.Header.Set("Origin", "http://evil.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "isn't allowed") {
		t.Fatalf("Tailscale check did not run first: %d %s", w.Code, w.Body.String())
	}
}
