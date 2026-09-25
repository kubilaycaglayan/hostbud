// Package auth implements whitelist-gated email/password accounts,
// server-side sessions and login throttling (docs/ARCHITECTURE.md §8.1–8.2).
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"hostbud/internal/store"
)

// Errors returned to the API. Messages are generic on purpose: they never
// reveal whether an account exists or an address is whitelisted.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrRegistrationFailed = errors.New("registration isn't possible with this email address")
	ErrUnauthenticated    = errors.New("sign in required")
)

// RateLimitedError means the caller must wait RetryAfter.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many attempts; try again in %d seconds", RetryAfterSeconds(e.RetryAfter))
}

// Config configures the service.
type Config struct {
	SessionTTL time.Duration
	Limits     Limits
	Params     Params // Argon2id cost
	Key        []byte // keys the rate-limit bucket hashes (install-local secret)
	Log        *slog.Logger
	Now        func() time.Time // tests
}

// Service handles registration, sign-in, sessions and throttling.
type Service struct {
	repo   store.AuthRepository
	ttl    time.Duration
	limits Limits
	params Params
	key    []byte
	log    *slog.Logger
	now    func() time.Time
	dummy  string // hash verified when the user doesn't exist (constant timing)
}

// New returns a Service.
func New(repo store.AuthRepository, cfg Config) (*Service, error) {
	if len(cfg.Key) < 16 {
		return nil, errors.New("auth: key must be at least 16 bytes")
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.Params == (Params{}) {
		cfg.Params = DefaultParams
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	dummy, err := HashPassword("hostbud-dummy-password", cfg.Params)
	if err != nil {
		return nil, err
	}
	return &Service{repo: repo, ttl: cfg.SessionTTL, limits: cfg.Limits, params: cfg.Params,
		key: cfg.Key, log: cfg.Log, now: cfg.Now, dummy: dummy}, nil
}

// SessionTTL is how long a new session lasts.
func (s *Service) SessionTTL() time.Duration { return s.ttl }

// Register creates an account for an enabled whitelisted address. It does
// not sign in.
func (s *Service) Register(ctx context.Context, email, password, ip string) error {
	norm, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := CheckPassword(password); err != nil {
		return err
	}
	buckets := s.registerBuckets(norm, ip)
	if wait, err := s.checkBlocked(ctx, buckets); err != nil {
		return err
	} else if wait > 0 {
		s.log.Info("registration throttled", "retry_after_s", RetryAfterSeconds(wait))
		return &RateLimitedError{RetryAfter: wait}
	}

	// Hash before the whitelist check so both outcomes take the same time.
	hash, err := HashPassword(password, s.params)
	if err != nil {
		return err
	}
	allowed, err := s.repo.EmailAllowed(ctx, norm)
	if err != nil {
		return err
	}
	if allowed {
		err = s.repo.CreateUser(ctx, store.User{
			ID: newULID(s.now()), Email: strings.TrimSpace(email), EmailNormalized: norm, PasswordHash: hash,
		})
		if err == nil {
			s.log.Info("account registered", "email", Redact(norm))
			return nil
		}
		if !errors.Is(err, store.ErrDuplicate) {
			return err
		}
	}
	s.log.Info("registration refused", "email", Redact(norm))
	if err := s.recordFailure(ctx, buckets); err != nil {
		return err
	}
	return ErrRegistrationFailed
}

// Login verifies credentials and whitelist status and returns a new session
// token. Any previous session (oldToken) is revoked: sign-in rotates.
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent, oldToken string) (string, time.Time, error) {
	norm, err := NormalizeEmail(email)
	if err != nil {
		norm = strings.ToLower(strings.TrimSpace(email))
	}
	buckets := s.loginBuckets(norm, ip)
	if wait, err := s.checkBlocked(ctx, buckets); err != nil {
		return "", time.Time{}, err
	} else if wait > 0 {
		s.log.Info("sign-in throttled", "retry_after_s", RetryAfterSeconds(wait))
		return "", time.Time{}, &RateLimitedError{RetryAfter: wait}
	}

	u, err := s.repo.UserByEmail(ctx, norm)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", time.Time{}, err
	}
	ok := false
	if err == nil {
		ok, _ = VerifyPassword(u.PasswordHash, password)
	} else {
		_, _ = VerifyPassword(s.dummy, password)
	}
	if ok {
		allowed, err := s.repo.EmailAllowed(ctx, norm)
		if err != nil {
			return "", time.Time{}, err
		}
		ok = allowed && !u.Disabled
	}
	if !ok {
		s.log.Info("sign-in failed", "email", Redact(norm))
		if err := s.recordFailure(ctx, buckets); err != nil {
			return "", time.Time{}, err
		}
		return "", time.Time{}, ErrInvalidCredentials
	}

	// Success clears this address's failures, not an IP-wide block.
	if err := s.repo.ClearRateLimit(ctx, buckets[0].key); err != nil {
		return "", time.Time{}, err
	}
	if oldToken != "" {
		_ = s.repo.DeleteSession(ctx, hashToken(oldToken))
	}
	now := s.now()
	token := newToken()
	expires := now.Add(s.ttl)
	if err := s.repo.CreateSession(ctx, store.AuthSession{
		IDHash: hashToken(token), UserID: u.ID, ExpiresAt: expires, CreatedAt: now,
		UserAgent: truncate(userAgent, 256), CreatedIP: ip,
	}); err != nil {
		return "", time.Time{}, err
	}
	if err := s.repo.RecordLogin(ctx, u.ID, now); err != nil {
		return "", time.Time{}, err
	}
	s.log.Info("signed in", "user", u.ID)
	return token, expires, nil
}

// Authenticate returns the user of a valid session token.
func (s *Service) Authenticate(ctx context.Context, token string) (store.User, error) {
	if token == "" {
		return store.User{}, ErrUnauthenticated
	}
	now := s.now()
	u, a, err := s.repo.SessionUser(ctx, hashToken(token), now)
	if errors.Is(err, store.ErrNotFound) {
		return u, ErrUnauthenticated
	}
	if err != nil {
		return u, err
	}
	if now.Sub(a.LastSeenAt) > time.Minute {
		_ = s.repo.TouchSession(ctx, a.IDHash, now)
	}
	return u, nil
}

// Logout revokes the session.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.repo.DeleteSession(ctx, hashToken(token))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
