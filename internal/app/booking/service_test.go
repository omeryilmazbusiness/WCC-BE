package booking_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	pkgdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

func sysCtx() context.Context {
	return access.WithScope(context.Background(), access.System())
}

type memBookingRepo struct {
	byID      map[uuid.UUID]*domain.Booking
	parts     map[uuid.UUID][]domain.Participant
	lines     map[uuid.UUID][]domain.LineItem
	check     map[uuid.UUID][]domain.ChecklistItem
	overrides map[uuid.UUID]*domain.ReadinessOverride
	deps      *memDepRepo
	locks     int
}

func newMemBooking() *memBookingRepo {
	return &memBookingRepo{
		byID: map[uuid.UUID]*domain.Booking{}, parts: map[uuid.UUID][]domain.Participant{},
		lines: map[uuid.UUID][]domain.LineItem{}, check: map[uuid.UUID][]domain.ChecklistItem{},
	}
}

func (m *memBookingRepo) Create(_ context.Context, b *domain.Booking) error {
	cp := *b
	m.byID[b.ID] = &cp
	return nil
}

// Update mirrors the postgres adapter: lifecycle columns are not written.
func (m *memBookingRepo) Update(_ context.Context, b *domain.Booking) error {
	cur, ok := m.byID[b.ID]
	if !ok {
		return shared.NewNotFound("booking")
	}
	cp := *b
	cp.Status, cp.HoldExpiresAt, cp.StatusChangedAt, cp.StatusReason, cp.ReadyForced =
		cur.Status, cur.HoldExpiresAt, cur.StatusChangedAt, cur.StatusReason, cur.ReadyForced
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
func (m *memBookingRepo) FindForUpdate(_ context.Context, id uuid.UUID) (*domain.Booking, error) {
	b, ok := m.byID[id]
	if !ok {
		return nil, shared.NewNotFound("booking")
	}
	cp := *b
	return &cp, nil
}
func (m *memBookingRepo) SaveStatus(_ context.Context, b *domain.Booking) error {
	cur, ok := m.byID[b.ID]
	if !ok {
		return shared.NewNotFound("booking")
	}
	cur.Status, cur.HoldExpiresAt, cur.StatusChangedAt, cur.StatusReason, cur.ReadyForced, cur.UpdatedAt =
		b.Status, b.HoldExpiresAt, b.StatusChangedAt, b.StatusReason, b.ReadyForced, b.UpdatedAt
	return nil
}
func (m *memBookingRepo) LockDeparture(context.Context, uuid.UUID) error {
	m.locks++
	return nil
}
func (m *memBookingRepo) page(after uuid.UUID, limit int, keep func(*domain.Booking) bool) []uuid.UUID {
	var ids []uuid.UUID
	for id, b := range m.byID {
		if keep(b) && id.String() > after.String() {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids
}
func (m *memBookingRepo) ListExpiredHolds(_ context.Context, now time.Time, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	return m.page(after, limit, func(b *domain.Booking) bool {
		return b.Status == domain.StatusOptionHold && !b.HoldExpiresAt.After(now)
	}), nil
}
func (m *memBookingRepo) ListDueForTravel(_ context.Context, today time.Time, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	return m.page(after, limit, func(b *domain.Booking) bool {
		d, ok := m.deps.deps[b.DepartureID]
		return ok && b.Status.IsConfirmedFamily() && domain.DepartureReached(d.DepartDate, today)
	}), nil
}
func (m *memBookingRepo) ListDerivedCandidates(_ context.Context, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	return m.page(after, limit, func(b *domain.Booking) bool { return b.Status.IsConfirmedFamily() }), nil
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
		if b.DepartureID == departureID && b.Status.ConsumesSeat() {
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

// setMoney simulates the payment service's balance sync.
func (m *memBookingRepo) setMoney(id uuid.UUID, collected int64) {
	b := m.byID[id]
	b.CollectedAmt = collected
	b.RecomputeBalance()
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

func newBus() *events.Bus {
	return events.NewBus(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

func newSvc(books *memBookingRepo, deps *memDepRepo) *appbooking.Service {
	books.deps = deps
	return appbooking.NewService(books, books, deps, tx.Nop{}, newBus())
}

func departure(capacity int, departIn time.Duration) *pkgdomain.Departure {
	return &pkgdomain.Departure{
		ID: uuid.New(), CapacityTotal: capacity, Currency: "USD", IsActive: true,
		DepartDate: time.Now().UTC().Add(departIn), ReturnDate: time.Now().UTC().Add(departIn + 10*24*time.Hour),
	}
}

type fixture struct {
	books *memBookingRepo
	deps  *memDepRepo
	dep   *pkgdomain.Departure
	svc   *appbooking.Service
}

func newFixture(capacity int) *fixture {
	dep := departure(capacity, 30*24*time.Hour)
	f := &fixture{books: newMemBooking(), deps: &memDepRepo{deps: map[uuid.UUID]*pkgdomain.Departure{dep.ID: dep}}, dep: dep}
	f.svc = newSvc(f.books, f.deps)
	return f
}

func (f *fixture) draft(t *testing.T, pax int, total int64) *domain.Booking {
	t.Helper()
	b, err := f.svc.CreateDraft(sysCtx(), appbooking.CreateInput{
		BranchID: uuid.New(), CustomerID: uuid.New(), DepartureID: f.dep.ID,
		PaxCount: pax, TotalAmount: total, OwnerID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// makeReady completes participants and the required checklist.
func (f *fixture) makeReady(t *testing.T, b *domain.Booking) {
	t.Helper()
	for i := 0; i < b.PaxCount; i++ {
		if _, err := f.svc.AddParticipant(sysCtx(), b.ID, appbooking.AddParticipantInput{FullName: "P", PassportNo: "P1"}); err != nil {
			t.Fatal(err)
		}
	}
	items, _ := f.svc.ListChecklist(sysCtx(), b.ID)
	for _, it := range items {
		if it.Required {
			if _, err := f.svc.UpdateChecklistItem(sysCtx(), b.ID, it.ID, appbooking.ChecklistUpdateInput{Completed: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func (f *fixture) status(id uuid.UUID) domain.Status { return f.books.byID[id].Status }

func code(err error) string {
	var app *shared.AppError
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

func TestCreateDraftSeedsChecklistAndLineRecalc(t *testing.T) {
	f := newFixture(40)
	b := f.draft(t, 2, 0)
	cl, err := f.svc.ListChecklist(sysCtx(), b.ID)
	if err != nil || len(cl) < 3 {
		t.Fatalf("checklist seed failed: %v len=%d", err, len(cl))
	}

	updated, lines, err := f.svc.SetLineItems(sysCtx(), b.ID, []appbooking.LineItemInput{
		{Kind: domain.LinePackage, Label: "Umrah", Quantity: 2, UnitPrice: 1500, UnitCost: 1100},
		{Kind: domain.KindItem, Category: domain.LineExtras, Label: "Ziyarah", Quantity: 1, UnitPrice: 200, UnitCost: 80},
		{Kind: domain.KindTax, Label: "VAT", Quantity: 1, UnitPrice: 480},
		{Kind: domain.KindFee, Label: "Service", Quantity: 1, UnitPrice: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 4 || lines[0].Kind != domain.KindItem || lines[0].Category != domain.LinePackage {
		t.Fatalf("lines=%+v", lines)
	}
	if updated.TotalAmount != 3730 || updated.TaxAmt != 480 || updated.FeeAmt != 50 || updated.CostAmt != 2280 {
		t.Fatalf("totals total=%d tax=%d fee=%d cost=%d", updated.TotalAmount, updated.TaxAmt, updated.FeeAmt, updated.CostAmt)
	}
	if updated.Margin() != 920 || updated.Subtotal() != 3200 {
		t.Fatalf("margin=%d subtotal=%d", updated.Margin(), updated.Subtotal())
	}
	for _, bad := range []appbooking.LineItemInput{
		{Kind: domain.KindItem, Quantity: 1},
		{Kind: domain.KindTax, Category: domain.LineHotel, Quantity: 1},
		{Kind: "bonus", Quantity: 1},
	} {
		if _, _, err := f.svc.SetLineItems(sysCtx(), b.ID, []appbooking.LineItemInput{bad}); err == nil {
			t.Fatalf("line %+v must be rejected", bad)
		}
	}
}

func TestDiscountRequiresPermissionAndSubtotal(t *testing.T) {
	f := newFixture(40)
	if _, err := f.svc.CreateDraft(sysCtx(), appbooking.CreateInput{
		BranchID: uuid.New(), CustomerID: uuid.New(), DepartureID: f.dep.ID, PaxCount: 1, DiscountAmt: 10, OwnerID: uuid.New(),
	}); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("create with discount without permission: %v", err)
	}
	b := f.draft(t, 1, 0)
	if _, _, err := f.svc.SetLineItems(sysCtx(), b.ID, []appbooking.LineItemInput{
		{Kind: domain.KindItem, Category: domain.LinePackage, Quantity: 1, UnitPrice: 1000},
		{Kind: domain.KindTax, Quantity: 1, UnitPrice: 100},
	}); err != nil {
		t.Fatal(err)
	}
	disc := int64(200)
	if _, err := f.svc.Update(sysCtx(), b.ID, appbooking.UpdateInput{PaxCount: 1, DiscountAmt: &disc}); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("discount change without permission: %v", err)
	}
	updated, err := f.svc.Update(sysCtx(), b.ID, appbooking.UpdateInput{PaxCount: 1, DiscountAmt: &disc, CanDiscount: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TotalAmount != 900 || updated.DiscountAmt != 200 {
		t.Fatalf("total=%d discount=%d", updated.TotalAmount, updated.DiscountAmt)
	}
	same := int64(200)
	if _, err := f.svc.Update(sysCtx(), b.ID, appbooking.UpdateInput{PaxCount: 1, DiscountAmt: &same}); err != nil {
		t.Fatalf("unchanged discount needs no permission: %v", err)
	}
	over := int64(1001)
	if _, err := f.svc.Update(sysCtx(), b.ID, appbooking.UpdateInput{PaxCount: 1, DiscountAmt: &over, CanDiscount: true}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("discount above subtotal: %v", err)
	}
}

func TestConfirmNeedsCapacityNotReadiness(t *testing.T) {
	f := newFixture(1)
	b := f.draft(t, 2, 1000)
	_, err := f.svc.Confirm(sysCtx(), b.ID)
	if gs := domain.FailedGuards(err); len(gs) != 1 || gs[0] != domain.GuardNoCapacity {
		t.Fatalf("want no_capacity, got %v", err)
	}
	if _, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{
		Status: domain.StatusConfirmed, Override: true, Reason: "manager approved oversell",
	}); len(domain.FailedGuards(err)) == 0 {
		t.Fatal("override must not bypass capacity")
	}
	f.dep.CapacityTotal = 10
	confirmed, err := f.svc.Confirm(sysCtx(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != domain.StatusConfirmed || f.dep.CapacitySold != 2 || f.books.locks == 0 {
		t.Fatalf("status=%s sold=%d locks=%d", confirmed.Status, f.dep.CapacitySold, f.books.locks)
	}
	r, err := f.svc.Readiness(sysCtx(), b.ID)
	if err != nil || r.ReadinessOK || r.CanConfirm {
		t.Fatalf("readiness=%+v err=%v", r, err)
	}
}

func TestConfirmSettlesOnDerivedStatus(t *testing.T) {
	f := newFixture(10)
	b := f.draft(t, 1, 1000)
	f.books.setMoney(b.ID, 300)
	rec := &recAudit{}
	f.svc.SetAuditor(rec)
	got, err := f.svc.Confirm(sysCtx(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusPartiallyPaid || f.status(b.ID) != domain.StatusPartiallyPaid {
		t.Fatalf("status=%s", got.Status)
	}
	changes := rec.all("booking.status_changed")
	if len(changes) != 2 || changes[0].Extra["actor_kind"] != domain.ActorUser || changes[1].Extra["actor_kind"] != domain.ActorSystem {
		t.Fatalf("audits=%+v", changes)
	}
	if rec.actors[len(rec.actors)-1] != "system" {
		t.Fatalf("derived transition must be audited as system, got %v", rec.actors)
	}
}

func TestUserCannotMoveWithinConfirmedFamily(t *testing.T) {
	f := newFixture(10)
	b := f.draft(t, 1, 1000)
	if _, err := f.svc.Confirm(sysCtx(), b.ID); err != nil {
		t.Fatal(err)
	}
	for _, to := range []domain.Status{domain.StatusPartiallyPaid, domain.StatusReady, domain.StatusTravelled, domain.StatusDraft} {
		if _, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: to}); code(err) != domain.CodeInvalidTransition {
			t.Fatalf("confirmed→%s: %v", to, err)
		}
	}
}

func TestRecomputeFollowsPaymentsAndReadiness(t *testing.T) {
	f := newFixture(10)
	b := f.draft(t, 1, 1000)
	if _, err := f.svc.Confirm(sysCtx(), b.ID); err != nil {
		t.Fatal(err)
	}
	f.books.setMoney(b.ID, 400)
	if got, _ := f.svc.Recompute(sysCtx(), b.ID); got.Status != domain.StatusPartiallyPaid {
		t.Fatalf("after deposit: %s", got.Status)
	}
	f.books.setMoney(b.ID, 1000)
	if got, _ := f.svc.Recompute(sysCtx(), b.ID); got.Status != domain.StatusPartiallyPaid {
		t.Fatalf("paid but not ready: %s", got.Status)
	}
	f.makeReady(t, b)
	if f.status(b.ID) != domain.StatusReady {
		t.Fatalf("readiness change must re-derive: %s", f.status(b.ID))
	}
	rec := &recAudit{}
	f.svc.SetAuditor(rec)
	if got, _ := f.svc.Recompute(sysCtx(), b.ID); got.Status != domain.StatusReady || len(rec.events) != 0 {
		t.Fatalf("recompute must be idempotent: %s %d audits", got.Status, len(rec.events))
	}
	f.books.setMoney(b.ID, 0)
	if got, _ := f.svc.Recompute(sysCtx(), b.ID); got.Status != domain.StatusConfirmed {
		t.Fatalf("after full reversal: %s", got.Status)
	}
}

func TestOverrideForcesReadyAndSticks(t *testing.T) {
	f := newFixture(10)
	b := f.draft(t, 1, 1000)
	if _, err := f.svc.Confirm(sysCtx(), b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusReady}); code(err) != domain.CodeInvalidTransition {
		t.Fatalf("ready without override: %v", err)
	}
	if _, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusReady, Override: true, Reason: "short"}); code(err) != domain.CodeGuardFailed {
		t.Fatalf("short override reason: %v", err)
	}
	rec := &recAudit{}
	f.svc.SetAuditor(rec)
	got, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{
		Status: domain.StatusReady, Override: true, Reason: "VIP group, docs arrive at airport",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusReady || !got.ReadyForced {
		t.Fatalf("status=%s forced=%v", got.Status, got.ReadyForced)
	}
	ev := rec.find("booking.status_overridden")
	if ev == nil {
		t.Fatal("override must be audited as booking.status_overridden")
	}
	bypassed, _ := ev.Extra["guards_bypassed"].([]string)
	if len(bypassed) != 2 {
		t.Fatalf("guards_bypassed=%v", ev.Extra["guards_bypassed"])
	}
	f.books.setMoney(b.ID, 100)
	if got, _ := f.svc.Recompute(sysCtx(), b.ID); got.Status != domain.StatusReady {
		t.Fatalf("forced ready must stick: %s", got.Status)
	}
}

func TestCancelNeedsReasonAndReleasesSeats(t *testing.T) {
	f := newFixture(10)
	b := f.draft(t, 3, 1000)
	if _, err := f.svc.Confirm(sysCtx(), b.ID); err != nil {
		t.Fatal(err)
	}
	if f.dep.CapacitySold != 3 {
		t.Fatalf("sold=%d", f.dep.CapacitySold)
	}
	if _, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusCancelled}); code(err) != domain.CodeGuardFailed {
		t.Fatalf("cancel without reason: %v", err)
	}
	got, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusCancelled, Reason: "customer withdrew"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusCancelled || got.StatusReason != "customer withdrew" || f.dep.CapacitySold != 0 {
		t.Fatalf("status=%s reason=%q sold=%d", got.Status, got.StatusReason, f.dep.CapacitySold)
	}
	if _, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusDraft}); code(err) != domain.CodeInvalidTransition {
		t.Fatalf("cancelled is terminal: %v", err)
	}
}

func TestQuotedAndOptionHoldFlow(t *testing.T) {
	f := newFixture(4)
	b := f.draft(t, 2, 1000)
	if _, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusQuoted}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusOptionHold}); code(err) != domain.CodeGuardFailed {
		t.Fatalf("hold without expiry: %v", err)
	}
	hold := time.Now().UTC().Add(72 * time.Hour)
	got, err := f.svc.Transition(sysCtx(), b.ID, appbooking.TransitionInput{Status: domain.StatusOptionHold, HoldExpiresAt: &hold})
	if err != nil {
		t.Fatal(err)
	}
	if got.HoldExpiresAt == nil || f.dep.CapacitySold != 2 {
		t.Fatalf("hold=%v sold=%d", got.HoldExpiresAt, f.dep.CapacitySold)
	}
	f.dep.CapacityTotal = 2
	got, err = f.svc.Confirm(sysCtx(), b.ID)
	if err != nil {
		t.Fatalf("held seats confirm without free capacity: %v", err)
	}
	if got.HoldExpiresAt != nil || f.dep.CapacitySold != 2 {
		t.Fatalf("hold=%v sold=%d", got.HoldExpiresAt, f.dep.CapacitySold)
	}
}

func TestListFilterByCustomer(t *testing.T) {
	f := newFixture(5)
	cust := uuid.New()
	_, _ = f.svc.CreateDraft(sysCtx(), appbooking.CreateInput{
		BranchID: uuid.New(), CustomerID: cust, DepartureID: f.dep.ID, PaxCount: 1, OwnerID: uuid.New(),
	})
	_, _ = f.svc.CreateDraft(sysCtx(), appbooking.CreateInput{
		BranchID: uuid.New(), CustomerID: uuid.New(), DepartureID: f.dep.ID, PaxCount: 1, OwnerID: uuid.New(),
	})
	items, total, err := f.svc.List(sysCtx(), appbooking.ListInput{CustomerID: &cust})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("list filter failed total=%d len=%d err=%v", total, len(items), err)
	}
}
