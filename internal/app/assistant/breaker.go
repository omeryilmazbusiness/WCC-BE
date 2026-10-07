package assistant

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// breaker remembers branches whose provider just failed (timeout, rate limit,
// outage) and keeps them on essential answers for a cooldown.
type breaker struct {
	mu       sync.Mutex
	cooldown time.Duration
	now      func() time.Time
	until    map[uuid.UUID]time.Time
}

func newBreaker(cooldown time.Duration, now func() time.Time) *breaker {
	return &breaker{cooldown: cooldown, now: now, until: map[uuid.UUID]time.Time{}}
}

func (b *breaker) open(branchID uuid.UUID) bool {
	if b.cooldown <= 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.until[branchID]
	if ok && !b.now().Before(t) {
		delete(b.until, branchID)
		return false
	}
	return ok
}

func (b *breaker) trip(branchID uuid.UUID) {
	if b.cooldown <= 0 {
		return
	}
	b.mu.Lock()
	b.until[branchID] = b.now().Add(b.cooldown)
	b.mu.Unlock()
}
