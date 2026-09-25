package auth

import (
	"context"
	"sync"
	"time"

	"hostbud/internal/store"
)

// memRepo is an in-memory store.AuthRepository.
type memRepo struct {
	mu       sync.Mutex
	allow    map[string]bool
	users    map[string]store.User // by normalized email
	sessions map[string]store.AuthSession
	limits   map[string]store.RateLimit
}

func newMem() *memRepo {
	return &memRepo{allow: map[string]bool{}, users: map[string]store.User{},
		sessions: map[string]store.AuthSession{}, limits: map[string]store.RateLimit{}}
}

func (m *memRepo) EmailAllowed(_ context.Context, e string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.allow[e], nil
}

func (m *memRepo) CreateUser(_ context.Context, u store.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.EmailNormalized]; ok {
		return store.ErrDuplicate
	}
	m.users[u.EmailNormalized] = u
	return nil
}

func (m *memRepo) UserByEmail(_ context.Context, e string) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[e]
	if !ok {
		return u, store.ErrNotFound
	}
	return u, nil
}

func (m *memRepo) RecordLogin(context.Context, string, time.Time) error { return nil }

func (m *memRepo) CreateSession(_ context.Context, s store.AuthSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.LastSeenAt = s.CreatedAt
	m.sessions[s.IDHash] = s
	return nil
}

func (m *memRepo) SessionUser(_ context.Context, h string, now time.Time) (store.User, store.AuthSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[h]
	if !ok || !s.ExpiresAt.After(now) {
		return store.User{}, s, store.ErrNotFound
	}
	for _, u := range m.users {
		if u.ID == s.UserID && !u.Disabled {
			return u, s, nil
		}
	}
	return store.User{}, s, store.ErrNotFound
}

func (m *memRepo) TouchSession(context.Context, string, time.Time) error { return nil }

func (m *memRepo) DeleteSession(_ context.Context, h string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, h)
	return nil
}

func (m *memRepo) RateLimits(_ context.Context, keys ...string) (map[string]store.RateLimit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]store.RateLimit{}
	for _, k := range keys {
		if rl, ok := m.limits[k]; ok {
			out[k] = rl
		}
	}
	return out, nil
}

func (m *memRepo) UpdateRateLimit(_ context.Context, k string, f func(store.RateLimit) store.RateLimit) (store.RateLimit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rl := m.limits[k]
	rl.Key = k
	rl = f(rl)
	m.limits[k] = rl
	return rl, nil
}

func (m *memRepo) ClearRateLimit(_ context.Context, k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.limits, k)
	return nil
}

// clock is a settable fake time.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }
