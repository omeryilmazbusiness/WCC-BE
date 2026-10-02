package flight

import (
	"sync"
	"time"
)

// ttlCache is a small bounded in-memory cache; when full, expired entries go
// first and then the oldest one.
type ttlCache[V any] struct {
	ttl time.Duration
	max int
	now func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry[V]
}

type cacheEntry[V any] struct {
	value  V
	stored time.Time
}

func newTTLCache[V any](ttl time.Duration, max int, now func() time.Time) *ttlCache[V] {
	return &ttlCache[V]{ttl: ttl, max: max, now: now, entries: map[string]cacheEntry[V]{}}
}

func (c *ttlCache[V]) get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || c.now().Sub(e.stored) > c.ttl {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *ttlCache[V]) put(key string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.max {
		c.evict(now)
	}
	c.entries[key] = cacheEntry[V]{value: v, stored: now}
}

func (c *ttlCache[V]) evict(now time.Time) {
	oldestKey, oldest := "", now
	for k, e := range c.entries {
		if now.Sub(e.stored) > c.ttl {
			delete(c.entries, k)
			continue
		}
		if !e.stored.After(oldest) {
			oldestKey, oldest = k, e.stored
		}
	}
	if len(c.entries) >= c.max && oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
