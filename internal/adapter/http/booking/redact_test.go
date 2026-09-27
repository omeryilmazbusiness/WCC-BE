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
	}{
		{platformauth.RoleGM, true},
		{platformauth.RoleEmployee, false},
	}
	for _, tc := range cases {
		fa := fieldAccessForRole(tc.role)
		if fa.financials != tc.financials {
			t.Fatalf("%s: got %+v", tc.role, fa)
		}
	}
}

func TestMapBookingRedactsFinancials(t *testing.T) {
	b := &domain.Booking{TotalAmount: 1000, CostAmt: 600, DiscountAmt: 100}

	open := mapBooking(b, fieldAccess{financials: true})
	if open["cost_amt"] != int64(600) || open["margin"] != int64(400) {
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

func TestFieldAccessOverrideAndDiscount(t *testing.T) {
	for _, r := range platformauth.AllRoles() {
		fa := fieldAccessForRole(r)
		want := r == platformauth.RoleGM || r == platformauth.RoleManager
		if fa.override != want || fa.discount != want {
			t.Fatalf("%s: %+v", r, fa)
		}
	}
}

func TestMapBookingAllowedTransitionsHideOverrideEdges(t *testing.T) {
	b := &domain.Booking{Status: domain.StatusConfirmed}
	has := func(m map[string]any, s domain.Status) bool {
		for _, a := range m["allowed_transitions"].([]domain.AllowedTransition) {
			if a.Status == s {
				return true
			}
		}
		return false
	}
	if has(mapBooking(b, fieldAccess{}), domain.StatusReady) {
		t.Fatal("ready is an override edge and must be hidden without bookings.override")
	}
	m := mapBooking(b, fieldAccess{override: true})
	if !has(m, domain.StatusReady) || !has(m, domain.StatusCancelled) {
		t.Fatalf("override caller: %v", m["allowed_transitions"])
	}
	if m["hold_expires_at"] != nil {
		t.Fatal("hold_expires_at must be null outside option_hold")
	}
}

func TestStatusRequestInput(t *testing.T) {
	bad := "tomorrow"
	if _, err := (statusRequest{Status: "option_hold", HoldExpiresAt: &bad}).input(); err == nil {
		t.Fatal("non-RFC3339 hold_expires_at must be rejected")
	}
	ok := "2026-10-01T12:00:00+03:00"
	in, err := (statusRequest{Status: " option_hold ", HoldExpiresAt: &ok, Reason: "r"}).input()
	if err != nil || in.Status != domain.StatusOptionHold || in.HoldExpiresAt.Hour() != 9 {
		t.Fatalf("input: %+v %v", in, err)
	}
	if _, err := (statusRequest{}).input(); err == nil {
		t.Fatal("status is required")
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

func TestMapParticipantAlwaysMasksPassport(t *testing.T) {
	p := &domain.Participant{PassportNo: "A12345678"}
	m := mapParticipant(p)
	if got := m["passport_no"]; got != "••••5678" {
		t.Fatalf("masked passport = %v", got)
	}
	if got := m["passport_last4"]; got != "5678" {
		t.Fatalf("passport_last4 = %v", got)
	}
}
