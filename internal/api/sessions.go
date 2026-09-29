package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"hostbud/internal/inventory"
	"hostbud/internal/projects"
	"hostbud/internal/session"
	"hostbud/internal/tmux"
)

// SessionService is the session service (session.Service).
type SessionService interface {
	Output(ctx context.Context, machine, name string) (string, error)
	Create(ctx context.Context, spec session.Spec) (string, error)
	Rename(ctx context.Context, machine, from, to string) error
	Kill(ctx context.Context, machine, name string) error
	KillMany(ctx context.Context, machine string, names []string) ([]string, []session.KillFailure, error)
	CopyMode(ctx context.Context, machine, name string, action tmux.CopyAction, lines int) (session.CopyModeState, error)
	ListWindows(ctx context.Context, machine, name string) (session.WindowsState, error)
	SelectWindow(ctx context.Context, machine, name, windowID, paneID string) (session.WindowsState, error)
}

func (s *server) sessionOutput(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.machine(w, r); !ok {
		return
	}
	out, err := s.cfg.Sessions.Output(r.Context(), r.PathValue("machine"), r.PathValue("name"))
	if err != nil {
		s.writeSessionError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}

func (s *server) listMachines(w http.ResponseWriter, _ *http.Request) {
	out := make([]inventory.Machine, 0, len(s.order))
	for _, id := range s.order {
		m, _ := s.machines[id].Snapshot()
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": out})
}

func (s *server) machine(w http.ResponseWriter, r *http.Request) (Snapshotter, bool) {
	m, ok := s.machines[r.PathValue("machine")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown machine", "")
	}
	return m, ok
}

func (s *server) listSessions(w http.ResponseWriter, r *http.Request) {
	m, ok := s.machine(w, r)
	if !ok {
		return
	}
	_, sessions := m.Snapshot()
	writeJSON(w, http.StatusOK, map[string][]tmux.Session{"sessions": s.withProjectPlacement(r.Context(), r.PathValue("machine"), sessions)})
}

type sessionPlacementResolver interface {
	Place(context.Context, string, string, string) (projects.Placement, error)
}

func (s *server) withProjectPlacement(ctx context.Context, machine string, sessions []tmux.Session) []tmux.Session {
	resolver, ok := s.cfg.Projects.(sessionPlacementResolver)
	if !ok {
		return sessions
	}
	placed := append([]tmux.Session{}, sessions...)
	for i := range placed {
		placement, err := resolver.Place(ctx, machine, placed[i].Name, placed[i].Path)
		if err != nil {
			s.cfg.Log.Warn("resolve session project placement", "machine", machine, "session", placed[i].Name, "err", err)
			continue
		}
		if placement.Matched {
			placed[i].ProjectID = placement.ProjectID
		}
	}
	return placed
}

type createRequest struct {
	Name         string            `json:"name"`
	Path         string            `json:"path"`
	Env          map[string]string `json:"env"`
	StartCommand string            `json:"startCommand"`
}

type renameRequest struct {
	Name string `json:"name"`
}

// decode reads a small JSON body, rejecting unknown fields.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is limited to 64 KiB", "Send a smaller JSON request.")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body", "Send a JSON object with the documented fields.")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON body", "Send one JSON value.")
		return false
	}
	return true
}

func (s *server) createSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.machine(w, r); !ok {
		return
	}
	var req createRequest
	if !decode(w, r, &req) {
		return
	}
	// Reject a bad explicit name before anything reaches the host.
	req.Name = tmux.NormalizeName(req.Name)
	if req.Name != "" {
		if err := tmux.ValidateName(req.Name); err != nil {
			writeError(w, http.StatusBadRequest, "invalid session name", err.Error())
			return
		}
	}
	for key := range req.Env {
		if err := tmux.ValidateEnvKey(key); err != nil {
			writeError(w, http.StatusBadRequest, "invalid environment variable name", err.Error())
			return
		}
	}
	name, err := s.cfg.Sessions.Create(r.Context(), session.Spec{
		Machine: r.PathValue("machine"), Name: req.Name, Path: req.Path, Env: req.Env, StartCommand: req.StartCommand,
	})
	if err != nil {
		s.writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (s *server) renameSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.machine(w, r); !ok {
		return
	}
	var req renameRequest
	if !decode(w, r, &req) {
		return
	}
	req.Name = tmux.NormalizeName(req.Name)
	for _, n := range []string{r.PathValue("name"), req.Name} {
		if err := tmux.ValidateName(n); err != nil {
			writeError(w, http.StatusBadRequest, "invalid session name", err.Error())
			return
		}
	}
	if err := s.cfg.Sessions.Rename(r.Context(), r.PathValue("machine"), r.PathValue("name"), req.Name); err != nil {
		s.writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name})
}

