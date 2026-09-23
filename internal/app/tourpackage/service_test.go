package tourpackage_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	apppkg "github.com/wodi-crm/wodi-crm-be/internal/app/tourpackage"
	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

func TestDepartureCapacityAlert(t *testing.T) {
	d := &domain.Departure{CapacityTotal: 10, CapacitySold: 8, SoftThresholdPct: 80}
	if d.CapacityAlert() != "low" {
		t.Fatalf("alert=%s", d.CapacityAlert())
	}
	d.CapacitySold = 10
	if d.CapacityAlert() != "full" {
		t.Fatal("expected full")
	}
	d.CapacitySold = 11
	if d.CapacityAlert() != "oversold" {
		t.Fatal("expected oversold")
	}
	d.CapacitySold = 0
	d.SalesClosed = true
	if d.CapacityAlert() != "full" {
		t.Fatal("closed sales => full")
	}
	if d.CanSell(1) {
		t.Fatal("closed sales cannot sell")
	}
}

func TestCloneResetsSold(t *testing.T) {
	src := &domain.Departure{
		PackageID: uuid.New(), CapacityTotal: 40, CapacitySold: 12,
		BasePrice: 5000, Currency: "USD", SoftThresholdPct: 75, AllowOversell: true,
	}
	depart := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	ret := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	cloned := src.Clone(uuid.New(), "UMR-NOV", depart, ret)
	if cloned.CapacitySold != 0 || cloned.SalesClosed {
		t.Fatal("clone must reset sold/closed")
	}
	if cloned.SoftThresholdPct != 75 || !cloned.AllowOversell {
		t.Fatal("clone should preserve threshold/oversell")
	}
}

type memPkgRepo struct {
	pkgs  map[uuid.UUID]*domain.Package
	deps  map[uuid.UUID]*domain.Departure
	tiers map[uuid.UUID][]domain.PricingTier
	dTier map[uuid.UUID][]domain.PricingTier
}

func newMemPkg() *memPkgRepo {
	return &memPkgRepo{
		pkgs: map[uuid.UUID]*domain.Package{}, deps: map[uuid.UUID]*domain.Departure{},
		tiers: map[uuid.UUID][]domain.PricingTier{}, dTier: map[uuid.UUID][]domain.PricingTier{},
	}
}

func (m *memPkgRepo) CreatePackage(_ context.Context, p *domain.Package) error {
	cp := *p
	m.pkgs[p.ID] = &cp
	return nil
}
func (m *memPkgRepo) UpdatePackage(_ context.Context, p *domain.Package) error {
	cp := *p
	m.pkgs[p.ID] = &cp
	return nil
}
func (m *memPkgRepo) FindPackage(_ context.Context, id uuid.UUID) (*domain.Package, error) {
	p, ok := m.pkgs[id]
	if !ok {
		return nil, context.Canceled
	}
	cp := *p
	return &cp, nil
}
func (m *memPkgRepo) ListPackages(_ context.Context, _ uuid.UUID, _ bool) ([]domain.Package, error) {
	return nil, nil
}
func (m *memPkgRepo) CreateDeparture(_ context.Context, d *domain.Departure) error {
	cp := *d
	m.deps[d.ID] = &cp
	return nil
}
func (m *memPkgRepo) UpdateDeparture(_ context.Context, d *domain.Departure) error {
	cp := *d
	m.deps[d.ID] = &cp
	return nil
}
func (m *memPkgRepo) FindDeparture(_ context.Context, id uuid.UUID) (*domain.Departure, error) {
	d, ok := m.deps[id]
	if !ok {
		return nil, context.Canceled
	}
	cp := *d
	return &cp, nil
}
func (m *memPkgRepo) UpdateDepartureCapacitySold(_ context.Context, id uuid.UUID, sold int) error {
	d := m.deps[id]
	d.CapacitySold = sold
	return nil
}
func (m *memPkgRepo) ListDepartures(_ context.Context, packageID uuid.UUID) ([]domain.Departure, error) {
	var out []domain.Departure
	for _, d := range m.deps {
		if d.PackageID == packageID {
			out = append(out, *d)
		}
	}
	return out, nil
}
func (m *memPkgRepo) ReplacePackageTiers(_ context.Context, packageID uuid.UUID, tiers []domain.PricingTier) error {
	m.tiers[packageID] = append([]domain.PricingTier{}, tiers...)
	return nil
}
func (m *memPkgRepo) ListPackageTiers(_ context.Context, packageID uuid.UUID) ([]domain.PricingTier, error) {
	return append([]domain.PricingTier{}, m.tiers[packageID]...), nil
}
func (m *memPkgRepo) SnapshotTiersToDeparture(_ context.Context, packageID, departureID uuid.UUID) error {
	m.dTier[departureID] = append([]domain.PricingTier{}, m.tiers[packageID]...)
	return nil
}
func (m *memPkgRepo) ListDepartureTiers(_ context.Context, departureID uuid.UUID) ([]domain.PricingTier, error) {
	return append([]domain.PricingTier{}, m.dTier[departureID]...), nil
}
func (m *memPkgRepo) ReplaceDepartureTiers(_ context.Context, departureID uuid.UUID, tiers []domain.PricingTier) error {
	m.dTier[departureID] = append([]domain.PricingTier{}, tiers...)
	return nil
}

