// Package notify is hostbud's V2-M3 notifier: per-account settings, the one
// payload builder for in-app and push notifications, Web Push subscriptions
// and outbound delivery (docs/roadmap-v2/ARCHITECTURE.md §4, §9).
package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"hostbud/internal/config"
	"hostbud/internal/store"
)

// Store is what the notifier needs from the store.
type Store interface {
	NotificationPrefs(ctx context.Context, userID string) (store.NotificationPrefs, error)
	PutNotificationPrefs(ctx context.Context, userID string, p store.NotificationPrefs) error
	NotificationsEnabled(ctx context.Context) (bool, error)
	SavePushSubscription(ctx context.Context, userID, endpoint, p256dh, auth string) (store.PushSubscription, error)
	DeletePushSubscription(ctx context.Context, userID, endpoint string) (bool, error)
	EnqueueTestNotification(ctx context.Context, userID, endpoint string, n store.Notice) error
}

// Error is a refusal the API shows as is.
type Error struct {
	Status     int
	Message    string
	Hint       string
	RetryAfter time.Duration // for 429
}

func (e *Error) Error() string { return e.Message }

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

// Service serves the notification settings and push subscriptions of each
// account, and wakes the sender when transitions queued notifications.
type Service struct {
	store      Store
	push       config.Push
	vapid      string
	testPrefix string
	sender     interface{ Wake() }
	log        *slog.Logger
	now        func() time.Time
	testMu     sync.Mutex
	testTokens map[string]testBucket // per account
}

// Test notifications per account: a burst of three, then one every 10 s
// (a button, not a flood).
const (
	testBurst    = 3
	testInterval = 10 * time.Second
)

type testBucket struct {
	tokens float64
	at     time.Time
}

// takeTest spends one of the account's test tokens, or says how long to wait.
func (s *Service) takeTest(userID string) time.Duration {
	s.testMu.Lock()
	defer s.testMu.Unlock()
	now := s.now()
	b, ok := s.testTokens[userID]
	if !ok {
		b = testBucket{tokens: testBurst, at: now}
	}
	b.tokens = min(testBurst, b.tokens+now.Sub(b.at).Seconds()/testInterval.Seconds())
	b.at = now
	if b.tokens < 1 {
		s.testTokens[userID] = b
		return time.Duration((1 - b.tokens) * float64(testInterval))
	}
	b.tokens--
	s.testTokens[userID] = b
	return 0
}

// SendTest queues a test notification for one of the account's devices
// (its push endpoint) only, and wakes the sender.
func (s *Service) SendTest(ctx context.Context, userID, endpoint string) error {
	if !s.push.Available {
		return &Error{Status: http.StatusConflict, Message: s.push.Reason, Hint: "The test is shown in the app instead while hostbud is open."}
	}
	if wait := s.takeTest(userID); wait > 0 {
		secs := int(math.Ceil(wait.Seconds()))
		return &Error{Status: http.StatusTooManyRequests, Message: "Too many test notifications", Hint: fmt.Sprintf("Try again in %d s.", secs), RetryAfter: time.Duration(secs) * time.Second}
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	p, err := Build(Event{Kind: KindTest, RunID: hex.EncodeToString(id)})
	if err != nil {
		return err
	}
	b, err := p.JSON()
	if err != nil {
		return err
	}
	err = s.store.EnqueueTestNotification(ctx, userID, endpoint, store.Notice{Kind: KindTest, Key: p.Key, Payload: b})
	if errors.Is(err, store.ErrNotFound) {
		return &Error{Status: http.StatusNotFound, Message: "This device has no push subscription for your account", Hint: "Turn notifications off and on again on this device."}
	}
	if err != nil {
		return err
	}
	s.Wake()
	return nil
}

// SetPush wires Web Push delivery: the sender to wake and the e2e-only
// endpoint prefix exempt from the endpoint rules.
func (s *Service) SetPush(sender interface{ Wake() }, testPrefix string) {
	s.sender, s.testPrefix = sender, testPrefix
}

// Wake tells the sender that transitions queued notifications.
func (s *Service) Wake() {
	if s.sender != nil {
		s.sender.Wake()
	}
}

// Subscribe stores this device's push subscription for the account (it
// moves here from another account).
func (s *Service) Subscribe(ctx context.Context, userID, endpoint, p256dh, auth string) error {
	if !s.push.Available {
		return &Error{Status: http.StatusConflict, Message: s.push.Reason, Hint: "In-app notifications still work while hostbud is open."}
	}
	if err := CheckEndpoint(endpoint, s.testPrefix); err != nil {
		return &Error{Status: http.StatusBadRequest, Message: err.Error(), Hint: "Subscribe from the browser's own push service."}
	}
	if err := CheckKeys(p256dh, auth); err != nil {
		return &Error{Status: http.StatusBadRequest, Message: err.Error(), Hint: "Send the subscription's keys as the browser reports them."}
	}
	_, err := s.store.SavePushSubscription(ctx, userID, endpoint, p256dh, auth)
	return err
}

// Unsubscribe removes this device's subscription from the account (a
// no-op if it has none there).
func (s *Service) Unsubscribe(ctx context.Context, userID, endpoint string) error {
	_, err := s.store.DeletePushSubscription(ctx, userID, endpoint)
	return err
}

// New returns the notifier. push is config.Config.Push(); publicKey is the
// VAPID public key (ignored unless push is available).
func New(st Store, push config.Push, publicKey string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Service{store: st, push: push, log: log, now: time.Now, testTokens: map[string]testBucket{}}
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
