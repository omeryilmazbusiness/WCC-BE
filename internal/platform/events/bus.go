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
	LeadCreated           = "lead.created"
	LeadConverted         = "lead.converted"
	BookingDrafted        = "booking.draft"
	BookingConfirmed      = "booking.confirmed"
	BookingCancelled      = "booking.cancelled"
	PaymentRecorded       = "payment.recorded"
	TaskCreated           = "task.created"
	MessageReceived       = "inbox.message_received"
	MessageSent           = "inbox.message_sent"
	ConversationAssigned  = "inbox.conversation_assigned"
	ConversationResolved  = "inbox.conversation_resolved"
	SLABreached           = "inbox.sla_breached"
	TaskEscalated         = "task.escalated"
)

// CatalogEntry describes a domain event for admin/docs (T-234).
type CatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Idempotent  bool   `json:"idempotent"`
}

// Catalog returns the canonical in-process event list (pure).
func Catalog() []CatalogEntry {
	return []CatalogEntry{
		{Name: LeadCreated, Description: "Lead created", Idempotent: true},
		{Name: LeadConverted, Description: "Lead converted to booking", Idempotent: true},
		{Name: BookingDrafted, Description: "Booking drafted", Idempotent: true},
		{Name: BookingConfirmed, Description: "Booking confirmed", Idempotent: true},
		{Name: BookingCancelled, Description: "Booking cancelled", Idempotent: true},
		{Name: PaymentRecorded, Description: "Payment ledger entry recorded", Idempotent: true},
		{Name: TaskCreated, Description: "Task created (seeded or manual)", Idempotent: true},
		{Name: TaskEscalated, Description: "Task escalated", Idempotent: true},
		{Name: MessageReceived, Description: "Inbound inbox message", Idempotent: true},
		{Name: MessageSent, Description: "Outbound inbox message", Idempotent: true},
		{Name: ConversationAssigned, Description: "Conversation assigned", Idempotent: true},
		{Name: ConversationResolved, Description: "Conversation resolved", Idempotent: true},
		{Name: SLABreached, Description: "Conversation SLA breached", Idempotent: true},
	}
}
