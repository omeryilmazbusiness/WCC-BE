package events

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
)

// Branched payloads name the branch they belong to (realtime fan-out, ops).
type Branched interface {
	EventBranch() uuid.UUID
}

type ConversationMessageReceivedPayload struct {
	ConversationID uuid.UUID  `json:"conversation_id"`
	MessageID      uuid.UUID  `json:"message_id"`
	BranchID       uuid.UUID  `json:"branch_id"`
	OwnerID        *uuid.UUID `json:"owner_id,omitempty"`
	Channel        string     `json:"channel"`
}

type ConversationRespondedPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	MessageID      uuid.UUID `json:"message_id"`
	BranchID       uuid.UUID `json:"branch_id"`
	ResponderID    uuid.UUID `json:"responder_id"`
	RespondedAt    time.Time `json:"responded_at"`
}

type LeadStageChangedPayload struct {
	LeadID   uuid.UUID `json:"lead_id"`
	BranchID uuid.UUID `json:"branch_id"`
	OwnerID  uuid.UUID `json:"owner_id"`
	From     string    `json:"from"`
	To       string    `json:"to"`
}

type PaymentReversedPayload struct {
	PaymentID  uuid.UUID `json:"payment_id"`
	ReversalID uuid.UUID `json:"reversal_id"`
	BookingID  uuid.UUID `json:"booking_id"`
	BranchID   uuid.UUID `json:"branch_id"`
	Amount     int64     `json:"amount"`
	Currency   string    `json:"currency"`
}

type DocumentStatusChangedPayload struct {
	DocumentID uuid.UUID  `json:"document_id"`
	BranchID   uuid.UUID  `json:"branch_id"`
	BookingID  *uuid.UUID `json:"booking_id,omitempty"`
	From       string     `json:"from"`
	To         string     `json:"to"`
}

type TaskOverduePayload struct {
	TaskID     uuid.UUID `json:"task_id"`
	BranchID   uuid.UUID `json:"branch_id"`
	AssigneeID uuid.UUID `json:"assignee_id"`
	Title      string    `json:"title"`
	DueAt      time.Time `json:"due_at"`
}

type TargetStatusChangedPayload struct {
	TargetID uuid.UUID `json:"target_id"`
	BranchID uuid.UUID `json:"branch_id"`
	Label    string    `json:"label"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	Deficit  int64     `json:"deficit"`
	// VariancePct is how far actual trails expected, in percent (0 when ahead).
	VariancePct int `json:"variance_pct"`
}

type IntegrationFailedPayload struct {
	BranchID  uuid.UUID  `json:"branch_id"`
	Source    string     `json:"source"` // inbox_send | webhook | external
	Provider  string     `json:"provider"`
	AccountID *uuid.UUID `json:"account_id,omitempty"`
	Error     string     `json:"error"`
	At        time.Time  `json:"at"`
}

type ImportCompletedPayload struct {
	ImportJobID uuid.UUID `json:"import_job_id"`
	BranchID    uuid.UUID `json:"branch_id"`
	ActorID     uuid.UUID `json:"actor_id"`
	Status      string    `json:"status"`
	Inserted    int       `json:"inserted"`
	Failed      int       `json:"failed"`
}

func (p ConversationMessageReceivedPayload) EventBranch() uuid.UUID { return p.BranchID }
func (p ConversationRespondedPayload) EventBranch() uuid.UUID       { return p.BranchID }
func (p LeadStageChangedPayload) EventBranch() uuid.UUID            { return p.BranchID }
func (p PaymentReversedPayload) EventBranch() uuid.UUID             { return p.BranchID }
func (p DocumentStatusChangedPayload) EventBranch() uuid.UUID       { return p.BranchID }
func (p TaskOverduePayload) EventBranch() uuid.UUID                 { return p.BranchID }
func (p TargetStatusChangedPayload) EventBranch() uuid.UUID         { return p.BranchID }
func (p IntegrationFailedPayload) EventBranch() uuid.UUID           { return p.BranchID }
func (p ImportCompletedPayload) EventBranch() uuid.UUID             { return p.BranchID }

var durableDecoders = map[string]func([]byte) (any, error){
	ConversationMessageReceived: decodeAs[ConversationMessageReceivedPayload],
	ConversationResponded:       decodeAs[ConversationRespondedPayload],
	LeadStageChanged:            decodeAs[LeadStageChangedPayload],
	PaymentReversed:             decodeAs[PaymentReversedPayload],
	DocumentStatusChanged:       decodeAs[DocumentStatusChangedPayload],
	TaskOverdue:                 decodeAs[TaskOverduePayload],
	TargetStatusChanged:         decodeAs[TargetStatusChangedPayload],
	IntegrationFailed:           decodeAs[IntegrationFailedPayload],
	ImportCompleted:             decodeAs[ImportCompletedPayload],
}

func decodeAs[T any](raw []byte) (any, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// IsDurable reports whether name travels through the outbox.
func IsDurable(name string) bool {
	_, ok := durableDecoders[name]
	return ok
}

// Decode rebuilds a durable event from its outbox row.
func Decode(name string, raw []byte) (Event, error) {
	dec, ok := durableDecoders[name]
	if !ok {
		return Event{}, fmt.Errorf("unknown durable event %q", name)
	}
	p, err := dec(raw)
	if err != nil {
		return Event{}, fmt.Errorf("decode %s: %w", name, err)
	}
	return Event{Name: name, Payload: p}, nil
}

// Encode serializes a durable event for the outbox.
func Encode(ev Event) ([]byte, *uuid.UUID, error) {
	if !IsDurable(ev.Name) {
		return nil, nil, fmt.Errorf("event %q is not durable", ev.Name)
	}
	raw, err := json.Marshal(ev.Payload)
	if err != nil {
		return nil, nil, fmt.Errorf("encode %s: %w", ev.Name, err)
	}
	back, err := durableDecoders[ev.Name](raw)
	if err != nil || reflect.TypeOf(back) != reflect.TypeOf(ev.Payload) {
		return nil, nil, fmt.Errorf("event %s: payload %T does not match its catalog type", ev.Name, ev.Payload)
	}
	var branch *uuid.UUID
	if b, ok := ev.Payload.(Branched); ok && b.EventBranch() != uuid.Nil {
		id := b.EventBranch()
		branch = &id
	}
	return raw, branch, nil
}
