package events

// In-process events: published after commit, payloads are domain pointers.
const (
	LeadCreated          = "lead.created"
	LeadConverted        = "lead.converted"
	BookingDrafted       = "booking.draft"
	BookingConfirmed     = "booking.confirmed"
	BookingCancelled     = "booking.cancelled"
	BookingStatusChanged = "booking.status_changed"
	PaymentRecorded      = "payment.recorded"
	TaskCreated          = "task.created"
	MessageReceived      = "inbox.message_received"
	MessageSent          = "inbox.message_sent"
	ConversationAssigned = "inbox.conversation_assigned"
	ConversationResolved = "inbox.conversation_resolved"
	SLABreached          = "inbox.sla_breached"
	SLAWarning           = "inbox.sla_warning"
	TaskEscalated        = "task.escalated"
)

// Durable events (T-282): written to the outbox inside the business
// transaction and delivered by the worker dispatcher. Payloads are the JSON
// value types in payloads.go.
const (
	ConversationMessageReceived = "conversation.message_received"
	ConversationResponded       = "conversation.responded"
	LeadStageChanged            = "lead.stage_changed"
	PaymentReversed             = "payment.reversed"
	DocumentStatusChanged       = "document.status_changed"
	TaskOverdue                 = "task.overdue"
	TargetStatusChanged         = "target.status_changed"
	IntegrationFailed           = "integration.failed"
	ImportCompleted             = "import.completed"
)

// CatalogEntry describes a domain event for admin/docs (T-234).
type CatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Idempotent  bool   `json:"idempotent"`
	Durable     bool   `json:"durable"`
}

// Catalog returns the canonical event list (pure).
func Catalog() []CatalogEntry {
	return []CatalogEntry{
		{Name: LeadCreated, Description: "Lead created", Idempotent: true},
		{Name: LeadConverted, Description: "Lead converted to booking", Idempotent: true},
		{Name: BookingDrafted, Description: "Booking drafted", Idempotent: true},
		{Name: BookingConfirmed, Description: "Booking confirmed", Idempotent: true},
		{Name: BookingCancelled, Description: "Booking cancelled", Idempotent: true},
		{Name: BookingStatusChanged, Description: "Booking lifecycle status changed (manual, override or system)", Idempotent: true},
		{Name: PaymentRecorded, Description: "Payment ledger entry recorded", Idempotent: true},
		{Name: TaskCreated, Description: "Task created (seeded or manual)", Idempotent: true},
		{Name: TaskEscalated, Description: "Task escalated", Idempotent: true},
		{Name: MessageReceived, Description: "Inbound inbox message", Idempotent: true},
		{Name: MessageSent, Description: "Outbound inbox message", Idempotent: true},
		{Name: ConversationAssigned, Description: "Conversation assigned", Idempotent: true},
		{Name: ConversationResolved, Description: "Conversation closed (resolved, spam or duplicate)", Idempotent: true},
		{Name: SLAWarning, Description: "Conversation SLA warning threshold (A) reached", Idempotent: true},
		{Name: SLABreached, Description: "Conversation SLA breach threshold (B) reached", Idempotent: true},
		{Name: ConversationMessageReceived, Description: "Customer message received on a conversation", Idempotent: true, Durable: true},
		{Name: ConversationResponded, Description: "Agent replied to a conversation (SLA stops)", Idempotent: true, Durable: true},
		{Name: LeadStageChanged, Description: "Lead moved to another pipeline stage", Idempotent: true, Durable: true},
		{Name: PaymentReversed, Description: "Payment reversed (refund or correction)", Idempotent: true, Durable: true},
		{Name: DocumentStatusChanged, Description: "Document status changed (uploaded, approved, rejected, expired)", Idempotent: true, Durable: true},
		{Name: TaskOverdue, Description: "Task passed its due time", Idempotent: true, Durable: true},
		{Name: TargetStatusChanged, Description: "Revenue target pace status changed", Idempotent: true, Durable: true},
		{Name: IntegrationFailed, Description: "Channel, webhook, file sync or external integration failed", Idempotent: true, Durable: true},
		{Name: ImportCompleted, Description: "Excel import finished", Idempotent: true, Durable: true},
	}
}
