package assistant

import (
	"sync"
	"time"
)

// memo keeps recent lookups for a short TTL so a burst of questions reads the
// same facts and provider settings once; bounded so it cannot grow without limit.
type memo[V any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	now     func() time.Time
	entries map[string]memoEntry[V]
}

type memoEntry[V any] struct {
	value   V
	expires time.Time
}

func newMemo[V any](ttl time.Duration, maxEntries int, now func() time.Time) *memo[V] {
	return &memo[V]{ttl: ttl, max: maxEntries, now: now, entries: map[string]memoEntry[V]{}}
}

// get returns the cached value, or loads, stores and returns it; errors are not cached.
func (m *memo[V]) get(key string, load func() (V, error)) (V, error) {
	if m == nil || m.ttl <= 0 {
		return load()
	}
	m.mu.Lock()
	e, ok := m.entries[key]
	m.mu.Unlock()
	if ok && m.now().Before(e.expires) {
		return e.value, nil
	}
	v, err := load()
	if err != nil {
		return v, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if len(m.entries) >= m.max {
		for k, old := range m.entries {
			if !now.Before(old.expires) {
				delete(m.entries, k)
			}
		}
		if len(m.entries) >= m.max {
			m.entries = map[string]memoEntry[V]{}
		}
	}
	m.entries[key] = memoEntry[V]{value: v, expires: now.Add(m.ttl)}
	return v, nil
}
