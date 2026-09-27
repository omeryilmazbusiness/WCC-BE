package booking_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

type recTasks struct {
	calls []uuid.UUID
}

func (r *recTasks) EnsureHoldExpiredTask(_ context.Context, _, bookingID, _ uuid.UUID, _ time.Time) error {
	r.calls = append(r.calls, bookingID)
	return nil
}

func TestExpireHoldsReleasesSeatAndCreatesTask(t *testing.T) {
	f := newFixture(10)
	rec := &recAudit{}
	f.svc.SetAuditor(rec)
	tasks := &recTasks{}
	lc := appbooking.NewLifecycle(f.svc, tasks)

	held := f.draft(t, 2, 1000)
	kept := f.draft(t, 1, 500)
	soon := time.Now().UTC().Add(time.Hour)
	later := time.Now().UTC().Add(5 * 24 * time.Hour)
	for id, h := range map[uuid.UUID]*time.Time{held.ID: &soon, kept.ID: &later} {
		if _, err := f.svc.Transition(sysCtx(), id, appbooking.TransitionInput{Status: domain.StatusOptionHold, HoldExpiresAt: h}); err != nil {
			t.Fatal(err)
		}
	}
	if f.dep.CapacitySold != 3 {
		t.Fatalf("sold=%d", f.dep.CapacitySold)
	}

	res, err := lc.ExpireHolds(context.Background(), soon.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if res.Transitioned != 1 || f.status(held.ID) != domain.StatusDraft || f.status(kept.ID) != domain.StatusOptionHold {
		t.Fatalf("res=%+v held=%s kept=%s", res, f.status(held.ID), f.status(kept.ID))
	}
	if f.dep.CapacitySold != 1 || f.books.byID[held.ID].HoldExpiresAt != nil {
		t.Fatalf("sold=%d hold=%v", f.dep.CapacitySold, f.books.byID[held.ID].HoldExpiresAt)
	}
	if f.books.byID[held.ID].StatusReason != domain.ReasonHoldExpired || len(tasks.calls) != 1 || tasks.calls[0] != held.ID {
		t.Fatalf("reason=%q tasks=%v", f.books.byID[held.ID].StatusReason, tasks.calls)
	}
	last := rec.events[len(rec.events)-1]
	if last.Action != "booking.status_changed" || last.Extra["actor_kind"] != domain.ActorSystem || rec.actors[len(rec.actors)-1] != "system" {
		t.Fatalf("expiry audit: %+v actor=%v", last, rec.actors[len(rec.actors)-1])
	}
	if last.Extra["capacity_sold_before"] != 3 || last.Extra["capacity_sold_after"] != 1 {
		t.Fatalf("capacity audit: %+v", last.Extra)
	}

	again, err := lc.ExpireHolds(context.Background(), soon.Add(time.Minute))
	if err != nil || again.Transitioned != 0 || len(tasks.calls) != 1 {
		t.Fatalf("expiry must be idempotent: %+v %v", again, err)
	}
}

func TestMarkTravelledOnDepartureDate(t *testing.T) {
	f := newFixture(10)
	rec := &recAudit{}
	f.svc.SetAuditor(rec)
	lc := appbooking.NewLifecycle(f.svc, nil)
	b := f.draft(t, 1, 1000)
	if _, err := f.svc.Confirm(sysCtx(), b.ID); err != nil {
		t.Fatal(err)
	}
	f.books.setMoney(b.ID, 200)
	if _, err := f.svc.Recompute(sysCtx(), b.ID); err != nil {
		t.Fatal(err)
	}

	res, err := lc.MarkTravelled(context.Background(), time.Now().UTC())
	if err != nil || res.Transitioned != 0 || f.status(b.ID) != domain.StatusPartiallyPaid {
		t.Fatalf("before departure: %+v %v %s", res, err, f.status(b.ID))
	}
	res, err = lc.MarkTravelled(context.Background(), f.dep.DepartDate)
	if err != nil || res.Transitioned != 1 || f.status(b.ID) != domain.StatusTravelled {
		t.Fatalf("on departure: %+v %v %s", res, err, f.status(b.ID))
	}
	last := rec.events[len(rec.events)-1]
	if last.Extra["readiness_ok"] != false || last.Extra["balance_amt"] != int64(800) {
		t.Fatalf("travelled audit must record guard state: %+v", last.Extra)
	}
	if f.dep.CapacitySold != 1 {
		t.Fatalf("travelled keeps the seat: sold=%d", f.dep.CapacitySold)
	}
	if _, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusCompleted}); err != nil {
		t.Fatal(err)
	}
}

func TestPaymentEventAndJobsRecompute(t *testing.T) {
	f := newFixture(10)
	bus := newBus()
	lc := appbooking.NewLifecycle(f.svc, nil)
	lc.Register(bus)
	b := f.draft(t, 1, 1000)
	if _, err := f.svc.Confirm(sysCtx(), b.ID); err != nil {
		t.Fatal(err)
	}

	f.books.setMoney(b.ID, 250)
	bus.Publish(context.Background(), events.Event{Name: events.PaymentRecorded, Payload: &paymentdomain.Payment{BookingID: b.ID}})
	if f.status(b.ID) != domain.StatusPartiallyPaid {
		t.Fatalf("payment event: %s", f.status(b.ID))
	}

	f.books.setMoney(b.ID, 0)
	payload, _ := json.Marshal(domain.RecomputePayload{BookingID: b.ID})
	if err := lc.HandleRecompute(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if f.status(b.ID) != domain.StatusConfirmed {
		t.Fatalf("recompute job: %s", f.status(b.ID))
	}
	if err := lc.HandleRecompute(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("missing booking_id must fail")
	}

	f.books.setMoney(b.ID, 10)
	res, err := lc.ReconcileDerived(context.Background())
	if err != nil || res.Transitioned != 1 || f.status(b.ID) != domain.StatusPartiallyPaid {
		t.Fatalf("reconcile: %+v %v %s", res, err, f.status(b.ID))
	}
	res, err = lc.ReconcileDerived(context.Background())
	if err != nil || res.Transitioned != 0 {
		t.Fatalf("reconcile must be idempotent: %+v %v", res, err)
	}
}
