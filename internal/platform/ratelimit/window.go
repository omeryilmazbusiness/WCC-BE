// Package ratelimit provides sliding-window event counters backed by Redis
// with an in-process fallback.
package ratelimit

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Window counts events per key over a trailing duration.
type Window interface {
	// Hit records one event and returns the count inside window including it.
	Hit(ctx context.Context, key string, window time.Duration) (int, error)
	// Count returns events inside window without recording one.
	Count(ctx context.Context, key string, window time.Duration) (int, error)
	Reset(ctx context.Context, key string) error
}

// Memory is a per-process Window for local runs and Redis outages.
type Memory struct {
	mu     sync.Mutex
	events map[string][]time.Time
	now    func() time.Time
}

func NewMemory() *Memory {
	return &Memory{events: map[string][]time.Time{}, now: time.Now}
}

func (m *Memory) Hit(_ context.Context, key string, window time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	kept := m.prune(key, now, window)
	kept = append(kept, now)
	m.events[key] = kept
	return len(kept), nil
}

func (m *Memory) Count(_ context.Context, key string, window time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.prune(key, m.now(), window)
	if len(kept) == 0 {
		delete(m.events, key)
	} else {
		m.events[key] = kept
	}
	return len(kept), nil
}

func (m *Memory) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.events, key)
	m.mu.Unlock()
	return nil
}

func (m *Memory) prune(key string, now time.Time, window time.Duration) []time.Time {
	cutoff := now.Add(-window)
	ev := m.events[key]
	i := 0
	for i < len(ev) && !ev[i].After(cutoff) {
		i++
	}
	return ev[i:]
}

// Resilient uses primary (Redis) and falls back to secondary (Memory) when
// the primary errors, so throttling degrades to per-instance instead of off.
type Resilient struct {
	primary   Window
	secondary Window
	log       *slog.Logger
}

func NewResilient(primary, secondary Window, log *slog.Logger) *Resilient {
	return &Resilient{primary: primary, secondary: secondary, log: log}
}

func (r *Resilient) Hit(ctx context.Context, key string, window time.Duration) (int, error) {
	if r.primary != nil {
		n, err := r.primary.Hit(ctx, key, window)
		if err == nil {
			return n, nil
		}
		r.warn("hit", err)
	}
	return r.secondary.Hit(ctx, key, window)
}

func (r *Resilient) Count(ctx context.Context, key string, window time.Duration) (int, error) {
	if r.primary != nil {
		n, err := r.primary.Count(ctx, key, window)
		if err == nil {
			return n, nil
		}
		r.warn("count", err)
	}
	return r.secondary.Count(ctx, key, window)
}

func (r *Resilient) Reset(ctx context.Context, key string) error {
	_ = r.secondary.Reset(ctx, key)
	if r.primary == nil {
		return nil
	}
	if err := r.primary.Reset(ctx, key); err != nil {
		r.warn("reset", err)
	}
	return nil
}

func (r *Resilient) warn(op string, err error) {
	if r.log != nil {
		r.log.Warn("rate limit store unavailable; using in-memory fallback", "op", op, "error", err)
	}
}
