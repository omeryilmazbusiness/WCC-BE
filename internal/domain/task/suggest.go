package task

import (
	"strings"
	"time"
)

// Outcome of a conversation/call used to suggest the next CRM task (T-231).
type CallOutcome string

const (
	OutcomeFollowUp     CallOutcome = "follow_up"
	OutcomeSendQuote    CallOutcome = "send_quote"
	OutcomeDocsPending  CallOutcome = "docs_pending"
	OutcomePaymentDue   CallOutcome = "payment_due"
	OutcomeNoAnswer     CallOutcome = "no_answer"
	OutcomeClosedWon    CallOutcome = "closed_won"
	OutcomeClosedLost   CallOutcome = "closed_lost"
)

// NextTaskSuggestion is a proposed task — never auto-created without confirm (T-231/T-232).
type NextTaskSuggestion struct {
	Title    string
	Kind     string
	DueIn    time.Duration
	Priority int
	Reason   string
}

// SuggestNextTask is a pure deterministic mapper from conversation outcome → task proposal.
func SuggestNextTask(outcome CallOutcome, now time.Time) (NextTaskSuggestion, bool) {
	switch CallOutcome(strings.ToLower(string(outcome))) {
	case OutcomeFollowUp, OutcomeNoAnswer:
		return NextTaskSuggestion{
			Title: "Follow up with guest", Kind: "follow_up",
			DueIn: 24 * time.Hour, Priority: 2, Reason: "outcome=" + string(outcome),
		}, true
	case OutcomeSendQuote:
		return NextTaskSuggestion{
			Title: "Send package quote", Kind: "quote",
			DueIn: 4 * time.Hour, Priority: 1, Reason: "outcome=send_quote",
		}, true
	case OutcomeDocsPending:
		return NextTaskSuggestion{
			Title: "Collect missing documents", Kind: "document",
			DueIn: 48 * time.Hour, Priority: 2, Reason: "outcome=docs_pending",
		}, true
	case OutcomePaymentDue:
		return NextTaskSuggestion{
			Title: "Collect outstanding payment", Kind: "payment",
			DueIn: 12 * time.Hour, Priority: 1, Reason: "outcome=payment_due",
		}, true
	case OutcomeClosedWon, OutcomeClosedLost:
		return NextTaskSuggestion{}, false
	default:
		return NextTaskSuggestion{
			Title: "Follow up with guest", Kind: "follow_up",
			DueIn: 24 * time.Hour, Priority: 3, Reason: "outcome=unknown",
		}, true
	}
}

// SuggestedDueAt applies DueIn to now (pure helper).
func SuggestedDueAt(s NextTaskSuggestion, now time.Time) time.Time {
	return now.UTC().Add(s.DueIn)
}
