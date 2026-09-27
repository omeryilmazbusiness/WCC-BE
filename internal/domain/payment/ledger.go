package payment

import (
	"fmt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// StatusTransition is one allowed status change of a ledger entry.
type StatusTransition struct {
	EventType EventType
	From      Status
	To        Status
}

// StatusTransitions is the complete whitelist of ledger status changes. The
// payments_ledger_guard trigger (latest definition: migration 00027) enforces
// the same list.
var StatusTransitions = []StatusTransition{
	{EventCharge, StatusUnverified, StatusVerified},
	{EventRefund, StatusPendingApproval, StatusApproved},
	{EventRefund, StatusPendingApproval, StatusRejected},
}

// ImmutableColumns are fixed at INSERT; payments_ledger_guard rejects any
// change. approved_by, approved_at and note may be set once; status follows
// StatusTransitions.
var ImmutableColumns = []string{
	"id", "booking_id", "amount", "currency", "method", "reference", "recorded_by",
	"idempotency_key", "created_at", "event_type", "reverses_payment_id",
	"received_at", "amount_reporting", "reporting_currency", "fx_rate_scaled", "fx_effective_date",
}

// CanTransition reports whether an entry of eventType may move from -> to.
func CanTransition(eventType EventType, from, to Status) bool {
	for _, t := range StatusTransitions {
		if t.EventType == eventType && t.From == from && t.To == to {
			return true
		}
	}
	return false
}

// TransitionTo validates and applies a status change on p.
func (p *Payment) TransitionTo(to Status) error {
	if !CanTransition(p.EventType, p.Status, to) {
		return shared.NewInvalidState(fmt.Sprintf("payment %s cannot move from %s to %s", p.EventType, p.Status, to))
	}
	p.Status = to
	return nil
}

// RequiresApprover reports whether reaching to must record approved_by/approved_at.
func RequiresApprover(to Status) bool {
	return to == StatusApproved || to == StatusRejected
}
