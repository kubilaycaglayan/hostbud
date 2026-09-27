package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const fixtureBootScript = `try{const m=localStorage.getItem('hostbud.theme');document.documentElement.dataset.theme=m||'dark'}catch{document.documentElement.dataset.theme='dark'}`

func TestBuildContentSecurityPolicy(t *testing.T) {
	dist := fstest.MapFS{"index.html": {Data: []byte(`<script>` + fixtureBootScript + `</script><script type="module" src="/app.js"></script>`)}}
	for _, tc := range []struct {
		name, domain, local string
	}{
		{"local only", "", "http://localhost:9055"},
		{"domain only", "https://hostbud.example.com", ""},
		{"both", "https://hostbud.example.com", "http://localhost:9055"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origins := make([]string, 0, 2)
			if tc.domain != "" {
				origins = append(origins, tc.domain)
			}
			if tc.local != "" {
				origins = append(origins, tc.local)
			}
			policy, err := BuildContentSecurityPolicy(dist, origins)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(policy, "script-src 'self' 'sha256-") || !strings.Contains(policy, "connect-src 'self'") || strings.Contains(policy, "unsafe-eval") {
				t.Fatalf("invalid CSP: %s", policy)
			}
			if (tc.local != "") != strings.Contains(policy, "ws://localhost:9055") || (tc.domain != "") != strings.Contains(policy, "wss://hostbud.example.com") {
				t.Fatalf("CSP connect origins don't match allowlist: %s", policy)
			}
			hash := sha256.Sum256([]byte(fixtureBootScript))
			if !strings.Contains(policy, "'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'") {
				t.Fatalf("CSP doesn't hash the exact boot script: %s", policy)
			}
		})
	}
	if _, err := BuildContentSecurityPolicy(fstest.MapFS{"index.html": {Data: []byte(`<script>` + fixtureBootScript + `</script><script>extra()</script>`)}}, nil); err == nil {
		t.Fatal("second inline script was accepted")
	}
	if _, err := BuildContentSecurityPolicy(fstest.MapFS{"index.html": {Data: []byte(`<script>other()</script>`)}}, nil); err == nil {
		t.Fatal("unapproved inline script was accepted")
	}
	if _, err := BuildContentSecurityPolicy(fstest.MapFS{"index.html": {Data: []byte(`<script>` + fixtureBootScript + `;fetch('/unexpected')</script>`)}}, nil); err == nil {
		t.Fatal("theme script with a network request was accepted")
	}
	if _, err := BuildContentSecurityPolicy(fstest.MapFS{}, nil); err == nil {
		t.Fatal("missing index.html was accepted")
	}
}

func TestSecurityHeadersCoverResponsesAndNeverSetHSTS(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":    {Data: []byte(`<script>` + fixtureBootScript + `</script><script type="module" src="/app.js"></script>`)},
		"assets/app.js": {Data: []byte(`console.log('ok')`)},
	}
	policy, err := BuildContentSecurityPolicy(dist, AllowedOrigins("hostbud.example.com", 9055))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Config{Dist: dist, Auth: &fakeAuth{}, ContentSecurityPolicy: policy, DBPing: func(_ context.Context) error { return errors.New("offline") }})
	for _, path := range []string{"/api/health", "/", "/assets/app.js", "/missing.js"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			for name, want := range map[string]string{
				"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "X-Frame-Options": "DENY",
				"Cross-Origin-Opener-Policy": "same-origin", "Cross-Origin-Resource-Policy": "same-origin",
				"Permissions-Policy": "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
			} {
				if got := rec.Header().Get(name); got != want {
					t.Errorf("%s=%q want %q", name, got, want)
				}
			}
			if rec.Header().Get("Strict-Transport-Security") != "" {
				t.Error("hostbud sent HSTS")
			}
			if rec.Header().Get("Content-Security-Policy") != policy {
				t.Error("missing CSP")
			}
		})
	}
}
