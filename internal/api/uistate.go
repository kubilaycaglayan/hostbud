package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"hostbud/internal/store"
)

// maxUIState bounds one stored UI state value (the layout is a few KiB).
const maxUIState = 64 << 10

// uiStateKeys are the UI state keys a client may store.
// The server doesn't interpret the values; clients validate what
// they read back.
var uiStateKeys = map[string]bool{"layout": true, "tree": true, "theme": true}

// UIStateStore keeps UI state per account (store.Store).
type UIStateStore interface {
	UIStateForUser(ctx context.Context, userID, key string) (json.RawMessage, error)
	PutUIStateForUser(ctx context.Context, userID, key string, value json.RawMessage) error
}

func (s *server) uiStateKey(w http.ResponseWriter, r *http.Request) (userID, key string, ok bool) {
	key = r.PathValue("key")
	if !uiStateKeys[key] {
		writeError(w, http.StatusNotFound, "unknown UI state key", "")
		return "", "", false
	}
	u, _ := r.Context().Value(userKey{}).(store.User)
	if u.ID == "" {
		writeError(w, http.StatusUnauthorized, "sign in first", "")
		return "", "", false
	}
	return u.ID, key, true
}

// getUIState: GET /api/ui-state/{key} → the account's stored JSON, or 404.
func (s *server) getUIState(w http.ResponseWriter, r *http.Request) {
	user, key, ok := s.uiStateKey(w, r)
	if !ok {
		return
	}
	v, err := s.cfg.UIState.UIStateForUser(r.Context(), user, key)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "nothing saved yet", "")
	case err != nil:
		s.cfg.Log.Warn("can't read UI state", "key", key, "err", err)
		writeError(w, http.StatusInternalServerError, "can't read the saved UI state", "")
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(v) //nolint:gosec // JSON (validated on PUT) served as application/json, never HTML

	}
}

// putUIState: PUT /api/ui-state/{key} with a JSON body (≤ 64 KiB) → 204.
func (s *server) putUIState(w http.ResponseWriter, r *http.Request) {
	user, key, ok := s.uiStateKey(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxUIState))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, "UI state is limited to 64 KiB", "")
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "can't read the request body", "")
		return
	case !json.Valid(body):
		writeError(w, http.StatusBadRequest, "the body must be JSON", "")
		return
	}
	if err := s.cfg.UIState.PutUIStateForUser(r.Context(), user, key, body); err != nil {
		s.cfg.Log.Warn("can't save UI state", "key", key, "err", err)
		writeError(w, http.StatusInternalServerError, "can't save the UI state", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
