package api

import (
	"context"
	"errors"
	"net/http"

	"hostbud/internal/notify"
	"hostbud/internal/store"
)

// NotificationService is the V2-M3 notifier surface (notify.Service). Every
// call is for the signed-in account only.
type NotificationService interface {
	Settings(ctx context.Context, userID string) (notify.Settings, error)
	PutSettings(ctx context.Context, userID string, p store.NotificationPrefs) (notify.Settings, error)
}

func mountNotificationRoutes(s *server, addFunc func(string, http.HandlerFunc)) {
	addFunc("GET /api/notifications/settings", s.getNotificationSettings)
	addFunc("PUT /api/notifications/settings", s.putNotificationSettings)
}

// notificationUser is the signed-in account, or a 401.
func notificationUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	u, _ := r.Context().Value(userKey{}).(store.User)
	if u.ID == "" {
		writeError(w, http.StatusUnauthorized, "sign in first", "")
		return "", false
	}
	return u.ID, true
}

func (s *server) notificationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found", "Reload hostbud.")
	case store.IsUnavailable(err):
		writeDatabaseUnavailable(w)
	default:
		s.cfg.Log.Error("notification request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error", "")
	}
}

// getNotificationSettings: GET /api/notifications/settings → the caller's
// settings, whether push is available (or why not) and the VAPID public key.
func (s *server) getNotificationSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := notificationUser(w, r)
	if !ok {
		return
	}
	settings, err := s.cfg.Notifications.Settings(r.Context(), user)
	if err != nil {
		s.notificationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// putNotificationSettings: PUT /api/notifications/settings with any of
// {enabled, onDone, onAttention, onFinished}; the rest keep their values.
func (s *server) putNotificationSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := notificationUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled     *bool `json:"enabled"`
		OnDone      *bool `json:"onDone"`
		OnAttention *bool `json:"onAttention"`
		OnFinished  *bool `json:"onFinished"`
	}
	if !decode(w, r, &req) {
		return
	}
	current, err := s.cfg.Notifications.Settings(r.Context(), user)
	if err != nil {
		s.notificationError(w, err)
		return
	}
	p := current.NotificationPrefs
	for dst, src := range map[*bool]*bool{&p.Enabled: req.Enabled, &p.OnDone: req.OnDone, &p.OnAttention: req.OnAttention, &p.OnFinished: req.OnFinished} {
		if src != nil {
			*dst = *src
		}
	}
	settings, err := s.cfg.Notifications.PutSettings(r.Context(), user, p)
	if err != nil {
		s.notificationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}
