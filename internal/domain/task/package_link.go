package task

import (
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// PackageLink ties a task to a catalogue package and, optionally, one of its
// departures. Codes and names are read-side enrichment and never persisted.
type PackageLink struct {
	PackageID     *uuid.UUID
	DepartureID   *uuid.UUID
	PackageCode   string
	PackageName   string
	PackageNameAr string
	DepartureCode string
	DepartDate    *time.Time
}

// NormalizeLink drops nil UUIDs so "no link" has a single representation.
func NormalizeLink(packageID, departureID *uuid.UUID) PackageLink {
	return PackageLink{PackageID: nonNil(packageID), DepartureID: nonNil(departureID)}
}

// Empty reports whether the task carries no package.
func (l PackageLink) Empty() bool { return l.PackageID == nil && l.DepartureID == nil }

// SameAs compares the persisted ids only.
func (l PackageLink) SameAs(o PackageLink) bool {
	return eqID(l.PackageID, o.PackageID) && eqID(l.DepartureID, o.DepartureID)
}

// LinkPackage replaces the task's package link; closed tasks keep theirs.
func (t *Task) LinkPackage(l PackageLink, now time.Time) error {
	if t.Status == StatusDone || t.Status == StatusCancelled {
		return shared.NewInvalidState("cannot relink a closed task")
	}
	if l.DepartureID != nil && l.PackageID == nil {
		return shared.NewValidation("departure_id needs package_id")
	}
	t.Package = l
	t.UpdatedAt = now
	return nil
}

func nonNil(id *uuid.UUID) *uuid.UUID {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	v := *id
	return &v
}

func eqID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
