package notification

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAcknowledgeResolve(t *testing.T) {
	n := &Notification{Status: StatusOpen, Title: "x"}
	actor := uuid.New()
	if err := n.Acknowledge(actor); err != nil {
		t.Fatal(err)
	}
	if n.Status != StatusAcknowledged || n.AcknowledgedBy == nil {
		t.Fatalf("ack %#v", n)
	}
	if err := n.Resolve(actor); err != nil {
		t.Fatal(err)
	}
	if n.Status != StatusResolved {
		t.Fatal(n.Status)
	}
	if err := n.Acknowledge(actor); err == nil {
		t.Fatal("expected invalid state after resolve")
	}
}

func TestBumpOccurrenceRaisesSeverity(t *testing.T) {
	n := &Notification{Severity: SeverityInfo, OccurrenceCount: 1, Title: "a"}
	n.BumpOccurrence("b", "body", SeverityCritical)
	if n.OccurrenceCount != 2 || n.Severity != SeverityCritical || n.Title != "b" {
		t.Fatalf("%#v", n)
	}
}

func TestBuildGroupKeyGroupable(t *testing.T) {
	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	k1 := BuildGroupKey(KindMessageSLA, branch, "conversation", nil)
	k2 := BuildGroupKey(KindMessageSLA, branch, "conversation", nil)
	if k1 != k2 || k1 != KindMessageSLA+":branch:"+branch.String() {
		t.Fatalf("got %q %q", k1, k2)
	}
}

func TestBuildGroupKeyNonGroupable(t *testing.T) {
	branch := uuid.New()
	eid := uuid.New()
	k := BuildGroupKey(KindBookingConfirmed, branch, "booking", &eid)
	want := KindBookingConfirmed + ":booking:" + eid.String()
	if k != want {
		t.Fatalf("got %q want %q", k, want)
	}
}

func TestShouldEscalate(t *testing.T) {
	now := time.Now().UTC()
	if ShouldEscalate(KindBookingConfirmed, now.Add(-time.Hour), now) {
		t.Fatal("info booking should not escalate")
	}
	if !ShouldEscalate(KindMessageSLA, now.Add(-20*time.Minute), now) {
		t.Fatal("SLA past 15m should escalate")
	}
	if ShouldEscalate(KindMessageSLA, now.Add(-5*time.Minute), now) {
		t.Fatal("SLA within grace should not escalate")
	}
}

func TestGroupLabel(t *testing.T) {
	got := GroupLabel(KindMessageSLA, 12, "x")
	if got != "12 conversations overdue" {
		t.Fatal(got)
	}
	if DisplayTitle(Notification{Title: "one", OccurrenceCount: 1}) != "one" {
		t.Fatal("single")
	}
}

func TestMatchRuleMatrixComplete(t *testing.T) {
	kinds := []string{
		KindMessageSLA, KindLeadNoFollowUp, KindTaskOverdue, KindTaskEscalated,
		KindPaymentOverdue, KindDocumentMissing, KindDocumentExpiring,
		KindTargetBehind, KindIntegrationDown, KindBookingConfirmed, KindSupplierUnconfirmed,
	}
	for _, k := range kinds {
		if MatchRule(k) == nil {
			t.Fatalf("missing rule %s", k)
		}
	}
	if MatchRule("unknown.kind") != nil {
		t.Fatal("expected nil")
	}
}
