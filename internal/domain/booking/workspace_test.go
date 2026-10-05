package booking

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestProfileNormalize(t *testing.T) {
	p, err := Profile{PNR: " ab12-cd ", Summary: "  IST   -  JED ", CompanyName: " Acme  Travel "}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if p.PNR != "AB12-CD" || p.Summary != "IST - JED" || p.CompanyName != "Acme Travel" {
		t.Fatalf("normalized: %+v", p)
	}
	if p.ServiceType != ServicePackage || p.Channel != ChannelAgent {
		t.Fatalf("defaults: %+v", p)
	}
	bad := []Profile{
		{PNR: "AB 12"},
		{PNR: "-ABC"},
		{ServiceType: "cruise"},
		{SupplierSource: "galileo"},
		{Channel: "fax"},
	}
	for _, b := range bad {
		if _, err := b.Normalize(); !errors.Is(err, shared.ErrValidation) {
			t.Fatalf("%+v: want validation, got %v", b, err)
		}
	}
}

func TestApplyProfileReportsChange(t *testing.T) {
	b := &Booking{ServiceType: ServicePackage, Channel: ChannelAgent}
	now := time.Now()
	changed, err := b.ApplyProfile(Profile{}, now)
	if err != nil || changed {
		t.Fatalf("no-op: changed=%v err=%v", changed, err)
	}
	changed, err = b.ApplyProfile(Profile{PNR: "x7y8z9", ServiceType: ServiceFlight, SupplierSource: SourceDuffel, Channel: ChannelB2CWeb}, now)
	if err != nil || !changed || b.PNR != "X7Y8Z9" || b.SupplierSource != SourceDuffel || !b.UpdatedAt.Equal(now) {
		t.Fatalf("apply: changed=%v err=%v b=%+v", changed, err, b)
	}
}

func TestRefCode(t *testing.T) {
	if RefCode(123) != "BK-000123" || RefCode(0) != "" {
		t.Fatal(RefCode(123))
	}
	for in, want := range map[string]int64{"BK-000123": 123, "bk123": 123, "BK-9": 9} {
		if n, ok := ParseRefCode(in); !ok || n != want {
			t.Fatalf("%s: %d %v", in, n, ok)
		}
	}
	for _, in := range []string{"BK-", "123", "XK-12", "BK-0"} {
		if _, ok := ParseRefCode(in); ok {
			t.Fatalf("%s parsed", in)
		}
	}
}

func TestTicketStatus(t *testing.T) {
	cases := []struct {
		s        Status
		reissues int
		refunded int64
		want     string
	}{
		{StatusDraft, 0, 0, TicketPending},
		{StatusQuoted, 0, 0, TicketPending},
		{StatusOptionHold, 0, 0, TicketOption},
		{StatusConfirmed, 0, 0, TicketIssued},
		{StatusReady, 2, 0, TicketReissued},
		{StatusTravelled, 0, 0, TicketIssued},
		{StatusCancelled, 0, 0, TicketCancelled},
		{StatusCancelled, 1, 5000, TicketRefunded},
	}
	for _, c := range cases {
		if got := TicketStatus(c.s, c.reissues, c.refunded); got != c.want {
			t.Fatalf("%s/%d/%d: got %s want %s", c.s, c.reissues, c.refunded, got, c.want)
		}
	}
}

func TestPaymentStatus(t *testing.T) {
	cases := []struct {
		total, collected, balance int64
		overdue                   bool
		want                      string
	}{
		{0, 0, 0, false, PaymentNone},
		{1000, 0, 1000, false, PaymentAwaiting},
		{1000, 300, 700, false, PaymentDeposit},
		{1000, 300, 700, true, PaymentOverdue},
		{1000, 0, 1000, true, PaymentOverdue},
		{1000, 1000, 0, true, PaymentPaid},
	}
	for _, c := range cases {
		if got := PaymentStatus(c.total, c.collected, c.balance, c.overdue); got != c.want {
			t.Fatalf("%+v: got %s", c, got)
		}
	}
}

func TestExtendHold(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	hold := now.Add(3 * time.Hour)
	b := &Booking{Status: StatusOptionHold, HoldExpiresAt: &hold}

	if _, err := b.ExtendHold(now.Add(2*time.Hour), now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("earlier deadline: %v", err)
	}
	if _, err := b.ExtendHold(now.Add(-time.Hour), now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("past deadline: %v", err)
	}
	if _, err := b.ExtendHold(now.Add(MaxHoldDuration+time.Hour), now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("too far: %v", err)
	}
	prev, err := b.ExtendHold(now.Add(24*time.Hour), now)
	if err != nil || !prev.Equal(hold) || !b.HoldExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("extend: prev=%v err=%v hold=%v", prev, err, b.HoldExpiresAt)
	}
	confirmed := &Booking{Status: StatusConfirmed}
	if _, err := confirmed.ExtendHold(now.Add(time.Hour), now); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("not on option: %v", err)
	}
}

