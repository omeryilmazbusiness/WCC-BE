package booking

import (
	"testing"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

func TestFieldAccessForRole(t *testing.T) {
	cases := []struct {
		role       platformauth.Role
		financials bool
		pii        bool
	}{
		{platformauth.RoleGM, true, true},
		{platformauth.RoleEmployee, false, false},
	}
	for _, tc := range cases {
		fa := fieldAccessForRole(tc.role)
		if fa.financials != tc.financials || fa.pii != tc.pii {
			t.Fatalf("%s: got %+v", tc.role, fa)
		}
	}
}

func TestMapBookingRedactsFinancials(t *testing.T) {
	b := &domain.Booking{TotalAmount: 1000, CostAmt: 600, DiscountAmt: 100}

	open := mapBooking(b, fieldAccess{financials: true})
	if open["cost_amt"] != int64(600) || open["margin"] != int64(300) {
		t.Fatalf("unredacted: %v %v", open["cost_amt"], open["margin"])
	}

	red := mapBooking(b, fieldAccess{})
	cost, hasCost := red["cost_amt"]
	margin, hasMargin := red["margin"]
	if !hasCost || !hasMargin {
		t.Fatal("redacted response must keep cost_amt and margin keys")
	}
	if cost != int64(0) || margin != int64(0) {
		t.Fatalf("redacted: %v %v", cost, margin)
	}
	if red["total_amount"] != int64(1000) {
		t.Fatal("total_amount must not be redacted")
	}
}

func TestMapLineRedactsCost(t *testing.T) {
	l := domain.LineItem{Quantity: 2, UnitPrice: 50, UnitCost: 30}
	red := mapLine(l, fieldAccess{})
	if red["unit_cost"] != int64(0) || red["line_cost"] != int64(0) {
		t.Fatalf("redacted: %v %v", red["unit_cost"], red["line_cost"])
	}
	if red["line_total"] != int64(100) {
		t.Fatal("line_total must not be redacted")
	}
	open := mapLine(l, fieldAccess{financials: true})
	if open["unit_cost"] != int64(30) || open["line_cost"] != int64(60) {
		t.Fatalf("unredacted: %v %v", open["unit_cost"], open["line_cost"])
	}
}

func TestMapParticipantMasksPassport(t *testing.T) {
	p := &domain.Participant{PassportNo: "A12345678"}
	if got := mapParticipant(p, fieldAccess{})["passport_no"]; got != "A1*****78" {
		t.Fatalf("masked passport = %v", got)
	}
	if got := mapParticipant(p, fieldAccess{pii: true})["passport_no"]; got != "A12345678" {
		t.Fatalf("full passport = %v", got)
	}
}
