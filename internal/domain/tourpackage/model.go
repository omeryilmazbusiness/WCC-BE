package tourpackage

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Package is a reusable product template (Hajj/Umrah package).
type Package struct {
	ID          uuid.UUID
	BranchID    uuid.UUID
	Code        string
	NameEN      string
	NameAR      string
	Description string
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Departure is a dated instance with capacity.
type Departure struct {
	ID            uuid.UUID
	PackageID     uuid.UUID
	Code          string
	DepartDate    time.Time
	ReturnDate    time.Time
	CapacityTotal int
	CapacitySold  int // derived from confirmed bookings (recomputed)
	BasePrice     int64
	Currency      string
	IsActive      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (d *Departure) Remaining() int {
	r := d.CapacityTotal - d.CapacitySold
	if r < 0 {
		return 0
	}
	return r
}

// CanSell reports whether adding pax would exceed capacity.
func (d *Departure) CanSell(additionalPax int) bool {
	return d.CapacitySold+additionalPax <= d.CapacityTotal
}

// Clone creates a new departure template from this one (new dates/code required by caller).
func (d *Departure) Clone(newID uuid.UUID, code string, depart, ret time.Time) *Departure {
	now := time.Now().UTC()
	return &Departure{
		ID:            newID,
		PackageID:     d.PackageID,
		Code:          strings.TrimSpace(code),
		DepartDate:    depart,
		ReturnDate:    ret,
		CapacityTotal: d.CapacityTotal,
		CapacitySold:  0,
		BasePrice:     d.BasePrice,
		Currency:      d.Currency,
		IsActive:      true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func ValidateDepartureDates(depart, ret time.Time) error {
	if depart.IsZero() || ret.IsZero() {
		return shared.NewValidation("depart_date and return_date are required")
	}
	if !ret.After(depart) && !ret.Equal(depart) {
		return shared.NewValidation("return_date must be on or after depart_date")
	}
	return nil
}

// Repository is the persistence port (DIP).
type Repository interface {
	CreatePackage(ctx context.Context, p *Package) error
	UpdatePackage(ctx context.Context, p *Package) error
	FindPackage(ctx context.Context, id uuid.UUID) (*Package, error)
	ListPackages(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]Package, error)

	CreateDeparture(ctx context.Context, d *Departure) error
	UpdateDeparture(ctx context.Context, d *Departure) error
	FindDeparture(ctx context.Context, id uuid.UUID) (*Departure, error)
	UpdateDepartureCapacitySold(ctx context.Context, id uuid.UUID, sold int) error
	ListDepartures(ctx context.Context, packageID uuid.UUID) ([]Departure, error)
}