type fakeBooks struct {
	pax int
	list []bookingdomain.Booking
}

func (f fakeBooks) ListByDeparture(_ context.Context, _ uuid.UUID) ([]bookingdomain.Booking, error) {
	return f.list, nil
}
func (f fakeBooks) CountConfirmedPaxByDeparture(_ context.Context, _ uuid.UUID) (int, error) {
	return f.pax, nil
}

func TestCreateDepartureSnapshotsTiersAndLocksPrice(t *testing.T) {
	repo := newMemPkg()
	svc := apppkg.NewService(repo, tx.Nop{})
	svc.SetBookingReader(fakeBooks{pax: 2, list: []bookingdomain.Booking{
		{Status: bookingdomain.StatusConfirmed, PaxCount: 2},
		{Status: bookingdomain.StatusDraft, PaxCount: 1},
	}})

	pkg, err := svc.CreatePackage(context.Background(), apppkg.CreatePackageInput{
		BranchID: uuid.New(), Code: "UMR", NameEN: "Umrah",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SetPackageTiers(context.Background(), pkg.ID, []apppkg.TierInput{
		{Code: "DBL", Label: "Double", Kind: domain.TierRoom, Amount: 10000, IsActive: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	depart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	ret := time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC)
	d, err := svc.CreateDeparture(context.Background(), apppkg.CreateDepartureInput{
		PackageID: pkg.ID, Code: "UMR-1", DepartDate: depart, ReturnDate: ret, CapacityTotal: 40, BasePrice: 420000,
	})
	if err != nil {
		t.Fatal(err)
	}
	tiers, _ := svc.ListDepartureTiers(context.Background(), d.ID)
	if len(tiers) != 1 {
		t.Fatalf("snapshot tiers=%d", len(tiers))
	}

	d.CapacitySold = 1
	repo.deps[d.ID].CapacitySold = 1
	price := int64(1)
	_, err = svc.UpdateDeparture(context.Background(), apppkg.UpdateDepartureInput{
		ID: d.ID, BasePrice: &price,
	})
	if err == nil {
		t.Fatal("expected pricing lock")
	}

	ready, err := svc.Readiness(context.Background(), d.ID)
	if err != nil || ready.BookingsConfirmed != 1 || ready.PaxConfirmed != 2 {
		t.Fatalf("readiness %#v err=%v", ready, err)
	}

	closed, err := svc.CloseSales(context.Background(), d.ID, true)
	if err != nil || !closed.SalesClosed {
		t.Fatal("close sales")
	}
}
