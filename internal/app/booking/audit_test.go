package booking_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	pkgdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

type recAudit struct {
	events []audit.RecordInput
	actors []audit.ActorType
	fail   error
}

func (r *recAudit) Record(ctx context.Context, in audit.RecordInput) error {
	if r.fail != nil {
		return r.fail
	}
	a, _ := audit.ActorFrom(ctx)
	r.events = append(r.events, in)
	r.actors = append(r.actors, a.Type)
	return nil
}

func (r *recAudit) all(action string) []audit.RecordInput {
	var out []audit.RecordInput
	for _, e := range r.events {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

func (r *recAudit) find(action string) *audit.RecordInput {
	for i := range r.events {
		if r.events[i].Action == action {
			return &r.events[i]
		}
	}
	return nil
}

func auditFixture(t *testing.T) (*appbooking.Service, *recAudit, *domain.Booking) {
	t.Helper()
	books := newMemBooking()
	depID := uuid.New()
	deps := &memDepRepo{deps: map[uuid.UUID]*pkgdomain.Departure{
		depID: {
			ID: depID, CapacityTotal: 40, Currency: "USD",
			DepartDate: time.Now().UTC().Add(30 * 24 * time.Hour),
			ReturnDate: time.Now().UTC().Add(40 * 24 * time.Hour),
		},
	}}
	svc := newSvc(books, deps)
	rec := &recAudit{}
	svc.SetAuditor(rec)
	b, err := svc.CreateDraft(sysCtx(), appbooking.CreateInput{
		BranchID: uuid.New(), CustomerID: uuid.New(), DepartureID: depID, PaxCount: 2, TotalAmount: 1000, OwnerID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, rec, b
}

func TestBookingUpdateAuditsBeforeAfterAndDiscount(t *testing.T) {
	svc, rec, b := auditFixture(t)
	discount := int64(150)
	if _, err := svc.Update(sysCtx(), b.ID, appbooking.UpdateInput{PaxCount: 2, TotalAmount: 1200, Currency: "USD", DiscountAmt: &discount, CanDiscount: true}); err != nil {
		t.Fatal(err)
	}
	upd := rec.find("booking.updated")
	if upd == nil || upd.BranchID == nil || *upd.BranchID != b.BranchID || upd.ActorID != uuid.Nil {
		t.Fatalf("booking.updated: %+v", upd)
	}
	before, after := upd.Before.(map[string]any), upd.After.(map[string]any)
	if before["total_amount"] != int64(1000) || after["total_amount"] != int64(1200) {
		t.Fatalf("before/after: %v -> %v", before, after)
	}
	disc := rec.find("booking.discount_changed")
	if disc == nil || disc.Before.(map[string]any)["discount_amt"] != int64(0) || disc.After.(map[string]any)["discount_amt"] != int64(150) {
		t.Fatalf("discount audit: %+v", disc)
	}
}

func TestParticipantAuditNeverContainsPassport(t *testing.T) {
	svc, rec, b := auditFixture(t)
	p, err := svc.AddParticipant(sysCtx(), b.ID, appbooking.AddParticipantInput{FullName: "A B", PassportNo: "X1234567"})
	if err != nil {
		t.Fatal(err)
	}
	ev := rec.find("booking.participant_added")
	if ev == nil || *ev.EntityID != p.ID {
		t.Fatalf("participant audit: %+v", ev)
	}
	after := ev.After.(map[string]any)
	if after["passport_on_file"] != true {
		t.Fatalf("after: %v", after)
	}
	for _, v := range after {
		if s, ok := v.(string); ok && s == "X1234567" {
			t.Fatal("passport number leaked into audit")
		}
	}
	masked := shared.MaskedPassport("X1234567")
	for _, in := range []*string{nil, &masked} {
		upd, err := svc.UpdateParticipant(sysCtx(), b.ID, p.ID, appbooking.UpdateParticipantInput{FullName: "A B", PassportNo: in})
		if err != nil {
			t.Fatal(err)
		}
		if upd.PassportNo != "X1234567" {
			t.Fatalf("omitted or masked passport must keep the stored value, got %q", upd.PassportNo)
		}
	}
	if err := svc.DeleteParticipant(sysCtx(), b.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	if ev := rec.find("booking.participant_removed"); ev == nil || ev.Before == nil {
		t.Fatalf("remove audit: %+v", ev)
	}
}

func TestBookingChangeFailsClosedWhenAuditFails(t *testing.T) {
	svc, rec, b := auditFixture(t)
	rec.fail = errors.New("audit down")
	cancel := appbooking.TransitionInput{Status: domain.StatusCancelled, Reason: "customer withdrew"}
	if _, err := svc.Transition(sysCtx(), b.ID, cancel); err == nil {
		t.Fatal("cancel must fail when the audit insert fails")
	}
	if _, err := svc.OverrideReadiness(sysCtx(), b.ID, uuid.New(), "documents arrive at the airport"); err == nil {
		t.Fatal("override must fail when the audit insert fails")
	}
}

func TestStatusChangeAuditsFromAndTo(t *testing.T) {
	svc, rec, b := auditFixture(t)
	if _, err := svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusCancelled, Reason: "customer withdrew"}); err != nil {
		t.Fatal(err)
	}
	ev := rec.find("booking.status_changed")
	if ev == nil || ev.Before.(map[string]any)["status"] != domain.StatusDraft || ev.After.(map[string]any)["status"] != domain.StatusCancelled {
		t.Fatalf("status audit: %+v", ev)
	}
	if ev.Extra["actor_kind"] != domain.ActorUser || ev.After.(map[string]any)["status_reason"] != "customer withdrew" {
		t.Fatalf("status audit extra: %+v", ev)
	}
}

func TestOverrideReadinessNeedsReasonAndRederives(t *testing.T) {
	svc, rec, b := auditFixture(t)
	if _, err := svc.OverrideReadiness(sysCtx(), b.ID, uuid.New(), "too short"); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("short reason: %v", err)
	}
	if _, err := svc.OverrideReadiness(sysCtx(), b.ID, uuid.New(), "documents arrive at the airport"); err != nil {
		t.Fatal(err)
	}
	if rec.find("booking.readiness_overridden") == nil {
		t.Fatal("readiness override must be audited")
	}
}
