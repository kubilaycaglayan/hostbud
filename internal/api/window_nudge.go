package api

import (
	"errors"
	"net/http"

	"hostbud/internal/store"
)

func (s *server) getWindowNudge(w http.ResponseWriter, r *http.Request) {
	settings, err := s.cfg.WindowNudge.Settings(r.Context())
	if err != nil {
		s.windowNudgeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) putWindowNudge(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled   bool   `json:"enabled"`
		MachineID string `json:"machineId"`
		Directory string `json:"directory"`
		Provider  string `json:"provider"`
		Schedule  string `json:"schedule"`
		SkipFirst bool   `json:"skipFirst"`
	}
	if !decode(w, r, &input) {
		return
	}
	settings, err := s.cfg.WindowNudge.PutSettings(r.Context(), store.WindowNudge{
		Enabled: input.Enabled, MachineID: input.MachineID, Directory: input.Directory,
		Provider: input.Provider, Schedule: input.Schedule,
	}, input.SkipFirst)
	if err != nil {
		s.windowNudgeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) windowNudgeError(w http.ResponseWriter, err error) {
	var ne interface {
		error
		HTTPStatus() int
		UserHint() string
	}
	if errors.As(err, &ne) {
		writeError(w, ne.HTTPStatus(), err.Error(), ne.UserHint())
		return
	}
	if store.IsUnavailable(err) {
		writeDatabaseUnavailable(w)
		return
	}
	s.cfg.Log.Error("window nudge request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error", "")
}
