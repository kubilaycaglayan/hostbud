// Package api serves hostbud's HTTP and WebSocket endpoints.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
)

// New returns the root HTTP handler. dist is the built SPA (see package web).
func New(log *slog.Logger, dist fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", handleHealth)
	mux.Handle("GET /", spaHandler(dist))
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
