package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// AllowedOrigins returns the Origin allowlist: http://localhost:<localPort>
// (SSH port forward) and, if set, https://<domain> (tailnet).
func AllowedOrigins(domain string, localPort int) []string {
	origins := []string{fmt.Sprintf("http://localhost:%d", localPort)}
	if domain != "" {
		origins = append(origins, "https://"+domain)
	}
	return origins
}

// checkOrigin rejects WebSocket upgrades and state-changing requests whose
// Origin header is missing or not in the allowlist (CSRF / cross-site
// WebSocket hijacking). Safe methods pass through.
func checkOrigin(allowed []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tokenAuthRoute(r) {
			next.ServeHTTP(w, r)
			return
		}
		safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
		upgrade := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
		if (!safe || upgrade) && !slices.Contains(allowed, r.Header.Get("Origin")) {
			writeError(w, http.StatusForbidden, "request origin not allowed",
				"Open hostbud at "+strings.Join(allowed, " or ")+"; requests from other sites are refused.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
