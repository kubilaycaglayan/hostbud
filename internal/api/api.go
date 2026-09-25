// Package api serves hostbud's HTTP and WebSocket endpoints
// (docs/ARCHITECTURE.md §9).
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/tmux"
)

// Snapshotter is one tracked machine (inventory.Inventory).
type Snapshotter interface {
	Snapshot() (inventory.Machine, []tmux.Session)
}

// Config wires the handler to the rest of hostbud.
type Config struct {
	Log  *slog.Logger
	Dist fs.FS // the built SPA (package web)
	// Origins are the allowed Origin header values for WebSocket upgrades and
	// state-changing requests (see AllowedOrigins).
	Origins  []string
	Bus      *events.Bus
	Machines []Snapshotter // v1: the host only
	Sessions SessionService
}

// New returns the root HTTP handler.
func New(cfg Config) http.Handler {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	s := &server{cfg: cfg, machines: map[string]Snapshotter{}}
	for _, m := range cfg.Machines {
		info, _ := m.Snapshot()
		s.machines[info.ID] = m
		s.order = append(s.order, info.ID)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", handleHealth)
	mux.HandleFunc("GET /api/machines", s.listMachines)
	mux.HandleFunc("GET /api/machines/{machine}/sessions", s.listSessions)
	mux.HandleFunc("POST /api/machines/{machine}/sessions", s.createSession)
	mux.HandleFunc("PATCH /api/machines/{machine}/sessions/{name}", s.renameSession)
	mux.HandleFunc("DELETE /api/machines/{machine}/sessions/{name}", s.killSession)
	mux.HandleFunc("GET /ws/events", s.eventsSocket)
	mux.Handle("GET /", spaHandler(cfg.Dist))
	return checkOrigin(cfg.Origins, mux)
}

type server struct {
	cfg      Config
	machines map[string]Snapshotter
	order    []string
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorBody is the JSON error shape: {error, hint?}.
type errorBody struct {
	Error string `json:"error"`
	Hint  string `json:"hint,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg, hint string) {
	writeJSON(w, status, errorBody{Error: msg, Hint: hint})
}
