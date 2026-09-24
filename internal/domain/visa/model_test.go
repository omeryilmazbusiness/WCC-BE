package visa_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/visa"
)

func TestValidTransition(t *testing.T) {
	cases := []struct {
		from, to visa.Status
		ok       bool
	}{
		{visa.StatusDraft, visa.StatusSubmitted, true},
		{visa.StatusDraft, visa.StatusIssued, false},
		{visa.StatusSubmitted, visa.StatusProcessing, true},
		{visa.StatusProcessing, visa.StatusApproved, true},
		{visa.StatusProcessing, visa.StatusIssued, true},
		{visa.StatusApproved, visa.StatusIssued, true},
		{visa.StatusIssued, visa.StatusDraft, false},
		{visa.StatusRejected, visa.StatusDraft, true},
		{visa.StatusCancelled, visa.StatusDraft, false},
	}
	for _, c := range cases {
		if got := visa.ValidTransition(c.from, c.to); got != c.ok {
			t.Fatalf("%s→%s: got %v want %v", c.from, c.to, got, c.ok)
		}
	}
}

func TestTransitionToRecordsEvent(t *testing.T) {
	v := &visa.VisaCase{
		ID: uuid.New(), Status: visa.StatusDraft, CreatedBy: uuid.New(),
	}
	actor := uuid.New()
	ev, err := v.TransitionTo(visa.StatusSubmitted, actor, "filed")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != visa.StatusSubmitted || v.SubmittedAt == nil {
		t.Fatalf("%#v", v)
	}
	if ev.FromStatus != visa.StatusDraft || ev.ToStatus != visa.StatusSubmitted {
		t.Fatalf("%#v", ev)
	}
	if _, err := v.TransitionTo(visa.StatusIssued, actor, ""); err == nil {
		t.Fatal("skip processing must fail")
	}
}
