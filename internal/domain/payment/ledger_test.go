package payment

import (
	"fmt"
	"os"
	"sort"
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

// latestGuard returns the Up-section body of the newest migration that
// (re)defines payments_ledger_guard.
func latestGuard(t *testing.T) string {
	t.Helper()
	const dir = "../../../migrations/"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	guard := ""
	for _, name := range names {
		raw, err := os.ReadFile(dir + name)
		if err != nil {
			t.Fatal(err)
		}
		up, _, _ := strings.Cut(string(raw), "-- +goose Down")
		if i := strings.Index(up, "FUNCTION payments_ledger_guard()"); i >= 0 {
			body := up[i:]
			if end := strings.Index(body, "$$;"); end >= 0 {
				body = body[:end]
			}
			guard = body
		}
	}
	if guard == "" {
		t.Fatal("payments_ledger_guard not found in migrations")
	}
	return guard
}

func TestTriggerMirrorsImmutableColumns(t *testing.T) {
	guard := latestGuard(t)
	start := strings.Index(guard, "IF NEW.id ")
	end := strings.Index(guard[max(start, 0):], " THEN")
	if start < 0 || end < 0 {
		t.Fatal("immutable column block not found in payments_ledger_guard")
	}
	block := guard[start : start+end]
	for _, col := range ImmutableColumns {
		if !strings.Contains(block, "NEW."+col+" ") || !strings.Contains(block, "OLD."+col) {
			t.Errorf("trigger does not guard %s", col)
		}
	}
	if n := strings.Count(block, "IS DISTINCT FROM"); n != len(ImmutableColumns) {
		t.Errorf("trigger guards %d immutable columns, Go lists %d", n, len(ImmutableColumns))
	}
}

// TestTriggerMirrorsTransitions keeps the DB trigger and the Go whitelist in sync.
func TestTriggerMirrorsTransitions(t *testing.T) {
	up := latestGuard(t)
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
