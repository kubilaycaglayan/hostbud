package tsauth

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

const maxEntries = 1024

type lookup func(context.Context, string) (string, error)
type cacheEntry struct {
	ip, login string
	unknown   bool
	expires   time.Time
}
type Cache struct {
	mu     sync.Mutex
	items  map[string]*list.Element
	lru    *list.List
	now    func() time.Time
	lookup lookup
}

func NewCache(query func(context.Context, string) (string, error)) *Cache {
	return &Cache{items: map[string]*list.Element{}, lru: list.New(), now: time.Now, lookup: query}
}

func (c *Cache) Login(ctx context.Context, ip string) (string, error) {
	return c.LoginFor(ctx, ip, "")
}

// LoginFor uses a short negative TTL when the returned identity isn't allowlisted.
func (c *Cache) LoginFor(ctx context.Context, ip, allowlist string) (string, error) {
	now := c.now()
	c.mu.Lock()
	if el := c.items[ip]; el != nil {
		entry := el.Value.(*cacheEntry)
		if now.Before(entry.expires) {
			c.lru.MoveToFront(el)
			login := entry.login
			unknown := entry.unknown
			c.mu.Unlock()
			if unknown {
				return "", ErrUnknown
			}
			return login, nil
		}
		c.lru.Remove(el)
		delete(c.items, ip)
	}
	c.mu.Unlock()
	login, err := c.lookup(ctx, ip)
	unknown := errors.Is(err, ErrUnknown)
	if err != nil && !unknown {
		return "", err
	}
	ttl := 10 * time.Second
	if !unknown && Allowed(allowlist, login) {
		ttl = time.Minute
	}
	c.mu.Lock()
	if el := c.items[ip]; el != nil {
		c.lru.Remove(el)
	}
	el := c.lru.PushFront(&cacheEntry{ip: ip, login: login, unknown: unknown, expires: now.Add(ttl)})
	c.items[ip] = el
	if c.lru.Len() > maxEntries {
		old := c.lru.Back()
		delete(c.items, old.Value.(*cacheEntry).ip)
		c.lru.Remove(old)
	}
	c.mu.Unlock()
	if unknown {
		return "", ErrUnknown
	}
	return login, nil
}
