package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"hostbud/internal/store"
)

// Push delivery limits (V2-M3 T3).
const (
	pushTimeout   = 10 * time.Second
	pushTTL       = 24 * 60 * 60 // seconds
	maxRetryAfter = 60 * time.Second
	pruneAfter    = 7 * 24 * time.Hour
	pruneEvery    = time.Hour
	senderWorkers = 4
)

// RetryDelays are the waits before each retry of a network error, 429 or
// 5xx: at most three retries.
var RetryDelays = []time.Duration{time.Second, 5 * time.Second, 25 * time.Second}

// ErrEndpoint means a push endpoint the rules refuse.
var ErrEndpoint = errors.New("push endpoint must be an https URL on port 443 with a DNS host name")

// ErrKeys means subscription keys that aren't a P-256 point and a 16-byte secret.
var ErrKeys = errors.New("subscription keys must be a base64url P-256 public key (p256dh) and a 16-byte auth secret")

// CheckEndpoint applies the endpoint rules: https, port 443, a DNS host (no
// IP literal, no single-label name), no credentials. An endpoint under
// testPrefix (HOSTBUD_PUSH_TEST_ENDPOINT, e2e only) is exempt.
func CheckEndpoint(raw, testPrefix string) error {
	if testPrefix != "" && strings.HasPrefix(raw, testPrefix) {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.User != nil || u.Opaque != "" {
		return ErrEndpoint
	}
	host := u.Hostname()
	if port := u.Port(); port != "" && port != "443" {
		return ErrEndpoint
	}
	if host == "" || !strings.Contains(host, ".") || strings.HasSuffix(host, ".") {
		return ErrEndpoint
	}
	if _, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return ErrEndpoint
	}
	return nil
}

// CheckKeys validates a subscription's p256dh and auth values.
func CheckKeys(p256dh, auth string) error {
	pub, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(p256dh, "="))
	if err != nil || len(pub) != 65 {
		return ErrKeys
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return ErrKeys
	}
	secret, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(auth, "="))
	if err != nil || len(secret) != 16 {
		return ErrKeys
	}
	return nil
}

// blockedPrefixes are addresses a push endpoint must never resolve to:
// hostbud's own network, the host, the tailnet.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT / Tailscale
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

func publicAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || !ip.IsGlobalUnicast() {
		return errors.New("push endpoint resolves to a non-public address")
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return errors.New("push endpoint resolves to a non-public address")
		}
	}
	return nil
}

