package store

import (
	"context"
	"database/sql"
	"errors"
)

// V2-M3 notifications (migrations/0008_notifications.sql): the per-account
// switch and event choices.

// NotificationPrefs is one account's notification settings. No row reads
// as the defaults: off, with every event chosen.
type NotificationPrefs struct {
	Enabled     bool `json:"enabled"`
	OnDone      bool `json:"onDone"`
	OnAttention bool `json:"onAttention"`
	OnFinished  bool `json:"onFinished"`
}

// DefaultNotificationPrefs is what an account without a row gets.
var DefaultNotificationPrefs = NotificationPrefs{OnDone: true, OnAttention: true, OnFinished: true}

// NotificationPrefs returns the account's settings (the defaults without a row).
func (s *Store) NotificationPrefs(ctx context.Context, userID string) (NotificationPrefs, error) {
	p := DefaultNotificationPrefs
	err := s.db.QueryRowContext(ctx, `SELECT enabled, on_done, on_attention, on_finished FROM notification_prefs WHERE user_id = $1`, userID).
		Scan(&p.Enabled, &p.OnDone, &p.OnAttention, &p.OnFinished)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultNotificationPrefs, nil
	}
	return p, err
}

// PutNotificationPrefs stores the account's settings (ErrNotFound for an
// unknown account).
func (s *Store) PutNotificationPrefs(ctx context.Context, userID string, p NotificationPrefs) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_prefs (user_id, enabled, on_done, on_attention, on_finished, updated_at)
		SELECT id, $2, $3, $4, $5, $6 FROM users WHERE id = $1
		ON CONFLICT (user_id) DO UPDATE SET enabled = EXCLUDED.enabled, on_done = EXCLUDED.on_done,
			on_attention = EXCLUDED.on_attention, on_finished = EXCLUDED.on_finished, updated_at = EXCLUDED.updated_at`,
		userID, p.Enabled, p.OnDone, p.OnAttention, p.OnFinished, s.now())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// NotificationsEnabled reports whether at least one account has turned
// notifications on (V2-M3: with none on, nothing new runs).
func (s *Store) NotificationsEnabled(ctx context.Context) (bool, error) {
	var on bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM notification_prefs WHERE enabled)`).Scan(&on)
	return on, err
}
