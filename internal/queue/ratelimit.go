package queue

import (
	"sync"
	"time"
)

// Hook rate limit per run: a token bucket refilled with HookRatePerMinute
// tokens a minute, holding at most HookBurst. A client sends a few hooks per
// turn; this only stops floods.
const (
	HookRatePerMinute = 60
	HookBurst         = 20
)

type bucket struct {
	tokens float64
	last   time.Time
}

// limiter is a token bucket per run id.
type limiter struct {
	mu      sync.Mutex
	perSec  float64
	burst   float64
	buckets map[string]*bucket
}

func newLimiter(perMinute, burst int) *limiter {
	return &limiter{perSec: float64(perMinute) / 60, burst: float64(burst), buckets: map[string]*bucket{}}
}

// allow takes one token from key's bucket if there is one.
func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) > 10_000 {
			l.prune(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.perSec)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// prune drops buckets that have refilled completely (idle runs).
func (l *limiter) prune(now time.Time) {
	for key, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.perSec >= l.burst {
			delete(l.buckets, key)
		}
	}
}
