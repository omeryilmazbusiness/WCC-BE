package events

import (
	"context"
	"log/slog"
	"sync"
)

// Event is a domain occurrence published after a successful unit of work.
type Event struct {
	Name    string
	Payload any
}

// Handler processes a domain event. Handlers must be idempotent (MVP requirement).
type Handler func(ctx context.Context, event Event) error

// Bus is an in-process pub/sub (no external message bus in MVP).
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
	log      *slog.Logger
}

func NewBus(log *slog.Logger) *Bus {
	return &Bus{
		handlers: make(map[string][]Handler),
		log:      log,
	}
}

// Subscribe registers an idempotent handler for event name.
func (b *Bus) Subscribe(name string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[name] = append(b.handlers[name], h)
}

// Publish dispatches to all handlers. Failures are logged; callers decide
// whether to surface them. Prefer publishing after successful commit.
func (b *Bus) Publish(ctx context.Context, event Event) {
	b.mu.RLock()
	hs := append([]Handler(nil), b.handlers[event.Name]...)
	b.mu.RUnlock()

	for _, h := range hs {
		if err := h(ctx, event); err != nil {
			b.log.Error("domain event handler failed",
				"event", event.Name,
				"error", err,
			)
		}
	}
}

// Common MVP event names (see plan §7).
const (
	LeadCreated       = "lead.created"
	LeadConverted     = "lead.converted"
	BookingDrafted    = "booking.draft"
	BookingConfirmed  = "booking.confirmed"
	PaymentRecorded   = "payment.recorded"
	TaskCreated       = "task.created"
)
