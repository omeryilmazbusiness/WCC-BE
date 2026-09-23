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

// PricingTier is room/occupancy/age pricing (T-049).
type PricingTier struct {
	ID         uuid.UUID
	PackageID  uuid.UUID // set for template tiers
	DepartureID *uuid.UUID
	Code       string
	Label      string
	Kind       string // room | occupancy | age
	Amount     int64
	Currency   string
	SortOrder  int
	IsActive   bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const (
	TierRoom      = "room"
	TierOccupancy = "occupancy"
	TierAge       = "age"
)

func ValidTierKind(k string) bool {
	return k == TierRoom || k == TierOccupancy || k == TierAge
}

// Departure is a dated instance with capacity.
type Departure struct {
	ID                uuid.UUID
	PackageID         uuid.UUID
	Code              string
	DepartDate        time.Time
	ReturnDate        time.Time
	CapacityTotal     int
	CapacitySold      int
	BasePrice         int64
	Currency          string
	IsActive          bool
	SalesClosed       bool
	SoftThresholdPct  int
	AllowOversell     bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (d *Departure) Remaining() int {
	r := d.CapacityTotal - d.CapacitySold
	if r < 0 {
		return 0
	}
	return r
}

// FillPct returns sold/total as 0–100 (100 if total is 0 and sold > 0).
func (d *Departure) FillPct() int {
	if d.CapacityTotal <= 0 {
		if d.CapacitySold > 0 {
			return 100
		}
		return 0
	}
	return (d.CapacitySold * 100) / d.CapacityTotal
}

// CapacityAlert returns ok | low | full | oversold (T-053).
func (d *Departure) CapacityAlert() string {
	if d.CapacitySold > d.CapacityTotal {
		return "oversold"
	}
	if d.Remaining() == 0 || d.SalesClosed {
		return "full"
	}
	th := d.SoftThresholdPct
	if th <= 0 {
		th = 80
	}
	if d.FillPct() >= th {
		return "low"
	}
	return "ok"
}

// CanSell reports whether adding pax would exceed capacity (unless oversell allowed).
func (d *Departure) CanSell(additionalPax int) bool {
	if d.SalesClosed || !d.IsActive {
		return false
	}
	if d.AllowOversell {
		return additionalPax > 0
	}
	return d.CapacitySold+additionalPax <= d.CapacityTotal
}

// PricingLocked is true once sold seats exist (T-052 immutability on departure pricing).
func (d *Departure) PricingLocked() bool {
	return d.CapacitySold > 0
}

// Clone creates a new departure template from this one (new dates/code required by caller).
func (d *Departure) Clone(newID uuid.UUID, code string, depart, ret time.Time) *Departure {
	now := time.Now().UTC()
	th := d.SoftThresholdPct
	if th <= 0 {
		th = 80
	}
	return &Departure{
		ID:               newID,
		PackageID:        d.PackageID,
		Code:             strings.TrimSpace(code),
		DepartDate:       depart,
		ReturnDate:       ret,
		CapacityTotal:    d.CapacityTotal,
		CapacitySold:     0,
		BasePrice:        d.BasePrice,
		Currency:         d.Currency,
		IsActive:         true,
		SalesClosed:      false,
		SoftThresholdPct: th,
		AllowOversell:    d.AllowOversell,
		CreatedAt:        now,
		UpdatedAt:        now,
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

// Readiness summarizes departure ops status (T-057).
type Readiness struct {
	DepartureID       uuid.UUID `json:"departure_id"`
	BookingsTotal     int       `json:"bookings_total"`
	BookingsDraft     int       `json:"bookings_draft"`
	BookingsConfirmed int       `json:"bookings_confirmed"`
	BookingsCancelled int       `json:"bookings_cancelled"`
	PaxConfirmed      int       `json:"pax_confirmed"`
	CapacityTotal     int       `json:"capacity_total"`
	CapacitySold      int       `json:"capacity_sold"`
	Remaining         int       `json:"remaining"`
	Alert             string    `json:"alert"`
	SalesClosed       bool      `json:"sales_closed"`
	PricingLocked     bool      `json:"pricing_locked"`
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

	ReplacePackageTiers(ctx context.Context, packageID uuid.UUID, tiers []PricingTier) error
	ListPackageTiers(ctx context.Context, packageID uuid.UUID) ([]PricingTier, error)
	SnapshotTiersToDeparture(ctx context.Context, packageID, departureID uuid.UUID) error
	ListDepartureTiers(ctx context.Context, departureID uuid.UUID) ([]PricingTier, error)
	ReplaceDepartureTiers(ctx context.Context, departureID uuid.UUID, tiers []PricingTier) error
}
