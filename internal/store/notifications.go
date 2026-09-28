package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
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

// ---------------------------------------------------------------- push

// PushSubscription is one device's Web Push endpoint and keys, owned by the
// account that subscribed it last.
type PushSubscription struct {
	ID        string    `json:"id"`
	UserID    string    `json:"-"`
	Endpoint  string    `json:"-"`
	P256dh    string    `json:"-"`
	Auth      string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}

const subscriptionCols = `id, user_id, endpoint, p256dh, auth, created_at`

func scanSubscription(row scanner) (PushSubscription, error) {
	var p PushSubscription
	err := row.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256dh, &p.Auth, &p.CreatedAt)
	return p, err
}

// SavePushSubscription stores a device's subscription for the account. An
// endpoint another account subscribed moves to this one (a shared device
// follows who is signed in); its undelivered notifications for the old
// account are dropped with the move.
func (s *Store) SavePushSubscription(ctx context.Context, userID, endpoint, p256dh, auth string) (PushSubscription, error) {
	id, err := newQueueRowID("push")
	if err != nil {
		return PushSubscription{}, err
	}
	var sub PushSubscription
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var previous string
		switch err := tx.QueryRowContext(ctx, `SELECT user_id FROM push_subscriptions WHERE endpoint = $1 FOR UPDATE`, endpoint).Scan(&previous); {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		}
		var err error
		sub, err = scanSubscription(tx.QueryRowContext(ctx, `
			INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, created_at)
			SELECT $1, u.id, $3, $4, $5, $6 FROM users u WHERE u.id = $2
			ON CONFLICT (endpoint) DO UPDATE SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth
			RETURNING `+subscriptionCols, id, userID, endpoint, p256dh, auth, s.now()))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil || previous == "" || previous == userID {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE subscription_id = $1 AND claimed_at IS NULL`, sub.ID)
		return err
	})
	return sub, err
}

// DeletePushSubscription removes the account's subscription for endpoint
// (and its undelivered notifications); false if the account has none there.
func (s *Store) DeletePushSubscription(ctx context.Context, userID, endpoint string) (bool, error) {
	var gone bool
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var id string
		err := tx.QueryRowContext(ctx, `SELECT id FROM push_subscriptions WHERE endpoint = $1 AND user_id = $2 FOR UPDATE`, endpoint, userID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		gone = true
		return deleteSubscription(ctx, tx, id)
	})
	return gone, err
}

// DeletePushSubscriptionByID removes a subscription the push service
// reported gone (404/410), with its pending deliveries.
func (s *Store) DeletePushSubscriptionByID(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error { return deleteSubscription(ctx, tx, id) })
}

func deleteSubscription(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE subscription_id = $1`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, id)
	return err
}

