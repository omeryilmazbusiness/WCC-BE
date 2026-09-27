package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
)

// Event is a domain occurrence published after a successful unit of work.
type Event struct {
	Name    string
	Payload any
}

// Handler processes a domain event. Handlers must be idempotent: durable
// events are redelivered to every handler when any of them fails.
type Handler func(ctx context.Context, event Event) error

// Bus is the in-process pub/sub. Durable events reach it through the outbox
// dispatcher; the rest are published directly after commit.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
	log      *slog.Logger
}

func NewBus(log *slog.Logger) *Bus {
	if log == nil {
		log = slog.Default()
	}
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

// Subscribed reports whether any handler listens to name.
func (b *Bus) Subscribed(name string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.handlers[name]) > 0
}

// Publish dispatches to all handlers; failures are logged, never returned.
func (b *Bus) Publish(ctx context.Context, event Event) {
	if err := b.Deliver(ctx, event); err != nil {
		b.log.Error("domain event handler failed", "event", event.Name, "error", err)
	}
}

// Deliver runs every handler and returns their joined errors so the outbox
// dispatcher can retry. A panicking handler is reported as an error.
func (b *Bus) Deliver(ctx context.Context, event Event) error {
	b.mu.RLock()
	hs := append([]Handler(nil), b.handlers[event.Name]...)
	b.mu.RUnlock()

	// Reactors are trusted system work: they act on behalf of the platform,
	// not the caller, so they run with system scope.
	hctx := audit.AsSystem(access.WithScope(ctx, access.System()))
	var errs []error
	for _, h := range hs {
		if err := safeCall(hctx, h, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func safeCall(ctx context.Context, h Handler, ev Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return h(ctx, ev)
}

// Outbox persists a durable event. Add must run inside the caller's
// transaction so the event commits (or rolls back) with the business change.
type Outbox interface {
	Add(ctx context.Context, event Event) error
}

// Record writes ev to the outbox; a nil outbox (unit tests, tools) is a no-op.
func Record(ctx context.Context, o Outbox, ev Event) error {
	if o == nil {
		return nil
	}
	return o.Add(ctx, ev)
}
