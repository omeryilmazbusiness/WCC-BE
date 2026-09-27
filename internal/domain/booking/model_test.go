package booking_test

import (
	"testing"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
)

func TestRecomputeBalance(t *testing.T) {
	b := &booking.Booking{Status: booking.StatusDraft, TotalAmount: 1000, CollectedAmt: 250}
	b.RecomputeBalance()
	if b.BalanceAmt != 750 {
		t.Fatalf("balance=%d want 750", b.BalanceAmt)
	}
	b.CollectedAmt = 1200
	b.RecomputeBalance()
	if b.BalanceAmt != 0 {
		t.Fatalf("overpaid balance=%d want 0", b.BalanceAmt)
	}
}

func TestApplyUpdateOnlyEditable(t *testing.T) {
	now := time.Now().UTC()
	disc := int64(0)
	notes := "n"
	for _, s := range booking.AllStatuses() {
		b := &booking.Booking{Status: s, TotalAmount: 100}
		err := b.ApplyUpdate(booking.UpdateFields{PaxCount: 3, TotalAmount: 300, Currency: "USD", DiscountAmt: &disc, Notes: &notes}, nil, now)
		if s.Editable() != (err == nil) {
			t.Fatalf("%s: editable=%v err=%v", s, s.Editable(), err)
		}
		if err == nil && (b.PaxCount != 3 || b.BalanceAmt != 300 || b.Notes != "n") {
			t.Fatalf("unexpected update %#v", b)
		}
	}
}

func TestApplyUpdateWithLinesDerivesTotals(t *testing.T) {
	b := &booking.Booking{Status: booking.StatusQuoted, CollectedAmt: 100}
	lines := []booking.LineItem{
		{Kind: booking.KindItem, Quantity: 2, UnitPrice: 500, UnitCost: 300},
		{Kind: booking.KindTax, Quantity: 1, UnitPrice: 150},
	}
	disc := int64(200)
	if err := b.ApplyUpdate(booking.UpdateFields{PaxCount: 2, TotalAmount: 99999, DiscountAmt: &disc}, lines, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b.TotalAmount != 950 || b.DiscountAmt != 200 || b.TaxAmt != 150 || b.BalanceAmt != 850 {
		t.Fatalf("total=%d discount=%d tax=%d balance=%d", b.TotalAmount, b.DiscountAmt, b.TaxAmt, b.BalanceAmt)
	}
	over := int64(1001)
	if err := b.ApplyUpdate(booking.UpdateFields{PaxCount: 2, DiscountAmt: &over}, lines, time.Now()); err == nil {
		t.Fatal("discount above subtotal must fail")
	}
}

func TestComputeTotals(t *testing.T) {
	lines := []booking.LineItem{
		{Kind: booking.KindItem, Quantity: 2, UnitPrice: 1500, UnitCost: 1100},
		{Kind: booking.KindItem, Quantity: 1, UnitPrice: 200, UnitCost: 80},
		{Kind: booking.KindTax, Quantity: 1, UnitPrice: 480},
		{Kind: booking.KindFee, Quantity: 3, UnitPrice: 25, UnitCost: 10},
	}
	cases := []struct {
		name     string
		discount int64
		want     booking.Totals
		wantErr  bool
	}{
		{"no discount", 0, booking.Totals{Subtotal: 3200, Tax: 480, Fees: 75, Total: 3755, Cost: 2310}, false},
		{"discount", 700, booking.Totals{Subtotal: 3200, Discount: 700, Tax: 480, Fees: 75, Total: 3055, Cost: 2310}, false},
		{"discount equals subtotal", 3200, booking.Totals{Subtotal: 3200, Discount: 3200, Tax: 480, Fees: 75, Total: 555, Cost: 2310}, false},
		{"discount above subtotal", 3201, booking.Totals{}, true},
		{"negative discount", -1, booking.Totals{}, true},
	}
	for _, c := range cases {
		got, err := booking.ComputeTotals(lines, c.discount)
		if (err != nil) != c.wantErr {
			t.Fatalf("%s: err=%v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
	if _, err := booking.ComputeTotals([]booking.LineItem{{Kind: "hotel", Quantity: 1}}, 0); err == nil {
		t.Fatal("unknown kind must fail")
	}
}

func TestRecalculateFromLinesAndMargin(t *testing.T) {
	b := &booking.Booking{Status: booking.StatusDraft, DiscountAmt: 100}
	err := b.RecalculateFromLines([]booking.LineItem{
		{Kind: booking.KindItem, Quantity: 2, UnitPrice: 500, UnitCost: 300},
		{Kind: booking.KindItem, Quantity: 1, UnitPrice: 200, UnitCost: 50},
		{Kind: booking.KindTax, Quantity: 1, UnitPrice: 60},
		{Kind: booking.KindFee, Quantity: 1, UnitPrice: 40},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if b.TotalAmount != 1200 || b.CostAmt != 650 || b.Subtotal() != 1200 {
		t.Fatalf("total=%d cost=%d subtotal=%d", b.TotalAmount, b.CostAmt, b.Subtotal())
	}
	// net revenue 1100 (1200 items - 100 discount) minus cost 650
	if b.Margin() != 450 {
		t.Fatalf("margin=%d want 450", b.Margin())
	}
}