// PushSubscriptions lists the account's subscriptions, oldest first.
func (s *Store) PushSubscriptions(ctx context.Context, userID string) ([]PushSubscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+subscriptionCols+` FROM push_subscriptions WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PushSubscription
	for rows.Next() {
		p, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- outbox

// Notice is a notification to fan out with a transition: one outbox row per
// account that has notifications on and chose Kind, one delivery per
// subscription of those accounts.
type Notice struct {
	Kind    string // done | attention | finished
	Key     string // the dedupe key
	Payload []byte // the allowlisted JSON payload (≤ 1 KiB)
}

var noticeChoice = map[string]string{"done": "on_done", "attention": "on_attention", "finished": "on_finished"}

// enqueueNotice writes n's outbox and delivery rows inside tx. A key an
// account already has is skipped (UNIQUE(user_id, dedupe_key)).
func enqueueNotice(ctx context.Context, tx *sql.Tx, n *Notice, now time.Time) error {
	if n == nil {
		return nil
	}
	col, ok := noticeChoice[n.Kind]
	if !ok {
		return fmt.Errorf("unknown notification kind %q", n.Kind)
	}
	_, err := tx.ExecContext(ctx, `
		WITH added AS (
			INSERT INTO notification_outbox (user_id, dedupe_key, payload_json, created_at)
			SELECT user_id, $1, $2, $3 FROM notification_prefs WHERE enabled AND `+col+`
			ON CONFLICT (user_id, dedupe_key) DO NOTHING
			RETURNING id, user_id)
		INSERT INTO notification_deliveries (outbox_id, subscription_id)
		SELECT a.id, s.id FROM added a JOIN push_subscriptions s ON s.user_id = a.user_id`,
		n.Key, string(n.Payload), now)
	return err
}

// TransitionQueueItemNotify is TransitionQueueItem that also fans out n
// (nil: none) in the same transaction, so a duplicate signal that loses the
// guarded update writes no notification.
func (s *Store) TransitionQueueItemNotify(ctx context.Context, id string, from []string, to string, n *Notice) (QueueItem, error) {
	if n == nil {
		return s.TransitionQueueItem(ctx, id, from, to)
	}
	if !slices.Contains(itemStatuses, to) {
		return QueueItem{}, fmt.Errorf("invalid item status %q", to)
	}
	var it QueueItem
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		if it, err = scanItem(tx.QueryRowContext(ctx, `
			UPDATE queue_items SET status = $2, updated_at = $3 WHERE id = $1 AND status = ANY($4) RETURNING `+itemCols,
			id, to, s.now(), from)); err != nil {
			return err
		}
		return enqueueNotice(ctx, tx, n, s.now())
	})
	if errors.Is(err, sql.ErrNoRows) {
		if _, e := s.QueueItem(ctx, id); e != nil {
			return QueueItem{}, e
		}
		return QueueItem{}, ErrConflict
	}
	return it, err
}

// TransitionQueueNotify is TransitionQueue that also fans out n (nil: none)
// in the same transaction.
func (s *Store) TransitionQueueNotify(ctx context.Context, id string, from []string, to string, n *Notice) (Queue, error) {
	if n == nil {
		return s.TransitionQueue(ctx, id, from, to)
	}
	if !slices.Contains(queueStatuses, to) {
		return Queue{}, fmt.Errorf("invalid queue status %q", to)
	}
	var q Queue
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		if q, err = scanQueue(tx.QueryRowContext(ctx, `
			UPDATE queues SET status = $2, updated_at = $3 WHERE id = $1 AND status = ANY($4) RETURNING `+queueCols,
			id, to, s.now(), from)); err != nil {
			return err
		}
		return enqueueNotice(ctx, tx, n, s.now())
	})
	if errors.Is(err, sql.ErrNoRows) {
		if _, e := s.Queue(ctx, id); e != nil {
			return Queue{}, e
		}
		return Queue{}, ErrConflict
	}
	return q, err
}

// EnqueueTestNotification queues n for one of the account's devices only
// (Settings → Send test notification). ErrNotFound: no such subscription.
func (s *Store) EnqueueTestNotification(ctx context.Context, userID, endpoint string, n Notice) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			WITH sub AS (SELECT id FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2),
			added AS (
				INSERT INTO notification_outbox (user_id, dedupe_key, payload_json, created_at)
				SELECT $1, $3, $4, $5 FROM sub
				ON CONFLICT (user_id, dedupe_key) DO NOTHING
				RETURNING id)
			INSERT INTO notification_deliveries (outbox_id, subscription_id)
			SELECT a.id, s.id FROM added a, sub s`, userID, endpoint, n.Key, string(n.Payload), s.now())
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ---------------------------------------------------------------- deliveries

// Delivery statuses.
const (
	DeliveryPending = "pending"
	DeliverySent    = "sent"
	DeliveryFailed  = "failed"
)

// Delivery is one claimed notification for one device.
type Delivery struct {
	OutboxID       int64
	SubscriptionID string
	Endpoint       string
	P256dh         string
	Auth           string
	Key            string
	Payload        []byte
}

// ClaimDelivery takes the oldest unclaimed delivery and marks it claimed in
// a committed transaction, before anything is sent: a claimed delivery is
// never sent again, even after a restart (at most once). ErrNotFound: none.
func (s *Store) ClaimDelivery(ctx context.Context) (Delivery, error) {
	var d Delivery
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var payload string
		err := tx.QueryRowContext(ctx, `
			SELECT d.outbox_id, d.subscription_id, s.endpoint, s.p256dh, s.auth, o.dedupe_key, o.payload_json
			FROM notification_deliveries d
			JOIN notification_outbox o ON o.id = d.outbox_id
			JOIN push_subscriptions s ON s.id = d.subscription_id
			WHERE d.claimed_at IS NULL
			ORDER BY d.outbox_id, d.subscription_id
			LIMIT 1 FOR UPDATE OF d SKIP LOCKED`).Scan(&d.OutboxID, &d.SubscriptionID, &d.Endpoint, &d.P256dh, &d.Auth, &d.Key, &payload)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		d.Payload = []byte(payload)
		_, err = tx.ExecContext(ctx, `UPDATE notification_deliveries SET claimed_at = $3 WHERE outbox_id = $1 AND subscription_id = $2`, d.OutboxID, d.SubscriptionID, s.now())
		return err
	})
	return d, err
}

// FinishDelivery records a claimed delivery's result (sent or failed).
func (s *Store) FinishDelivery(ctx context.Context, outboxID int64, subscriptionID, status string) error {
	if status != DeliverySent && status != DeliveryFailed {
		return fmt.Errorf("invalid delivery status %q", status)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = $3, finished_at = $4 WHERE outbox_id = $1 AND subscription_id = $2`,
		outboxID, subscriptionID, status, s.now())
	return err
}

// PruneNotifications deletes outbox rows created before cutoff and their
// deliveries (operational rows, not user data). It returns the outbox rows
// removed.
func (s *Store) PruneNotifications(ctx context.Context, cutoff time.Time) (int64, error) {
	var n int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE outbox_id IN (SELECT id FROM notification_outbox WHERE created_at < $1)`, cutoff); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM notification_outbox WHERE created_at < $1`, cutoff)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}