// killSession kills a session; the UI asks the user to confirm first.
func (s *server) killSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.machine(w, r); !ok {
		return
	}
	if err := tmux.ValidateName(r.PathValue("name")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session name", err.Error())
		return
	}
	if err := s.cfg.Sessions.Kill(r.Context(), r.PathValue("machine"), r.PathValue("name")); err != nil {
		s.writeSessionError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type killManyRequest struct {
	Names []string `json:"names"`
}

// killSessions kills several sessions in one request (a project's "kill
// all"), refreshing the inventory once; the UI asks the user to confirm first.
// It answers 200 with the killed names and per-session failures.
func (s *server) killSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.machine(w, r); !ok {
		return
	}
	var req killManyRequest
	if !decode(w, r, &req) {
		return
	}
	killed, failed, err := s.cfg.Sessions.KillMany(r.Context(), r.PathValue("machine"), req.Names)
	if err != nil {
		s.writeSessionError(w, err)
		return
	}
	if failed == nil {
		failed = []session.KillFailure{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"killed": killed, "failed": failed})
}

type copyModeRequest struct {
	Action tmux.CopyAction `json:"action"`
	Lines  *int            `json:"lines,omitempty"`
}

func (s *server) copyMode(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.machine(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if err := tmux.ValidateName(name); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session name", err.Error())
		return
	}
	var req copyModeRequest
	if !decode(w, r, &req) {
		return
	}
	lines := 0
	if req.Lines != nil {
		lines = *req.Lines
	}
	scroll := req.Action == tmux.CopyScrollUp || req.Action == tmux.CopyScrollDown || req.Action == tmux.CopyWheelUp || req.Action == tmux.CopyWheelDown
	if (!scroll && req.Lines != nil) || (req.Lines != nil && (lines < 1 || lines > 500)) {
		writeError(w, http.StatusBadRequest, "invalid copy-mode request", "Lines is only accepted for scroll-up, scroll-down, wheel-up or wheel-down and must be between 1 and 500.")
		return
	}
	if _, err := tmux.CopyModeArgs(name, req.Action, lines); err != nil {
		writeError(w, http.StatusBadRequest, "invalid copy-mode request", err.Error())
		return
	}
	state, err := s.cfg.Sessions.CopyMode(r.Context(), r.PathValue("machine"), name, req.Action, lines)
	if err != nil {
		s.writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *server) listWindows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "Use GET to list session windows and panes.")
		return
	}
	if _, ok := s.machine(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if err := tmux.ValidateName(name); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session name", err.Error())
		return
	}
	state, err := s.cfg.Sessions.ListWindows(r.Context(), r.PathValue("machine"), name)
	if err != nil {
		s.writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

type selectRequest struct {
	Window string `json:"window"`
	Pane   string `json:"pane,omitempty"`
}

func (s *server) selectWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "Use POST to select a session window or pane.")
		return
	}
	if _, ok := s.machine(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if err := tmux.ValidateName(name); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session name", err.Error())
		return
	}
	var req selectRequest
	if !decode(w, r, &req) {
		return
	}
	if _, err := tmux.SelectArgs(name, req.Window, req.Pane); err != nil {
		writeError(w, http.StatusBadRequest, "invalid window or pane", "Use a window id such as @1 and an optional pane id such as %1.")
		return
	}
	state, err := s.cfg.Sessions.SelectWindow(r.Context(), r.PathValue("machine"), name, req.Window, req.Pane)
	if err != nil {
		s.writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

var codeStatus = map[session.Code]int{
	session.CodeInvalid:        http.StatusBadRequest,
	session.CodePathNotFound:   http.StatusBadRequest,
	session.CodeNotFound:       http.StatusNotFound,
	session.CodeUnknownMachine: http.StatusNotFound,
	session.CodeDuplicate:      http.StatusConflict,
	session.CodeTmuxMissing:    http.StatusServiceUnavailable,
	session.CodeUnavailable:    http.StatusServiceUnavailable,
	session.CodeTmuxVersion:    http.StatusConflict,
	session.CodeTimeout:        http.StatusGatewayTimeout,
}

func (s *server) writeSessionError(w http.ResponseWriter, err error) {
	var e *session.Error
	if !errors.As(err, &e) {
		s.cfg.Log.Error("session request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error", "")
		return
	}
	status, ok := codeStatus[e.Code]
	if !ok {
		status = http.StatusInternalServerError
	}
	writeError(w, status, e.Message, e.Hint)
}
