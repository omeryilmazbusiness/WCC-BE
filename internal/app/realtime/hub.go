// Package realtime fans server-side changes out to connected clients (T-288).
package realtime

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

const (
	TypeNotification = "notification"
	TypeInvalidate   = "invalidate"
)

// Signal is a change announcement. UserID targets one user; otherwise
// BranchID limits delivery to users who can see that branch.
type Signal struct {
	Type     string     `json:"type"`
	UserID   *uuid.UUID `json:"user_id,omitempty"`
	BranchID *uuid.UUID `json:"branch_id,omitempty"`
	Topic    string     `json:"topic"`
}

// Announcer publishes a signal to every API replica.
type Announcer interface {
	Announce(ctx context.Context, s Signal) error
}

// Subscriber identifies a connected stream.
type Subscriber struct {
	UserID uuid.UUID
	Scope  access.Scope
}

// Visible reports whether s may be delivered to sub; this is the only
// authorization check on the stream, so it must fail closed.
func (sub Subscriber) Visible(s Signal) bool {
	if s.UserID != nil {
		return *s.UserID == sub.UserID
	}
	if s.BranchID == nil {
		return sub.Scope.Level >= access.LevelGlobal
	}
	return sub.Scope.CanAccessBranch(*s.BranchID)
}

// Hub keeps connected streams in memory; one per API process.
type Hub struct {
	mu     sync.RWMutex
	subs   map[*stream]struct{}
	buffer int
	max    int
}

type stream struct {
	sub Subscriber
	ch  chan Signal
}

// NewHub bounds per-stream buffering and the number of open streams.
func NewHub(buffer, maxStreams int) *Hub {
	if buffer < 1 {
		buffer = 16
	}
	if maxStreams < 1 {
		maxStreams = 1000
	}
	return &Hub{subs: map[*stream]struct{}{}, buffer: buffer, max: maxStreams}
}

// Subscribe registers a stream; ok is false when the hub is full. The
// returned cancel must be called when the client disconnects.
func (h *Hub) Subscribe(sub Subscriber) (<-chan Signal, func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= h.max {
		return nil, func() {}, false
	}
	s := &stream{sub: sub, ch: make(chan Signal, h.buffer)}
	h.subs[s] = struct{}{}
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, s)
			h.mu.Unlock()
		})
	}, true
}

// Broadcast delivers s to every visible stream. A slow client drops the
// signal rather than blocking the others; clients refetch on the next one.
func (h *Hub) Broadcast(s Signal) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for st := range h.subs {
		if !st.sub.Visible(s) {
			continue
		}
		select {
		case st.ch <- s:
		default:
		}
	}
}

// Len is the number of open streams.
func (h *Hub) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
