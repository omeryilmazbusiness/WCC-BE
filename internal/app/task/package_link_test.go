package task_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// fakeCatalogue knows packages per branch and departures per package.
type fakeCatalogue struct {
	branchOf  map[uuid.UUID]uuid.UUID // package -> branch
	packageOf map[uuid.UUID]uuid.UUID // departure -> package
	calls     int
}

func (c *fakeCatalogue) ResolvePackageLink(_ context.Context, branchID uuid.UUID, l domain.PackageLink) (domain.PackageLink, error) {
	c.calls++
	if l.DepartureID != nil {
		pkg, ok := c.packageOf[*l.DepartureID]
		if !ok {
			return l, shared.NewValidation("departure not found")
		}
		if l.PackageID == nil {
			l.PackageID = &pkg
		} else if *l.PackageID != pkg {
			return l, shared.NewValidation("departure of another package")
		}
	}
	if c.branchOf[*l.PackageID] != branchID {
		return l, shared.NewValidation("not a package of this branch")
	}
	l.PackageCode = "PKG"
	return l, nil
}

func linkFixture(t *testing.T) (context.Context, *apptask.Service, *fakeCatalogue, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx, _, branch := createCtx(access.LevelBranch)
	pkg, dep := uuid.New(), uuid.New()
	cat := &fakeCatalogue{
		branchOf:  map[uuid.UUID]uuid.UUID{pkg: branch, uuid.New(): uuid.New()},
		packageOf: map[uuid.UUID]uuid.UUID{dep: pkg},
	}
	svc := apptask.NewService(newTaskMem(), tx.Nop{}, events.NewBus(nil))
	svc.SetPackageLinker(cat)
	return ctx, svc, cat, pkg, dep
}

func TestCreateTaskWithPackageAndDeparture(t *testing.T) {
	ctx, svc, _, pkg, dep := linkFixture(t)
	lead := uuid.New()
	got, err := svc.Create(ctx, apptask.CreateInput{
		Title: "Send Ramadan quote", Kind: domain.KindFollowUp,
		RelatedType: "lead", RelatedID: lead, DepartureID: &dep,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Package.PackageID == nil || *got.Package.PackageID != pkg || *got.Package.DepartureID != dep {
		t.Fatalf("departure must pull in its package: %+v", got.Package)
	}
	if got.RelatedType != "lead" || got.RelatedID != lead {
		t.Fatalf("package link must not replace the related record: %+v", got)
	}
}

func TestCreateTaskWithoutPackageSkipsCatalogue(t *testing.T) {
	ctx, svc, cat, _, _ := linkFixture(t)
	nilID := uuid.Nil
	got, err := svc.Create(ctx, apptask.CreateInput{Title: "Call", Kind: domain.KindCustom, PackageID: &nilID})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Package.Empty() || cat.calls != 0 {
		t.Fatalf("nil package id must mean no link and no lookup: %+v calls=%d", got.Package, cat.calls)
	}
}

func TestCreateTaskRejectsForeignPackage(t *testing.T) {
	ctx, svc, _, pkg, _ := linkFixture(t)
	foreign, strayDep := uuid.New(), uuid.New()
	cases := map[string]apptask.CreateInput{
		"unknown package":   {Title: "x", Kind: domain.KindCustom, PackageID: &foreign},
		"unknown departure": {Title: "x", Kind: domain.KindCustom, PackageID: &pkg, DepartureID: &strayDep},
	}
	for name, in := range cases {
		if _, err := svc.Create(ctx, in); !isValidation(err) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
	}
}

func TestCreateTaskWithPackageNeedsLinker(t *testing.T) {
	ctx, _, _ := createCtx(access.LevelBranch)
	svc := apptask.NewService(newTaskMem(), tx.Nop{}, events.NewBus(nil))
	pkg := uuid.New()
	if _, err := svc.Create(ctx, apptask.CreateInput{Title: "x", Kind: domain.KindCustom, PackageID: &pkg}); !isValidation(err) {
		t.Fatalf("without a catalogue the link must be refused, got %v", err)
	}
}

func TestLinkPackageSetsChangesAndClears(t *testing.T) {
	ctx, svc, _, pkg, dep := linkFixture(t)
	task, err := svc.Create(ctx, apptask.CreateInput{Title: "Collect passports", Kind: domain.KindDocument})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.LinkPackage(ctx, task.ID, apptask.LinkPackageInput{PackageID: &pkg})
	if err != nil || got.Package.PackageID == nil || *got.Package.PackageID != pkg || got.Package.DepartureID != nil {
		t.Fatalf("link package: %+v %v", got, err)
	}
	got, err = svc.LinkPackage(ctx, task.ID, apptask.LinkPackageInput{PackageID: &pkg, DepartureID: &dep})
	if err != nil || got.Package.DepartureID == nil || *got.Package.DepartureID != dep {
		t.Fatalf("add departure: %+v %v", got, err)
	}
	got, err = svc.LinkPackage(ctx, task.ID, apptask.LinkPackageInput{})
	if err != nil || !got.Package.Empty() {
		t.Fatalf("clear: %+v %v", got, err)
	}
}

func TestLinkPackageRefusesClosedAndForeign(t *testing.T) {
	ctx, svc, _, pkg, _ := linkFixture(t)
	task, err := svc.Create(ctx, apptask.CreateInput{Title: "Book hotel", Kind: domain.KindCustom})
	if err != nil {
		t.Fatal(err)
	}
	foreign := uuid.New()
	if _, err := svc.LinkPackage(ctx, task.ID, apptask.LinkPackageInput{PackageID: &foreign}); !isValidation(err) {
		t.Fatalf("foreign package: want validation, got %v", err)
	}
	if _, err := svc.Complete(ctx, task.ID, apptask.CompleteInput{Outcome: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkPackage(ctx, task.ID, apptask.LinkPackageInput{PackageID: &pkg}); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("closed task: want invalid state, got %v", err)
	}
	if _, err := svc.LinkPackage(ctx, uuid.New(), apptask.LinkPackageInput{PackageID: &pkg}); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("missing task: want not found, got %v", err)
	}
}
