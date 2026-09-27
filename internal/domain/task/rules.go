package task

import (
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Automation rules that create tasks; persisted in tasks.source_rule and
// used to close a rule's open tasks once the underlying issue resolves.
const (
	RuleLeadFollowUp         = "lead.follow_up"
	RuleLeadConverted        = "lead.converted"
	RuleBookingDocuments     = "booking.documents"
	RuleBookingPayment       = "booking.payment"
	RulePaymentDue           = "payment.due"
	RuleHoldExpired          = "booking.hold_expired"
	RuleTargetRecovery       = "target.recovery"
	RulePaymentPromise       = "payment.promise"
	RulePaymentPromiseBroken = "payment.promise_broken"
	RuleConversationOutcome  = "conversation.outcome"
	RuleUnansweredMessage    = "inbox.unanswered"
	RuleVisaFollowUp         = "visa.follow_up"
	RuleSupplierConfirm      = "supplier.confirm"
	RuleMissingDocument      = "booking.missing_document"
)

// JobOverdueSweep announces newly overdue tasks (task.overdue) once each.
const JobOverdueSweep shared.JobName = "task.overdue_sweep"

// Grace is how long after due_at a task of this rule may stay open before it
// escalates; escalation rules configured per branch override it.
func Grace(rule string, overrides map[string]time.Duration) time.Duration {
	if g, ok := overrides[rule]; ok && g > 0 {
		return g
	}
	switch rule {
	case RuleUnansweredMessage, RulePaymentPromiseBroken:
		return 2 * time.Hour
	case RuleSupplierConfirm, RuleMissingDocument, RulePaymentDue:
		return 12 * time.Hour
	default:
		return DefaultGrace
	}
}
