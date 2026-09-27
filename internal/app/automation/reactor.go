package automation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	inboxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	notificationdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	taskdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

// Reactor wires the Epic 22 subscriptions (T-283, T-284, T-286). Durable
// events arrive through the outbox dispatcher, in-process ones after commit;
// every handler is idempotent because both paths may redeliver.
type Reactor struct {
	alerts   *Alerts
	tasks    RuleTasks
	bookings BookingRecomputer
	targets  TargetRecomputer
	docs     Checklists
}

func NewReactor(alerts *Alerts, tasks RuleTasks, bookings BookingRecomputer, targets TargetRecomputer, docs Checklists) *Reactor {
	return &Reactor{alerts: alerts, tasks: tasks, bookings: bookings, targets: targets, docs: docs}
}

func (r *Reactor) Register(bus *events.Bus) {
	bus.Subscribe(events.PaymentReversed, r.onPaymentReversed)
	bus.Subscribe(events.DocumentStatusChanged, r.onDocumentStatusChanged)
	bus.Subscribe(events.ConversationResponded, r.onConversationResponded)
	bus.Subscribe(events.ConversationResolved, r.onConversationResolved)
	bus.Subscribe(events.TargetStatusChanged, r.onTargetStatusChanged)
	bus.Subscribe(events.TaskOverdue, r.onTaskOverdue)
	bus.Subscribe(events.IntegrationFailed, r.onIntegrationFailed)
	bus.Subscribe(events.ImportCompleted, r.onImportCompleted)
	bus.Subscribe(events.LeadStageChanged, r.onLeadStageChanged)
	bus.Subscribe(events.SLABreached, r.onSLABreached)
	bus.Subscribe(events.SLAWarning, r.onSLAWarning)
}

func (r *Reactor) onPaymentReversed(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(events.PaymentReversedPayload)
	if !ok {
		return nil
	}
	return errors.Join(
		r.bookings.RecomputeBooking(ctx, p.BookingID),
		r.targets.RecomputeTargets(ctx, p.BranchID),
	)
}

