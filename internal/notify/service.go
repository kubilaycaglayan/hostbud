// Package notify is hostbud's V2-M3 notifier: per-account settings, the one
// payload builder for in-app and push notifications, Web Push subscriptions
// and outbound delivery (docs/roadmap-v2/ARCHITECTURE.md §4, §9).
package notify

import (
	"context"
	"log/slog"

	"hostbud/internal/config"
	"hostbud/internal/store"
)

// Store is what the notifier needs from the store.
type Store interface {
	NotificationPrefs(ctx context.Context, userID string) (store.NotificationPrefs, error)
	PutNotificationPrefs(ctx context.Context, userID string, p store.NotificationPrefs) error
	NotificationsEnabled(ctx context.Context) (bool, error)
}

// PushStatus says whether Web Push is available; Reason says how to turn
// it on when it isn't.
type PushStatus struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Settings is one account's view of its notification settings.
type Settings struct {
	store.NotificationPrefs
	Push PushStatus `json:"push"`
	// VAPIDPublicKey is the applicationServerKey for subscribing (only when
	// push is available).
	VAPIDPublicKey string `json:"vapidPublicKey,omitempty"`
}

// Service serves the notification settings of each account.
type Service struct {
	store Store
	push  config.Push
	vapid string
	log   *slog.Logger
}

// New returns the notifier. push is config.Config.Push(); publicKey is the
// VAPID public key (ignored unless push is available).
func New(st Store, push config.Push, publicKey string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Service{store: st, push: push, log: log}
	if push.Available {
		s.vapid = publicKey
	}
	return s
}

// Enabled reports whether at least one account has notifications on; an
// unreadable store counts as off (nothing is sent on a guess).
func (s *Service) Enabled(ctx context.Context) bool {
	on, err := s.store.NotificationsEnabled(ctx)
	if err != nil {
		s.log.Warn("notification settings unreadable; not notifying", "err", err)
		return false
	}
	return on
}

// PushAvailable reports whether VAPID keys are configured and valid.
func (s *Service) PushAvailable() bool { return s.push.Available }

func (s *Service) settings(p store.NotificationPrefs) Settings {
	return Settings{NotificationPrefs: p, Push: PushStatus{Available: s.push.Available, Reason: s.push.Reason}, VAPIDPublicKey: s.vapid}
}

// Settings returns the account's settings (off without a row).
func (s *Service) Settings(ctx context.Context, userID string) (Settings, error) {
	p, err := s.store.NotificationPrefs(ctx, userID)
	if err != nil {
		return Settings{}, err
	}
	return s.settings(p), nil
}

// PutSettings replaces the account's settings; it never touches another
// account's.
func (s *Service) PutSettings(ctx context.Context, userID string, p store.NotificationPrefs) (Settings, error) {
	if err := s.store.PutNotificationPrefs(ctx, userID, p); err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx, userID)
}
