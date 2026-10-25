package company

import (
	"testing"
	"time"
)

func TestTermIsFixedOneYear(t *testing.T) {
	start := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	c := Company{Plan: PlanPremium, PlanStartedAt: start}
	mid := c.TermAt(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if mid.Years != 1 || mid.EndsAt.Year() != 2027 || mid.Status != TermActive {
		t.Fatalf("%+v", mid)
	}
	if mid.DaysLeft <= 0 {
		t.Fatal("mid-year must have days left")
	}
	late := c.TermAt(time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC))
	if late.Status != TermExpiring {
		t.Fatalf("want expiring, got %s", late.Status)
	}
	done := c.TermAt(time.Date(2027, 3, 10, 12, 0, 0, 0, time.UTC))
	if done.Status != TermExpired || done.DaysLeft != 0 {
		t.Fatalf("year is up: %+v", done)
	}
}

func TestTermFallsBackToCreatedAt(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := Company{Plan: PlanStandard, CreatedAt: created}
	got := c.TermAt(created)
	if !got.StartedAt.Equal(created) || got.EndsAt.Year() != 2027 {
		t.Fatalf("%+v", got)
	}
}