// onDocumentStatusChanged re-derives readiness and closes the document tasks
// once the booking checklist has nothing required left.
func (r *Reactor) onDocumentStatusChanged(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(events.DocumentStatusChangedPayload)
	if !ok || p.BookingID == nil {
		return nil
	}
	bookingID := *p.BookingID
	if err := r.bookings.RecomputeBooking(ctx, bookingID); err != nil {
		return err
	}
	cl, err := r.docs.Checklist(ctx, bookingID)
	if err != nil || cl == nil || len(cl.MissingRequired) > 0 {
		return err
	}
	for _, rule := range []string{taskdomain.RuleMissingDocument, taskdomain.RuleBookingDocuments} {
		if _, err := r.tasks.CloseByRule(ctx, rule, apptask.RelatedBooking, bookingID, "documents complete"); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reactor) onConversationResponded(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(events.ConversationRespondedPayload)
	if !ok {
		return nil
	}
	_, err := r.tasks.CloseByRule(ctx, taskdomain.RuleUnansweredMessage, apptask.RelatedConversation, p.ConversationID, "responded")
	return err
}

func (r *Reactor) onConversationResolved(ctx context.Context, ev events.Event) error {
	c, ok := ev.Payload.(*inboxdomain.Conversation)
	if !ok || c == nil {
		return nil
	}
	_, err := r.tasks.CloseByRule(ctx, taskdomain.RuleUnansweredMessage, apptask.RelatedConversation, c.ID, string(c.Status))
	return err
}

// onTargetStatusChanged alerts managers and opens a recovery task when a
// target falls behind; other transitions need no action.
func (r *Reactor) onTargetStatusChanged(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(events.TargetStatusChangedPayload)
	if !ok || p.To != "behind" {
		return nil
	}
	a := r.alerts
	err := a.once(ctx, ledgerTargetBehind, p.TargetID.String(), a.today(), func(ctx context.Context) error {
		return a.notifyTargetBehind(ctx, p.BranchID, p.TargetID, p.Label, p.Deficit)
	})
	if err != nil {
		return err
	}
	assignee, err := a.firstStaff(ctx, p.BranchID, managerRoles)
	if err != nil || assignee == uuid.Nil {
		return err
	}
	return r.tasks.EnsureTargetRecoveryTask(ctx, p.BranchID, p.TargetID, assignee, p.Label, p.Deficit)
}

func (r *Reactor) onTaskOverdue(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(events.TaskOverduePayload)
	if !ok {
		return nil
	}
	id := p.TaskID
	return r.alerts.emitTo(ctx, p.AssigneeID, appnotification.EmitInput{
		BranchID: p.BranchID, Kind: notificationdomain.KindTaskOverdue, Title: "Task overdue",
		Body: p.Title, EntityType: "task", EntityID: &id, HrefHint: "/tasks",
		Meta: map[string]any{"due_at": p.DueAt},
	})
}

func (r *Reactor) onIntegrationFailed(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(events.IntegrationFailedPayload)
	if !ok {
		return nil
	}
	in := appnotification.EmitInput{
		Kind: notificationdomain.KindIntegrationDown, Title: "Integration failure",
		Body: p.Provider + ": " + p.Error, EntityType: "integration", EntityID: p.AccountID,
		HrefHint: "/integrations",
		Meta:     map[string]any{"source": p.Source, "provider": p.Provider, "at": p.At},
	}
	_, err := r.alerts.notify.EmitToRoles(ctx, p.BranchID, integrationRoles, in)
	return err
}

func (r *Reactor) onImportCompleted(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(events.ImportCompletedPayload)
	if !ok {
		return nil
	}
	id := p.ImportJobID
	in := appnotification.EmitInput{
		BranchID: p.BranchID, Kind: notificationdomain.KindImportCompleted, Title: "Import finished",
		Body:       importSummary(p),
		EntityType: "import_job", EntityID: &id, HrefHint: "/import-export",
		Meta: map[string]any{"status": p.Status, "inserted": p.Inserted, "failed": p.Failed},
	}
	if p.Status == "failed" {
		in.Severity = notificationdomain.SeverityWarning
	}
	return r.alerts.emitTo(ctx, p.ActorID, in)
}

func importSummary(p events.ImportCompletedPayload) string {
	if p.Status == "failed" {
		return "Import failed"
	}
	return fmt.Sprintf("Imported %d rows, %d failed", p.Inserted, p.Failed)
}

func (r *Reactor) onLeadStageChanged(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(events.LeadStageChangedPayload)
	if !ok || (p.To != "won" && p.To != "lost") {
		return nil
	}
	_, err := r.tasks.CloseByRule(ctx, taskdomain.RuleLeadFollowUp, "lead", p.LeadID, p.To)
	return err
}

// onSLABreached opens the unanswered-message task for the owner (SLA B).
func (r *Reactor) onSLABreached(ctx context.Context, ev events.Event) error {
	c, ok := ev.Payload.(*inboxdomain.Conversation)
	if !ok || c == nil || c.OwnerID == nil || *c.OwnerID == uuid.Nil {
		return nil
	}
	since := time.Now().UTC()
	if c.SLAStartedAt != nil {
		since = *c.SLAStartedAt
	}
	return r.tasks.EnsureUnansweredTask(ctx, c.BranchID, c.ID, *c.OwnerID, since)
}

// onSLAWarning warns the owner, or the managers of an unassigned conversation (SLA A).
func (r *Reactor) onSLAWarning(ctx context.Context, ev events.Event) error {
	c, ok := ev.Payload.(*inboxdomain.Conversation)
	if !ok || c == nil {
		return nil
	}
	id := c.ID
	in := appnotification.EmitInput{
		BranchID: c.BranchID, Kind: notificationdomain.KindMessageSLAWarning,
		Title: "Conversation nearing SLA", Body: c.LastMessagePreview,
		EntityType: "conversation", EntityID: &id, HrefHint: "/inbox",
	}
	if c.OwnerID != nil && *c.OwnerID != uuid.Nil {
		return r.alerts.emitTo(ctx, *c.OwnerID, in)
	}
	_, err := r.alerts.notify.EmitToRoles(ctx, c.BranchID, managerRoles, in)
	return err
}