func TestQuoteCancellation(t *testing.T) {
	policy := DefaultCancellationPolicy()
	sold := &Booking{Status: StatusPartiallyPaid, TotalAmount: 100000, CollectedAmt: 60000, Currency: "SAR"}
	cases := []struct {
		days            int
		pct             int
		penalty, refund int64
	}{
		{60, 10, 10000, 50000},
		{30, 25, 25000, 35000},
		{20, 50, 50000, 10000},
		{8, 75, 75000, 0},
		{3, 100, 100000, 0},
		{-2, 100, 100000, 0},
	}
	for _, c := range cases {
		q := sold.QuoteCancellation(c.days, policy)
		if q.PenaltyPct != c.pct || q.PenaltyAmt != c.penalty || q.RefundableAmt != c.refund || q.Currency != "SAR" {
			t.Fatalf("%d days: %+v", c.days, q)
		}
	}
	hold := &Booking{Status: StatusOptionHold, TotalAmount: 100000, CollectedAmt: 20000}
	if q := hold.QuoteCancellation(3, policy); q.PenaltyPct != 0 || q.RefundableAmt != 20000 {
		t.Fatalf("option: %+v", q)
	}
}

func TestDaysUntil(t *testing.T) {
	today := time.Date(2026, 10, 5, 23, 30, 0, 0, time.UTC)
	if d := DaysUntil(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), today); d != 10 {
		t.Fatal(d)
	}
	if d := DaysUntil(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), today); d != -4 {
		t.Fatal(d)
	}
}

func TestNotes(t *testing.T) {
	author := uuid.New()
	if _, err := NewNote(uuid.New(), author, "   ", false, time.Now()); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("blank: %v", err)
	}
	n, err := NewNote(uuid.New(), author, "  call supplier  ", true, time.Now())
	if err != nil || n.Body != "call supplier" || !n.Pinned {
		t.Fatalf("note: %+v %v", n, err)
	}
	if !n.CanDelete(author) || n.CanDelete(uuid.New()) {
		t.Fatal("delete rule")
	}
}

func TestChangeRequests(t *testing.T) {
	now := time.Now()
	actor := uuid.New()
	b := &Booking{ID: uuid.New(), Status: StatusConfirmed}
	if _, err := NewChangeRequest(b, "upgrade", "x", actor, now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("kind: %v", err)
	}
	if _, err := NewChangeRequest(b, ChangeDate, " ", actor, now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("details: %v", err)
	}
	if _, err := NewChangeRequest(&Booking{Status: StatusCancelled}, ChangeDate, "x", actor, now); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("terminal: %v", err)
	}
	c, err := NewChangeRequest(b, ChangeName, "Fix spelling of surname", actor, now)
	if err != nil || c.Status != ChangeRequested {
		t.Fatalf("new: %+v %v", c, err)
	}
	if err := c.Resolve(ChangeRejected, "", actor, now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("reject without note: %v", err)
	}
	if err := c.Resolve("done", "", actor, now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("bad status: %v", err)
	}
	if err := c.Resolve(ChangeCompleted, "", actor, now); err != nil || c.Status != ChangeCompleted || c.ResolvedAt == nil {
		t.Fatalf("complete: %+v %v", c, err)
	}
	if err := c.Resolve(ChangeRejected, "late", actor, now); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("twice: %v", err)
	}
}

func TestSegmentsAndFields(t *testing.T) {
	for _, s := range []Segment{SegmentAll, SegmentOptionToday, SegmentPaymentDue, SegmentVisaPending, SegmentOverdue, SegmentIssued, SegmentCancelled} {
		if !s.Valid() {
			t.Fatal(s)
		}
	}
	if Segment("vip").Valid() || DateField("x").Valid() || ValidSort("price") || !ValidGender("") || ValidGender("x") {
		t.Fatal("invalid values accepted")
	}
	if !ValidNationalID("") || !ValidNationalID("12345678901") || ValidNationalID("12") || ValidNationalID("1234-5678") {
		t.Fatal("national id rule")
	}
}
