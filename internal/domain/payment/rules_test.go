package payment

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func appCode(err error) string {
	var app *shared.AppError
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

func TestInitialChargeStatus(t *testing.T) {
	if st, err := InitialChargeStatus(false, false); err != nil || st != StatusUnverified {
		t.Fatalf("plain record: %s %v", st, err)
	}
	if st, err := InitialChargeStatus(false, true); err != nil || st != StatusUnverified {
		t.Fatalf("approver without auto_verify: %s %v", st, err)
	}
	if st, err := InitialChargeStatus(true, true); err != nil || st != StatusVerified {
		t.Fatalf("approver auto_verify: %s %v", st, err)
	}
	_, err := InitialChargeStatus(true, false)
	if appCode(err) != "forbidden_auto_verify" || !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("auto_verify without payments.approve must be 403 forbidden_auto_verify, got %v", err)
	}
}

func TestCheckRefundApprover(t *testing.T) {
	requester := uuid.New()
	refund := &Payment{EventType: EventRefund, Status: StatusPendingApproval, RecordedBy: requester}
	err := CheckRefundApprover(refund, requester)
	if appCode(err) != "sod_violation" || !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("requester approving own refund must be sod_violation, got %v", err)
	}
	if err := CheckRefundApprover(refund, uuid.New()); err != nil {
		t.Fatalf("another approver is allowed: %v", err)
	}
}

func TestResolveReceivedAt(t *testing.T) {
	today := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	got, err := ResolveReceivedAt(nil, today)
	if err != nil || !got.Equal(time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("default today: %s %v", got, err)
	}
	past := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if got, err := ResolveReceivedAt(&past, today); err != nil || !got.Equal(past) {
		t.Fatalf("past date: %s %v", got, err)
	}
	same := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	if _, err := ResolveReceivedAt(&same, today); err != nil {
		t.Fatalf("today is allowed: %v", err)
	}
	future := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	if _, err := ResolveReceivedAt(&future, today); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("future date must be rejected, got %v", err)
	}
}

func TestPromiseOutcome(t *testing.T) {
	promisedOn := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	p := Promise{Amount: 50000, PromisedOn: promisedOn, Status: PromiseOpen}
	cases := []struct {
		name      string
		collected int64
		today     time.Time
		want      PromiseStatus
	}{
		{"nothing yet, before date", 0, promisedOn.AddDate(0, 0, -2), PromiseOpen},
		{"partial on the date", 20000, promisedOn.Add(20 * time.Hour), PromiseOpen},
		{"covered before date", 50000, promisedOn.AddDate(0, 0, -1), PromiseKept},
		{"over-covered after date", 60000, promisedOn.AddDate(0, 0, 3), PromiseKept},
		{"partial after date", 49999, promisedOn.AddDate(0, 0, 1), PromiseBroken},
	}
	for _, c := range cases {
		if got := p.Outcome(c.collected, c.today); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
	cancelled := p
	cancelled.Status = PromiseCancelled
	if got := cancelled.Outcome(90000, promisedOn); got != PromiseCancelled {
		t.Errorf("resolved promises keep their status, got %s", got)
	}
}

func TestPromiseResolveAndValidate(t *testing.T) {
	today := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	p := Promise{Amount: 100, PromisedOn: today.AddDate(0, 0, -1), Status: PromiseOpen}
	if err := p.ValidateNew(today); err == nil {
		t.Fatal("past promised_on must be rejected")
	}
	p.PromisedOn = today
	if err := p.ValidateNew(today); err != nil {
		t.Fatalf("today is a valid promised_on: %v", err)
	}
	if err := (&Promise{Amount: 0, PromisedOn: today}).ValidateNew(today); err == nil {
		t.Fatal("zero amount must be rejected")
	}
	if err := p.Resolve(PromiseCancelled, today); err != nil || p.Status != PromiseCancelled || p.ResolvedAt == nil {
		t.Fatalf("cancel open promise: %v %+v", err, p)
	}
	if err := p.Resolve(PromiseKept, today); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("resolved promise cannot change again, got %v", err)
	}
}

func TestSummarizePromises(t *testing.T) {
	d1 := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	s := SummarizePromises([]Promise{
		{Amount: 100, PromisedOn: d1, Status: PromiseOpen},
		{Amount: 250, PromisedOn: d2, Status: PromiseOpen},
		{Amount: 999, PromisedOn: d2.AddDate(0, 0, -5), Status: PromiseKept},
	})
	if s.OpenCount != 2 || s.OpenAmount != 350 || s.NextPromisedOn == nil || !s.NextPromisedOn.Equal(d2) {
		t.Fatalf("summary: %+v", s)
	}
	if empty := SummarizePromises(nil); empty.OpenCount != 0 || empty.NextPromisedOn != nil {
		t.Fatalf("empty summary: %+v", empty)
	}
}

func TestComponentsSubtotal(t *testing.T) {
	if got := (Components{Items: 1000, Tax: 150, Fees: 50, HasLines: true}).Subtotal(1100, 100); got != 1000 {
		t.Errorf("line-item subtotal: %d", got)
	}
	if got := (Components{}).Subtotal(900, 100); got != 1000 {
		t.Errorf("legacy booking subtotal = total + discount: %d", got)
	}
}
