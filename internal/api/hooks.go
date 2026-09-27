package api

import (
	"errors"
	"net/http"
	"strings"

	"hostbud/internal/queue"
	"hostbud/internal/store"
)

const hookRoute = "POST /api/hooks/{run}/{event}"

// tokenAuthRoutes are the only routes authenticated by a per-run bearer
// token instead of the cookie session. Agent hooks on the host call them,
// not a browser, so they are also exempt from the Origin allowlist, the
// Tailscale identity gate and the generic JSON body limits; the handler
// caps the body itself (docs/roadmap-v2/ARCHITECTURE.md §8, §9).
var tokenAuthRoutes = map[string]bool{hookRoute: true}

// tokenAuthRoute reports whether r targets a token-authenticated route.
func tokenAuthRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/hooks/")
}

func (s *server) runHook(w http.ResponseWriter, r *http.Request) {
	err := s.cfg.Hooks.Receive(r.Context(), r.PathValue("run"), r.PathValue("event"), r.Header.Get("Authorization"), r.Body)
	var refused *queue.HookError
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.As(err, &refused):
		writeError(w, refused.Status, refused.Message, "")
	case store.IsUnavailable(err):
		writeDatabaseUnavailable(w)
	default:
		s.cfg.Log.Error("run hook failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error", "")
	}
}
