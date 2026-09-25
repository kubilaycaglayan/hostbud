package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"time"

	"hostbud/internal/store"
)

// Limits configure throttling (docs/ARCHITECTURE.md §8.2).
type Limits struct {
	LoginMax    int           // failed sign-ins per email+IP before blocking
	RegisterMax int           // failed registrations per email+IP before blocking
	IPMax       int           // failures per IP (any email) before blocking
	BlockBase   time.Duration // first block
	BlockMax    time.Duration // ceiling
	Multiplier  float64       // growth per further hit
	Window      time.Duration // failures older than this are forgotten
}

// DefaultLimits are the production defaults.
var DefaultLimits = Limits{
	LoginMax: 5, RegisterMax: 10, IPMax: 20,
	BlockBase: 30 * time.Second, BlockMax: time.Hour, Multiplier: 2, Window: time.Hour,
}

type bucket struct {
	key string
	max int
}

// scopeKey is a keyed hash: rate-limit rows never hold raw emails or IPs.
func (s *Service) scopeKey(parts ...string) string {
	m := hmac.New(sha256.New, s.key)
	for _, p := range parts {
		m.Write([]byte(p))
		m.Write([]byte{0})
	}
	return hex.EncodeToString(m.Sum(nil))[:40]
}

func (s *Service) loginBuckets(email, ip string) []bucket {
	return []bucket{{s.scopeKey("login", email, ip), s.limits.LoginMax}, {s.scopeKey("ip", ip), s.limits.IPMax}}
}

func (s *Service) registerBuckets(email, ip string) []bucket {
	return []bucket{{s.scopeKey("register", email, ip), s.limits.RegisterMax}, {s.scopeKey("ip", ip), s.limits.IPMax}}
}

// blockFor is BlockBase·Multiplier^n, capped at BlockMax.
func (l Limits) blockFor(n int) time.Duration {
	d := float64(l.BlockBase) * math.Pow(l.Multiplier, float64(n))
	if d > float64(l.BlockMax) || math.IsInf(d, 0) {
		return l.BlockMax
	}
	return time.Duration(d)
}

// fail counts one failure (or one hit while blocked) on a bucket; once the
// count reaches max, each further one blocks for exponentially longer.
func (l Limits) fail(rl store.RateLimit, max int, now time.Time) store.RateLimit {
	blocked := rl.BlockedUntil.After(now)
	if !blocked && !rl.LastFailureAt.IsZero() && now.Sub(rl.LastFailureAt) > l.Window {
		rl.Failures = 0
	}
	rl.Failures++
	rl.LastFailureAt = now
	if rl.Failures >= max {
		rl.BlockedUntil = now.Add(l.blockFor(rl.Failures - max))
	}
	return rl
}

// checkBlocked returns how long the caller must wait, counting this attempt
// as another hit on every bucket that is currently blocked. It runs before
// any password verification.
func (s *Service) checkBlocked(ctx context.Context, buckets []bucket) (time.Duration, error) {
	keys := make([]string, len(buckets))
	for i, b := range buckets {
		keys[i] = b.key
	}
	rls, err := s.repo.RateLimits(ctx, keys...)
	if err != nil {
		return 0, err
	}
	now := s.now()
	var wait time.Duration
	for _, b := range buckets {
		if !rls[b.key].BlockedUntil.After(now) {
			continue
		}
		rl, err := s.repo.UpdateRateLimit(ctx, b.key, func(rl store.RateLimit) store.RateLimit {
			return s.limits.fail(rl, b.max, now)
		})
		if err != nil {
			return 0, err
		}
		wait = max(wait, rl.BlockedUntil.Sub(now))
	}
	return wait, nil
}

// recordFailure counts a failed attempt on every bucket.
func (s *Service) recordFailure(ctx context.Context, buckets []bucket) error {
	now := s.now()
	for _, b := range buckets {
		if _, err := s.repo.UpdateRateLimit(ctx, b.key, func(rl store.RateLimit) store.RateLimit {
			return s.limits.fail(rl, b.max, now)
		}); err != nil {
			return err
		}
	}
	return nil
}

// RetryAfterSeconds rounds a wait up to whole seconds (at least 1).
func RetryAfterSeconds(d time.Duration) int {
	return max(1, int(math.Ceil(d.Seconds())))
}
