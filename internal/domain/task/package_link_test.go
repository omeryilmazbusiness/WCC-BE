package task

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestNormalizeLinkDropsNilIDs(t *testing.T) {
	nilID := uuid.Nil
	if l := NormalizeLink(&nilID, &nilID); !l.Empty() {
		t.Fatalf("nil uuids must normalise to an empty link, got %+v", l)
	}
	p := uuid.New()
	l := NormalizeLink(&p, nil)
	if l.PackageID == nil || *l.PackageID != p || l.DepartureID != nil {
		t.Fatalf("unexpected link %+v", l)
	}
	p2 := p
	if !l.SameAs(NormalizeLink(&p2, nil)) {
		t.Fatal("equal ids must compare equal")
	}
	if l.SameAs(PackageLink{}) {
		t.Fatal("linked and empty must differ")
	}
}

func TestLinkPackageRules(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	p, d := uuid.New(), uuid.New()

	open := &Task{Status: StatusOpen}
	if err := open.LinkPackage(PackageLink{PackageID: &p, DepartureID: &d}, now); err != nil {
		t.Fatalf("open task should link: %v", err)
	}
	if !open.UpdatedAt.Equal(now) || *open.Package.DepartureID != d {
		t.Fatalf("link not applied: %+v", open)
	}
	if err := open.LinkPackage(PackageLink{}, now); err != nil || !open.Package.Empty() {
		t.Fatalf("clearing should work: %v", err)
	}

	if err := open.LinkPackage(PackageLink{DepartureID: &d}, now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("departure without package must be a validation error, got %v", err)
	}
	for _, s := range []Status{StatusDone, StatusCancelled} {
		closed := &Task{Status: s}
		if err := closed.LinkPackage(PackageLink{PackageID: &p}, now); !errors.Is(err, shared.ErrInvalidState) {
			t.Fatalf("%s task must not relink", s)
		}
	}
}
