package tourpackage_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

func TestDepartureCapacity(t *testing.T) {
	d := &domain.Departure{CapacityTotal: 10, CapacitySold: 8}
	if d.Remaining() != 2 {
		t.Fatalf("remaining=%d", d.Remaining())
	}
	if !d.CanSell(2) {
		t.Fatal("should allow selling 2")
	}
	if d.CanSell(3) {
		t.Fatal("should not allow selling 3")
	}
}

func TestCloneResetsSold(t *testing.T) {
	src := &domain.Departure{
		PackageID:     uuid.New(),
		CapacityTotal: 40,
		CapacitySold:  12,
		BasePrice:     5000,
		Currency:      "USD",
	}
	depart := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	ret := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	cloned := src.Clone(uuid.New(), "UMR-NOV", depart, ret)
	if cloned.CapacitySold != 0 {
		t.Fatal("clone must reset capacity_sold")
	}
	if cloned.CapacityTotal != 40 || cloned.BasePrice != 5000 {
		t.Fatal("clone should preserve capacity/price")
	}
}

func TestValidateDates(t *testing.T) {
	d := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	r := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := domain.ValidateDepartureDates(d, r); err == nil {
		t.Fatal("expected validation error")
	}
}
