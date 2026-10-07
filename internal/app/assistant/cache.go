package assistant

import (
	"sync"
	"time"
)

type cacheEntry struct {
	value   string
	expires time.Time
	seq     uint64
}

// MemoryCache is a bounded TTL cache; per instance, which is enough because a
// miss only costs one model call.
type MemoryCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	now     func() time.Time
	seq     uint64
	entries map[string]cacheEntry
}

func NewMemoryCache(ttl time.Duration, maxEntries int) *MemoryCache {
	return &MemoryCache{ttl: ttl, max: maxEntries, now: time.Now, entries: map[string]cacheEntry{}}
}

func (c *MemoryCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if c.now().After(e.expires) {
		delete(c.entries, key)
		return "", false
	}
	return e.value, true
}

func (c *MemoryCache) Put(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.max {
		c.evict(now)
	}
	c.seq++
	c.entries[key] = cacheEntry{value: value, expires: now.Add(c.ttl), seq: c.seq}
}

// evict drops expired entries, then the one closest to expiry (oldest insert on a tie) if still full.
func (c *MemoryCache) evict(now time.Time) {
	var oldestKey string
	var oldest cacheEntry
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
			continue
		}
		if oldestKey == "" || e.expires.Before(oldest.expires) || (e.expires.Equal(oldest.expires) && e.seq < oldest.seq) {
			oldestKey, oldest = k, e
		}
	}
	if len(c.entries) >= c.max && oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
