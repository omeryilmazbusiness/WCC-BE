package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	inboxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	taskdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

// Reactor turns domain events into in-app notifications (SRP separate from Service).
type Reactor struct {
	svc *Service
}

func NewReactor(svc *Service) *Reactor {
	return &Reactor{svc: svc}
}

func (r *Reactor) Register(bus *events.Bus) {
	bus.Subscribe(events.SLABreached, r.onSLABreached)
	bus.Subscribe(events.BookingConfirmed, r.onBookingConfirmed)
	bus.Subscribe(events.TaskCreated, r.onTaskCreated)
	bus.Subscribe(events.TaskEscalated, r.onTaskEscalated)
}

func (r *Reactor) onSLABreached(ctx context.Context, ev events.Event) error {
	c, ok := ev.Payload.(*inboxdomain.Conversation)
	if !ok || c == nil {
		return nil
	}
	eid := c.ID
	body := "Unanswered conversation breached SLA"
	if c.LastMessagePreview != "" {
		body = c.LastMessagePreview
	}
	in := EmitInput{
		BranchID: c.BranchID, Kind: domain.KindMessageSLA,
		Title: "Conversation SLA breached", Body: body,
		EntityType: "conversation", EntityID: &eid, HrefHint: "/inbox",
	}
	if c.OwnerID != nil && *c.OwnerID != uuid.Nil {
		in.RecipientUserID = *c.OwnerID
		_, _ = r.svc.Emit(ctx, in)
	}
	_, _ = r.svc.EmitToRoles(ctx, c.BranchID, []string{"manager", "gm"}, in)
	return nil
}

func (r *Reactor) onBookingConfirmed(ctx context.Context, ev events.Event) error {
	b, ok := ev.Payload.(*bookingdomain.Booking)
	if !ok || b == nil {
		return nil
	}
	if b.OwnerID == uuid.Nil {
		return nil
	}
	eid := b.ID
	_, err := r.svc.Emit(ctx, EmitInput{
		BranchID: b.BranchID, RecipientUserID: b.OwnerID,
		Kind: domain.KindBookingConfirmed,
		Title: "Booking confirmed",
		Body:  fmt.Sprintf("Booking %s is confirmed", shortID(b.ID)),
		EntityType: "booking", EntityID: &eid, HrefHint: "/bookings/" + b.ID.String(),
	})
	return err
}

func (r *Reactor) onTaskCreated(ctx context.Context, ev events.Event) error {
	t, ok := ev.Payload.(*taskdomain.Task)
	if !ok || t == nil || t.AssigneeID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	if t.DueAt == nil || !t.DueAt.Before(now) {
		return nil
	}
	eid := t.ID
	_, err := r.svc.Emit(ctx, EmitInput{
		BranchID: t.BranchID, RecipientUserID: t.AssigneeID,
		Kind: domain.KindTaskOverdue, Title: "Task overdue",
		Body: t.Title, EntityType: "task", EntityID: &eid, HrefHint: "/tasks",
	})
	return err
}

func (r *Reactor) onTaskEscalated(ctx context.Context, ev events.Event) error {
	t, ok := ev.Payload.(*taskdomain.Task)
	if !ok || t == nil {
		return nil
	}
	eid := t.ID
	in := EmitInput{
		BranchID: t.BranchID, Kind: domain.KindTaskEscalated,
		Title: "Task escalated", Body: t.Title,
		EntityType: "task", EntityID: &eid, HrefHint: "/tasks",
	}
	if t.AssigneeID != uuid.Nil {
		in.RecipientUserID = t.AssigneeID
		_, _ = r.svc.Emit(ctx, in)
	}
	_, err := r.svc.EmitToRoles(ctx, t.BranchID, []string{"manager", "gm"}, in)
	return err
}

func shortID(id uuid.UUID) string {
	s := id.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}
