package payment

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCanTransition(t *testing.T) {
	statuses := []Status{StatusUnverified, StatusVerified, StatusPendingApproval, StatusApproved, StatusRejected}
	events := []EventType{EventCharge, EventReverse, EventAdjust, EventRefund}
	allowed := map[string]bool{
		"charge:unverified>verified":       true,
		"refund:pending_approval>approved": true,
		"refund:pending_approval>rejected": true,
	}
	for _, e := range events {
		for _, from := range statuses {
			for _, to := range statuses {
				key := fmt.Sprintf("%s:%s>%s", e, from, to)
				if got := CanTransition(e, from, to); got != allowed[key] {
					t.Errorf("%s: got %v, want %v", key, got, allowed[key])
				}
			}
		}
	}
}

func TestTransitionTo(t *testing.T) {
	p := Payment{EventType: EventRefund, Status: StatusPendingApproval}
	if err := p.TransitionTo(StatusApproved); err != nil || p.Status != StatusApproved {
		t.Fatalf("approve pending refund: %v %s", err, p.Status)
	}
	if err := p.TransitionTo(StatusRejected); err == nil {
		t.Fatal("approved refund must not be rejected afterwards")
	}
	c := Payment{EventType: EventCharge, Status: StatusVerified}
	if err := c.TransitionTo(StatusUnverified); err == nil || c.Status != StatusVerified {
		t.Fatal("verified charge must not be unverified")
	}
}

// TestTriggerMirrorsTransitions keeps the DB trigger and the Go whitelist in sync.
func TestTriggerMirrorsTransitions(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/00025_epic20_audit_ledger.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- +goose Down")
	start := strings.Index(up, "NOT IN (")
	if start < 0 {
		t.Fatal("transition whitelist not found in payments_ledger_guard")
	}
	end := strings.Index(up[start:], ") THEN")
	if end < 0 {
		t.Fatal("transition whitelist is not terminated")
	}
	block := up[start : start+end]
	for _, tr := range StatusTransitions {
		tuple := fmt.Sprintf("('%s', '%s', '%s')", tr.EventType, tr.From, tr.To)
		if !strings.Contains(block, tuple) {
			t.Errorf("trigger is missing %s", tuple)
		}
	}
	if n := strings.Count(block, "('"); n != len(StatusTransitions) {
		t.Errorf("trigger lists %d transitions, Go lists %d", n, len(StatusTransitions))
	}
}
