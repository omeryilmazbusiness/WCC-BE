package booking_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	pkgdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memBookingRepo struct {
	byID      map[uuid.UUID]*domain.Booking
	parts     map[uuid.UUID][]domain.Participant
	lines     map[uuid.UUID][]domain.LineItem
	check     map[uuid.UUID][]domain.ChecklistItem
	sold      map[uuid.UUID]int
	overrides map[uuid.UUID]*domain.ReadinessOverride
}

func newMemBooking() *memBookingRepo {
	return &memBookingRepo{
		byID: map[uuid.UUID]*domain.Booking{}, parts: map[uuid.UUID][]domain.Participant{},
		lines: map[uuid.UUID][]domain.LineItem{}, check: map[uuid.UUID][]domain.ChecklistItem{},
		sold: map[uuid.UUID]int{},
	}
}

func (m *memBookingRepo) Create(_ context.Context, b *domain.Booking) error {
	cp := *b
	m.byID[b.ID] = &cp
	return nil
}
func (m *memBookingRepo) Update(_ context.Context, b *domain.Booking) error {
	cp := *b
	m.byID[b.ID] = &cp
	return nil
}
func (m *memBookingRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Booking, error) {
	b, ok := m.byID[id]
	if !ok {
		return nil, context.Canceled
	}
	cp := *b
	return &cp, nil
}
func (m *memBookingRepo) List(_ context.Context, f domain.ListFilter) ([]domain.Booking, int, error) {
	var out []domain.Booking
	for _, b := range m.byID {
		if f.Status != "" && b.Status != f.Status {
			continue
		}
		if f.CustomerID != nil && b.CustomerID != *f.CustomerID {
			continue
		}
		out = append(out, *b)
	}
	return out, len(out), nil
}
func (m *memBookingRepo) AddParticipant(_ context.Context, p *domain.Participant) error {
	cp := *p
	m.parts[p.BookingID] = append(m.parts[p.BookingID], cp)
	return nil
}
func (m *memBookingRepo) UpdateParticipant(_ context.Context, p *domain.Participant) error {
	list := m.parts[p.BookingID]
	for i := range list {
		if list[i].ID == p.ID {
			list[i] = *p
			m.parts[p.BookingID] = list
			return nil
		}
	}
	return context.Canceled
}
func (m *memBookingRepo) DeleteParticipant(_ context.Context, bookingID, participantID uuid.UUID) error {
	list := m.parts[bookingID]
	out := list[:0]
	for _, p := range list {
		if p.ID != participantID {
			out = append(out, p)
		}
	}
	m.parts[bookingID] = out
	return nil
}
func (m *memBookingRepo) ListParticipants(_ context.Context, bookingID uuid.UUID) ([]domain.Participant, error) {
	return append([]domain.Participant{}, m.parts[bookingID]...), nil
}
func (m *memBookingRepo) CountConfirmedPaxByDeparture(_ context.Context, departureID uuid.UUID) (int, error) {
	n := 0
	for _, b := range m.byID {
		if b.DepartureID == departureID && b.Status == domain.StatusConfirmed {
			n += b.PaxCount
		}
	}
	return n, nil
}
func (m *memBookingRepo) ListByDeparture(_ context.Context, departureID uuid.UUID) ([]domain.Booking, error) {
	var out []domain.Booking
	for _, b := range m.byID {
		if b.DepartureID == departureID {
			out = append(out, *b)
		}
	}
	return out, nil
}
func (m *memBookingRepo) ReplaceLineItems(_ context.Context, bookingID uuid.UUID, items []domain.LineItem) error {
	m.lines[bookingID] = append([]domain.LineItem{}, items...)
	return nil
}
func (m *memBookingRepo) ListLineItems(_ context.Context, bookingID uuid.UUID) ([]domain.LineItem, error) {
	return append([]domain.LineItem{}, m.lines[bookingID]...), nil
}
func (m *memBookingRepo) SeedChecklist(_ context.Context, items []domain.ChecklistItem) error {
	if len(items) == 0 {
		return nil
	}
	bid := items[0].BookingID
	m.check[bid] = append([]domain.ChecklistItem{}, items...)
	return nil
}
func (m *memBookingRepo) ListChecklist(_ context.Context, bookingID uuid.UUID) ([]domain.ChecklistItem, error) {
	return append([]domain.ChecklistItem{}, m.check[bookingID]...), nil
}
func (m *memBookingRepo) UpdateChecklistItem(_ context.Context, item *domain.ChecklistItem) error {
	list := m.check[item.BookingID]
	for i := range list {
		if list[i].ID == item.ID {
			list[i] = *item
			m.check[item.BookingID] = list
			return nil
		}
	}
	return context.Canceled
}
func (m *memBookingRepo) UpsertReadinessOverride(_ context.Context, o *domain.ReadinessOverride) error {
	if m.overrides == nil {
		m.overrides = map[uuid.UUID]*domain.ReadinessOverride{}
	}
	cp := *o
	m.overrides[o.BookingID] = &cp
	return nil
}
func (m *memBookingRepo) FindReadinessOverride(_ context.Context, bookingID uuid.UUID) (*domain.ReadinessOverride, error) {
	if m.overrides == nil {
		return nil, nil
	}
	o, ok := m.overrides[bookingID]
	if !ok {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}

type memDepRepo struct {
	deps map[uuid.UUID]*pkgdomain.Departure
}

func (m *memDepRepo) CreatePackage(context.Context, *pkgdomain.Package) error { return nil }
func (m *memDepRepo) UpdatePackage(context.Context, *pkgdomain.Package) error { return nil }
func (m *memDepRepo) FindPackage(context.Context, uuid.UUID) (*pkgdomain.Package, error) {
	return nil, context.Canceled
}
func (m *memDepRepo) ListPackages(context.Context, uuid.UUID, bool) ([]pkgdomain.Package, error) {
	return nil, nil
}
func (m *memDepRepo) CreateDeparture(context.Context, *pkgdomain.Departure) error { return nil }
func (m *memDepRepo) UpdateDeparture(_ context.Context, d *pkgdomain.Departure) error {
	cp := *d
	m.deps[d.ID] = &cp
	return nil
}
func (m *memDepRepo) FindDeparture(_ context.Context, id uuid.UUID) (*pkgdomain.Departure, error) {
	d, ok := m.deps[id]
	if !ok {
		return nil, context.Canceled
	}
	cp := *d
	return &cp, nil
}
func (m *memDepRepo) UpdateDepartureCapacitySold(_ context.Context, id uuid.UUID, sold int) error {
	m.deps[id].CapacitySold = sold
	return nil
}
func (m *memDepRepo) ListDepartures(context.Context, uuid.UUID) ([]pkgdomain.Departure, error) {
	return nil, nil
}
func (m *memDepRepo) ReplacePackageTiers(context.Context, uuid.UUID, []pkgdomain.PricingTier) error {
	return nil
}
func (m *memDepRepo) ListPackageTiers(context.Context, uuid.UUID) ([]pkgdomain.PricingTier, error) {
	return nil, nil
}
func (m *memDepRepo) SnapshotTiersToDeparture(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (m *memDepRepo) ListDepartureTiers(context.Context, uuid.UUID) ([]pkgdomain.PricingTier, error) {
	return nil, nil
}
func (m *memDepRepo) ReplaceDepartureTiers(context.Context, uuid.UUID, []pkgdomain.PricingTier) error {
	return nil
}

func newSvc(books *memBookingRepo, deps *memDepRepo) *appbooking.Service {
	bus := events.NewBus(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	return appbooking.NewService(books, deps, tx.Nop{}, bus)
}

func TestCreateDraftSeedsChecklistAndLineRecalc(t *testing.T) {
	books := newMemBooking()
	depID := uuid.New()
	deps := &memDepRepo{deps: map[uuid.UUID]*pkgdomain.Departure{
		depID: {
			ID: depID, CapacityTotal: 40, CapacitySold: 0, Currency: "USD",
			DepartDate: time.Now().UTC().Add(30 * 24 * time.Hour),
			ReturnDate: time.Now().UTC().Add(40 * 24 * time.Hour),
		},
	}}
	svc := newSvc(books, deps)

	b, err := svc.CreateDraft(context.Background(), appbooking.CreateInput{
		BranchID: uuid.New(), CustomerID: uuid.New(), DepartureID: depID,
		PaxCount: 2, TotalAmount: 0, OwnerID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	cl, err := svc.ListChecklist(context.Background(), b.ID)
	if err != nil || len(cl) < 3 {
		t.Fatalf("checklist seed failed: %v len=%d", err, len(cl))
	}

	updated, lines, err := svc.SetLineItems(context.Background(), b.ID, []appbooking.LineItemInput{
		{Kind: domain.LinePackage, Label: "Umrah", Quantity: 2, UnitPrice: 1500, UnitCost: 1100},
		{Kind: domain.LineExtras, Label: "Ziyarah", Quantity: 1, UnitPrice: 200, UnitCost: 80},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines=%d", len(lines))
	}
	if updated.TotalAmount != 3200 || updated.CostAmt != 2280 {
		t.Fatalf("totals total=%d cost=%d", updated.TotalAmount, updated.CostAmt)
	}
	if updated.Margin() != 920 {
		t.Fatalf("margin=%d", updated.Margin())
	}
}

func TestConfirmBlockedUntilReady(t *testing.T) {
	books := newMemBooking()
	depID := uuid.New()
	deps := &memDepRepo{deps: map[uuid.UUID]*pkgdomain.Departure{
		depID: {
			ID: depID, CapacityTotal: 10, CapacitySold: 0, Currency: "USD", IsActive: true,
			DepartDate: time.Now().UTC().Add(45 * 24 * time.Hour),
			ReturnDate: time.Now().UTC().Add(55 * 24 * time.Hour),
		},
	}}
	svc := newSvc(books, deps)
	b, err := svc.CreateDraft(context.Background(), appbooking.CreateInput{
		BranchID: uuid.New(), CustomerID: uuid.New(), DepartureID: depID,
		PaxCount: 1, TotalAmount: 1000, OwnerID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(context.Background(), b.ID); err == nil {
		t.Fatal("confirm must block when participants/checklist incomplete")
	}

	_, err = svc.AddParticipant(context.Background(), b.ID, appbooking.AddParticipantInput{
		FullName: "Ali", PassportNo: "P1", Nationality: "TR",
	})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := svc.ListChecklist(context.Background(), b.ID)
	for _, it := range items {
		if !it.Required {
			continue
		}
		if _, err := svc.UpdateChecklistItem(context.Background(), b.ID, it.ID, appbooking.ChecklistUpdateInput{Completed: true}); err != nil {
			t.Fatal(err)
		}
	}
	ready, err := svc.Readiness(context.Background(), b.ID)
	if err != nil || !ready.CanConfirm {
		t.Fatalf("expected ready: %#v err=%v", ready, err)
	}
	confirmed, err := svc.Confirm(context.Background(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != domain.StatusConfirmed {
		t.Fatalf("status=%s", confirmed.Status)
	}
	if deps.deps[depID].CapacitySold != 1 {
		t.Fatalf("capacity_sold=%d", deps.deps[depID].CapacitySold)
	}
}

func TestListFilterByCustomer(t *testing.T) {
	books := newMemBooking()
	depID := uuid.New()
	cust := uuid.New()
	deps := &memDepRepo{deps: map[uuid.UUID]*pkgdomain.Departure{
		depID: {ID: depID, CapacityTotal: 5, Currency: "USD", IsActive: true,
			DepartDate: time.Now().UTC().Add(20 * 24 * time.Hour),
			ReturnDate: time.Now().UTC().Add(30 * 24 * time.Hour)},
	}}
	svc := newSvc(books, deps)
	_, _ = svc.CreateDraft(context.Background(), appbooking.CreateInput{
		BranchID: uuid.New(), CustomerID: cust, DepartureID: depID, PaxCount: 1, OwnerID: uuid.New(),
	})
	_, _ = svc.CreateDraft(context.Background(), appbooking.CreateInput{
		BranchID: uuid.New(), CustomerID: uuid.New(), DepartureID: depID, PaxCount: 1, OwnerID: uuid.New(),
	})
	items, total, err := svc.List(context.Background(), appbooking.ListInput{CustomerID: &cust})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("list filter failed total=%d len=%d err=%v", total, len(items), err)
	}
}