// newPushClient returns the HTTP client for push services: bounded, no
// redirects, and (unless for the e2e test endpoint) public addresses only.
func newPushClient(publicOnly bool) *http.Client {
	dialer := &net.Dialer{Timeout: pushTimeout}
	if publicOnly {
		dialer.Control = func(_, address string, _ syscall.RawConn) error { return publicAddress(address) }
	}
	return &http.Client{
		Timeout: pushTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   pushTimeout,
			ResponseHeaderTimeout: pushTimeout,
			MaxIdleConns:          8,
			IdleConnTimeout:       90 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// SenderStore is what the sender needs from the store.
type SenderStore interface {
	ClaimDelivery(ctx context.Context) (store.Delivery, error)
	FinishDelivery(ctx context.Context, outboxID int64, subscriptionID, status string) error
	DeletePushSubscriptionByID(ctx context.Context, id string) error
	PruneNotifications(ctx context.Context, cutoff time.Time) (int64, error)
}

// VAPID is the server's push identity.
type VAPID struct {
	PublicKey, PrivateKey, Subject string
}

// Sender drains the outbox: each delivery is claimed (committed) before its
// POST and never sent again, so a crash mid-POST loses at most that one.
type Sender struct {
	store      SenderStore
	vapid      VAPID
	testPrefix string
	client     *http.Client // public push services
	testClient *http.Client // the e2e push fake (testPrefix)
	log        *slog.Logger
	wake       chan struct{}
	// sleep waits between retries (tests replace it).
	sleep func(ctx context.Context, d time.Duration) error
	now   func() time.Time
}

// NewSender returns a sender for the configured VAPID identity.
func NewSender(st SenderStore, vapid VAPID, testPrefix string, log *slog.Logger) *Sender {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Sender{
		store: st, vapid: vapid, testPrefix: testPrefix, log: log,
		client: newPushClient(true), testClient: newPushClient(false),
		wake: make(chan struct{}, 1), sleep: sleepCtx, now: time.Now,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Wake asks the sender to look for new deliveries (after a transition
// committed some). It never blocks.
func (s *Sender) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run sends until ctx ends. On start it sends every unclaimed delivery (a
// restart's backlog; claimed ones are never sent again), then works only
// when woken: with every account off it does nothing. Old rows are pruned
// at most hourly, when there is work.
func (s *Sender) Run(ctx context.Context) {
	s.prune(ctx)
	lastPrune := s.now()
	for {
		s.Drain(ctx)
		if s.now().Sub(lastPrune) >= pruneEvery {
			s.prune(ctx)
			lastPrune = s.now()
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
	}
}

// Drain sends every unclaimed delivery, a few devices at a time, and
// returns when they are all finished.
func (s *Sender) Drain(ctx context.Context) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, senderWorkers)
	for ctx.Err() == nil {
		d, err := s.store.ClaimDelivery(ctx)
		if errors.Is(err, store.ErrNotFound) {
			break
		}
		if err != nil {
			s.log.Warn("push: can't claim a delivery", "err", err)
			break
		}
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-slots; wg.Done() }()
			s.deliver(ctx, d)
		}()
	}
	wg.Wait()
}

func (s *Sender) prune(ctx context.Context) {
	if n, err := s.store.PruneNotifications(ctx, s.now().Add(-pruneAfter)); err != nil {
		s.log.Warn("push: pruning old notifications failed", "err", err)
	} else if n > 0 {
		s.log.Debug("push: pruned old notifications", "rows", n)
	}
}

// deliver sends one claimed delivery: retries network errors, 429 and 5xx
// (RetryDelays, Retry-After up to 60 s); 404/410 removes the subscription;
// other 4xx drop it. Logs name the subscription id, never the endpoint.
func (s *Sender) deliver(ctx context.Context, d store.Delivery) {
	for attempt := 0; ; attempt++ {
		status, retryAfter, err := s.post(ctx, d)
		switch {
		case err == nil && status >= 200 && status < 300:
			s.finish(ctx, d, store.DeliverySent)
			s.log.Info("push sent", "subscription", d.SubscriptionID, "status", status)
			return
		case err == nil && (status == http.StatusNotFound || status == http.StatusGone):
			if err := s.store.DeletePushSubscriptionByID(ctx, d.SubscriptionID); err != nil {
				s.log.Warn("push: can't remove an expired subscription", "subscription", d.SubscriptionID, "err", err)
			}
			s.log.Info("push subscription expired; removed", "subscription", d.SubscriptionID, "status", status)
			return
		case err == nil && status != http.StatusTooManyRequests && status < 500:
			s.finish(ctx, d, store.DeliveryFailed)
			s.log.Warn("push refused; dropped", "subscription", d.SubscriptionID, "status", status)
			return
		}
		if attempt >= len(RetryDelays) || ctx.Err() != nil {
			s.finish(ctx, d, store.DeliveryFailed)
			s.log.Warn("push failed; giving up", "subscription", d.SubscriptionID, "status", status, "err", redact(err), "attempts", attempt+1)
			return
		}
		wait := RetryDelays[attempt]
		if retryAfter > 0 {
			wait = min(retryAfter, maxRetryAfter)
		}
		s.log.Info("push failed; retrying", "subscription", d.SubscriptionID, "status", status, "err", redact(err), "in", wait)
		if s.sleep(ctx, wait) != nil {
			s.finish(context.WithoutCancel(ctx), d, store.DeliveryFailed)
			return
		}
	}
}

func (s *Sender) finish(ctx context.Context, d store.Delivery, status string) {
	if err := s.store.FinishDelivery(ctx, d.OutboxID, d.SubscriptionID, status); err != nil {
		s.log.Warn("push: can't record a delivery", "subscription", d.SubscriptionID, "err", err)
	}
}

// Topic is the push Topic header for a dedupe key: a device that is offline
// keeps only the latest message per event.
func Topic(key string) string {
	sum := sha256.Sum256([]byte(key))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:32]
}

// post encrypts (RFC 8291 aes128gcm) and sends one message with a VAPID
// JWT (RFC 8292). It returns the status and any Retry-After.
func (s *Sender) post(ctx context.Context, d store.Delivery) (int, time.Duration, error) {
	client := s.client
	if s.testPrefix != "" && strings.HasPrefix(d.Endpoint, s.testPrefix) {
		client = s.testClient
	} else if err := CheckEndpoint(d.Endpoint, ""); err != nil {
		return http.StatusBadRequest, 0, nil // treated as a refusal: dropped
	}
	urgency := webpush.UrgencyNormal
	var head struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(d.Payload, &head) == nil && head.Kind == KindAttention {
		urgency = webpush.UrgencyHigh
	}
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	resp, err := webpush.SendNotificationWithContext(ctx, d.Payload, &webpush.Subscription{
		Endpoint: d.Endpoint, Keys: webpush.Keys{P256dh: d.P256dh, Auth: d.Auth},
	}, &webpush.Options{
		HTTPClient: client, Subscriber: strings.TrimPrefix(s.vapid.Subject, "mailto:"),
		VAPIDPublicKey: s.vapid.PublicKey, VAPIDPrivateKey: s.vapid.PrivateKey,
		TTL: pushTTL, Urgency: urgency, Topic: Topic(d.Key),
	})
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, retryAfter(resp.Header.Get("Retry-After"), s.now()), nil
}

func retryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// redact drops the URL from a transport error (url.Error carries the
// endpoint, which must not reach the logs).
func redact(err error) string {
	if err == nil {
		return ""
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return "timeout"
		}
		return "network error"
	}
	return "push request failed"
}
