package tourpackage

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	BasePrice     int64 // minor units
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

type Repository interface {
	CreatePackage(ctx context.Context, p *Package) error
	FindPackage(ctx context.Context, id uuid.UUID) (*Package, error)
	CreateDeparture(ctx context.Context, d *Departure) error
	FindDeparture(ctx context.Context, id uuid.UUID) (*Departure, error)
	UpdateDepartureCapacitySold(ctx context.Context, id uuid.UUID, sold int) error
	ListDepartures(ctx context.Context, packageID uuid.UUID) ([]Departure, error)
}
