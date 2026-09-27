package payment

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var (
	today  = time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	clock  = Clock{Now: func() time.Time { return today.Add(10 * time.Hour) }}
	branch = uuid.New()
)

// paymentMem embeds the port so unused methods panic instead of needing stubs.
type paymentMem struct {
	domain.Repository
	payments  map[uuid.UUID]*domain.Payment
	schedules []domain.Schedule
	promises  map[uuid.UUID]*domain.Promise
	reporting string
	comp      domain.Components
	snapshot  *domain.ReportingSnapshot
}

func newPaymentMem() *paymentMem {
	return &paymentMem{
		payments: map[uuid.UUID]*domain.Payment{}, promises: map[uuid.UUID]*domain.Promise{}, reporting: "USD",
	}
}

func (m *paymentMem) Insert(_ context.Context, p *domain.Payment) error {
	cp := *p
	m.payments[p.ID] = &cp
	return nil
}

func (m *paymentMem) UpdateStatus(_ context.Context, id uuid.UUID, st domain.Status, by *uuid.UUID, at *time.Time) error {
	m.payments[id].Status, m.payments[id].ApprovedBy, m.payments[id].ApprovedAt = st, by, at
	return nil
}

func (m *paymentMem) Get(_ context.Context, id uuid.UUID) (*domain.Payment, error) {
	p, ok := m.payments[id]
	if !ok {
		return nil, shared.NewNotFound("payment")
	}
	cp := *p
	return &cp, nil
}

func (m *paymentMem) FindByIdempotencyKey(_ context.Context, key string) (*domain.Payment, error) {
	for _, p := range m.payments {
		if p.IdempotencyKey == key {
			return p, nil
		}
	}
	return nil, nil
}

func (m *paymentMem) collected(bookingID uuid.UUID, currency string, since time.Time) int64 {
	var sum int64
	for _, p := range m.payments {
		if p.BookingID != bookingID || (currency != "" && p.Currency != currency) || p.CreatedAt.Before(since) {
			continue
		}
		if p.Status == domain.StatusVerified || p.Status == domain.StatusApproved {
			sum += p.Amount
		}
	}
	return sum
}

func (m *paymentMem) SumCollectedByBooking(_ context.Context, bookingID uuid.UUID) (int64, error) {
	return m.collected(bookingID, "", time.Time{}), nil
}

func (m *paymentMem) SumByBookingStatus(_ context.Context, bookingID uuid.UUID, st domain.Status) (int64, error) {
	var sum int64
	for _, p := range m.payments {
		if p.BookingID == bookingID && p.Status == st {
			sum += p.Amount
		}
	}
	return sum, nil
}

func (m *paymentMem) GetFinanceSettings(context.Context, uuid.UUID) (string, error) {
	return m.reporting, nil
}

func (m *paymentMem) MarkSchedulesOverdue(_ context.Context, now time.Time, limit int) ([]domain.Schedule, error) {
	var out []domain.Schedule
	for i := range m.schedules {
		sc := &m.schedules[i]
		if len(out) == limit {
			break
		}
		if sc.Status == domain.ScheduleOpen && sc.DueAt.Before(now) {
			sc.Status = domain.ScheduleOverdue
			sc.BranchID = branch
			out = append(out, *sc)
		}
	}
	return out, nil
}

func (m *paymentMem) ListByStatus(_ context.Context, _ *uuid.UUID, st domain.Status, _ *domain.EventType, _ int) ([]domain.Payment, error) {
	var out []domain.Payment
	for _, p := range m.payments {
		if p.Status == st {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (m *paymentMem) Components(context.Context, uuid.UUID) (domain.Components, error) {
	return m.comp, nil
}

func (m *paymentMem) ReportingSnapshot(context.Context, uuid.UUID) (*domain.ReportingSnapshot, error) {
	return m.snapshot, nil
}

func (m *paymentMem) SetReportingSnapshot(_ context.Context, _ uuid.UUID, s domain.ReportingSnapshot) error {
	m.snapshot = &s
	return nil
}

func (m *paymentMem) InsertPromise(_ context.Context, p *domain.Promise) error {
	cp := *p
	m.promises[p.ID] = &cp
	return nil
}

func (m *paymentMem) GetPromise(_ context.Context, id uuid.UUID) (*domain.Promise, error) {
	p, ok := m.promises[id]
	if !ok {
		return nil, shared.NewNotFound("payment promise")
	}
	cp := *p
	return &cp, nil
}

func (m *paymentMem) ListPromises(_ context.Context, bookingID uuid.UUID) ([]domain.Promise, error) {
	var out []domain.Promise
	for _, p := range m.promises {
		if p.BookingID == bookingID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (m *paymentMem) ListOpenPromises(_ context.Context, after uuid.UUID, limit int) ([]domain.Promise, error) {
	var out []domain.Promise
	for _, p := range m.promises {
		if p.Status == domain.PromiseOpen && p.ID.String() > after.String() {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *paymentMem) ResolvePromise(_ context.Context, p *domain.Promise) (bool, error) {
	cur, ok := m.promises[p.ID]
	if !ok || cur.Status != domain.PromiseOpen {
		return false, nil
	}
	cp := *p
	m.promises[p.ID] = &cp
	return true, nil
}

func (m *paymentMem) SumCollectedSince(_ context.Context, bookingID uuid.UUID, currency string, since time.Time) (int64, error) {
	return m.collected(bookingID, currency, since), nil
}

type bookingMem struct {
	bookingdomain.Repository
	b *bookingdomain.Booking
}

func (m *bookingMem) FindByID(_ context.Context, id uuid.UUID) (*bookingdomain.Booking, error) {
	if m.b == nil || m.b.ID != id {
		return nil, shared.NewNotFound("booking")
	}
	cp := *m.b
	return &cp, nil
}

func (m *bookingMem) Update(_ context.Context, b *bookingdomain.Booking) error {
	cp := *b
	m.b = &cp
	return nil
}

type auditMem struct {
	entries []audit.RecordInput
	fail    error
}

func (a *auditMem) Record(_ context.Context, in audit.RecordInput) error {
	if a.fail != nil {
		return a.fail
	}
	a.entries = append(a.entries, in)
	return nil
}

func (a *auditMem) actions() []string {
	out := make([]string, 0, len(a.entries))
	for _, e := range a.entries {
		out = append(out, e.Action)
	}
	return out
}

// fixedConverter converts SAR→USD at 0.26666667 and knows no other pair.
type fixedConverter struct{}

func (fixedConverter) Convert(_ context.Context, amount int64, from, to string, on time.Time) (fx.Conversion, error) {
	if from != "SAR" || to != "USD" {
		return fx.Conversion{}, fx.ErrRateNotFound
	}
	rate := fx.Rate{Base: from, Quote: to, Scaled: 26666667, EffectiveDate: on, Source: "test"}
	converted, err := fx.Apply(amount, rate.Scaled)
	return fx.Conversion{Amount: converted, Rate: rate}, err
}

type fixture struct {
	svc      *Service
	payments *paymentMem
	bookings *bookingMem
	audit    *auditMem
	booking  *bookingdomain.Booking
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	b := &bookingdomain.Booking{
		ID: uuid.New(), BranchID: branch, OwnerID: uuid.New(), Status: bookingdomain.StatusConfirmed,
		TotalAmount: 100000, DiscountAmt: 5000, CostAmt: 60000, Currency: "SAR",
	}
	f := &fixture{payments: newPaymentMem(), bookings: &bookingMem{b: b}, audit: &auditMem{}, booking: b}
	f.svc = NewService(f.payments, f.bookings, tx.Nop{}, events.NewBus(slog.Default()))
	f.svc.SetAuditor(f.audit)
	f.svc.SetFinance(FinanceDeps{Converter: fixedConverter{}, Bookings: f.payments, Clock: clock})
	return f
}

func appCode(err error) string {
	var app *shared.AppError
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

func (f *fixture) record(t *testing.T, in RecordInput) (*domain.Payment, error) {
	t.Helper()
	if in.BookingID == uuid.Nil {
		in.BookingID = f.booking.ID
	}
	if in.Amount == 0 {
		in.Amount = 30000
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = uuid.NewString()
	}
	return f.svc.Record(context.Background(), in)
}

func TestRecordAutoVerifyNeedsApprover(t *testing.T) {
	f := newFixture(t)
	if _, err := f.record(t, RecordInput{AutoVerify: true}); appCode(err) != "forbidden_auto_verify" || !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("want forbidden_auto_verify, got %v", err)
	}
	p, err := f.record(t, RecordInput{AutoVerify: true, MayApprove: true})
	if err != nil || p.Status != domain.StatusVerified {
		t.Fatalf("approver auto_verify: %v %v", p, err)
	}
	if p, _ := f.record(t, RecordInput{MayApprove: true}); p.Status != domain.StatusUnverified {
		t.Fatalf("without auto_verify status = %s", p.Status)
	}
}

func TestRecordReceivedAtAndSnapshot(t *testing.T) {
	f := newFixture(t)
	future := today.AddDate(0, 0, 1)
	if _, err := f.record(t, RecordInput{ReceivedAt: &future}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("future received_at: %v", err)
	}

	p, err := f.record(t, RecordInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !p.ReceivedAt.Equal(today) {
		t.Fatalf("received_at default = %s", p.ReceivedAt)
	}
	if p.Reporting == nil || p.Reporting.Currency != "USD" || p.Reporting.Amount != 8000 || p.Reporting.RateScaled != 26666667 {
		t.Fatalf("snapshot = %+v", p.Reporting)
	}

	past := today.AddDate(0, 0, -3)
	p, _ = f.record(t, RecordInput{ReceivedAt: &past})
	if !p.ReceivedAt.Equal(past) || !p.Reporting.EffectiveDate.Equal(past) {
		t.Fatalf("snapshot must use received_at: %+v", p)
	}

	f.payments.reporting = "EUR"
	p, err = f.record(t, RecordInput{})
	if err != nil || p.Reporting != nil {
		t.Fatalf("missing rate must record without snapshot: %+v %v", p, err)
	}
}

func TestApproveRefundSegregationOfDuties(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	requester, approver := uuid.New(), uuid.New()
	refund := func() *domain.Payment {
		p := &domain.Payment{
			ID: uuid.New(), BookingID: f.booking.ID, Amount: -1000, Currency: "SAR", RecordedBy: requester,
			EventType: domain.EventRefund, Status: domain.StatusPendingApproval, IdempotencyKey: uuid.NewString(),
		}
		_ = f.payments.Insert(ctx, p)
		return p
	}

	p := refund()
	if _, err := f.svc.ApproveRefund(ctx, p.ID, requester, true); appCode(err) != "sod_violation" || !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("want sod_violation, got %v", err)
	}
	if got, err := f.svc.ApproveRefund(ctx, p.ID, approver, true); err != nil || got.Status != domain.StatusApproved {
		t.Fatalf("other approver: %v %v", got, err)
	}
	withdrawn := refund()
	if got, err := f.svc.ApproveRefund(ctx, withdrawn.ID, requester, false); err != nil || got.Status != domain.StatusRejected {
		t.Fatalf("requester may reject: %v %v", got, err)
	}
}

func TestFinancialSummaryShape(t *testing.T) {
	f := newFixture(t)
	f.payments.comp = domain.Components{Items: 95000, Tax: 7000, Fees: 3000, HasLines: true}
	f.booking.TaxAmt, f.booking.FeeAmt = 7000, 3000
	if _, err := f.record(t, RecordInput{AutoVerify: true, MayApprove: true, Amount: 40000}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.record(t, RecordInput{Amount: 10000}); err != nil {
		t.Fatal(err)
	}
	f.payments.snapshot = &domain.ReportingSnapshot{Currency: "USD", RateScaled: 25000000, EffectiveDate: today.AddDate(0, 0, -30)}

	sum, err := f.svc.FinancialSummary(context.Background(), f.booking.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Subtotal != 95000 || sum.Discount != 5000 || sum.Tax != 7000 || sum.Fees != 3000 || sum.Total != 100000 {
		t.Fatalf("components = %+v", sum)
	}
	if sum.Collected != 40000 || sum.Pending != 10000 || sum.Balance != 60000 {
		t.Fatalf("money = %+v", sum)
	}
	if sum.Margin != 100000-7000-3000-60000 || sum.Cost != 60000 {
		t.Fatalf("margin = %d cost = %d", sum.Margin, sum.Cost)
	}
	r := sum.Reporting
	if r == nil || r.Total != 25000 || r.Collected != 10000 || r.Balance != 15000 || r.RateScaled != 25000000 {
		t.Fatalf("reporting must use the confirmation snapshot: %+v", r)
	}

	f.payments.snapshot = nil
	f.payments.reporting = "EUR"
	if sum, _ = f.svc.FinancialSummary(context.Background(), f.booking.ID); sum.Reporting != nil {
		t.Fatalf("no rate must yield null reporting, got %+v", sum.Reporting)
	}
}

func TestSnapshotBookingFXOnConfirm(t *testing.T) {
	f := newFixture(t)
	bus := events.NewBus(slog.Default())
	f.svc.RegisterReactors(bus)
	bus.Publish(context.Background(), events.Event{Name: events.BookingConfirmed, Payload: f.booking})
	s := f.payments.snapshot
	if s == nil || s.Currency != "USD" || s.Amount != 26667 || !s.EffectiveDate.Equal(today) {
		t.Fatalf("booking snapshot = %+v", s)
	}
}

func TestMarkOverdueSchedulesIsIdempotentAndAudited(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	for i := 0; i < 5; i++ {
		f.payments.schedules = append(f.payments.schedules, domain.Schedule{
			ID: uuid.New(), BookingID: f.booking.ID, DueAt: now.Add(-time.Hour), Status: domain.ScheduleOpen,
		})
	}
	f.payments.schedules = append(f.payments.schedules, domain.Schedule{
		ID: uuid.New(), BookingID: f.booking.ID, DueAt: now.Add(time.Hour), Status: domain.ScheduleOpen,
	})
	n, err := f.svc.MarkOverdueSchedules(context.Background(), 2)
	if err != nil || n != 5 {
		t.Fatalf("marked %d, %v", n, err)
	}
	if len(f.audit.entries) != 5 || f.audit.entries[0].Action != "payment.schedule_overdue" || f.audit.entries[0].ActorID != uuid.Nil {
		t.Fatalf("audit = %v", f.audit.actions())
	}
	if n, _ = f.svc.MarkOverdueSchedules(context.Background(), 2); n != 0 {
		t.Fatalf("second run marked %d", n)
	}
}

func TestExportAuditsFailClosed(t *testing.T) {
	f := newFixture(t)
	ctx := access.WithScope(context.Background(), access.System())
	if _, err := f.record(t, RecordInput{Note: "=HYPERLINK(1)"}); err != nil {
		t.Fatal(err)
	}
	before := len(f.audit.entries)
	out, err := f.svc.ExportQueueCSV(ctx, nil, domain.QueueUnverified)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte(shared.CSVBOM)) || !bytes.Contains(out, []byte("'=HYPERLINK(1)")) {
		t.Fatalf("csv = %q", out)
	}
	e := f.audit.entries[before]
	if e.Action != "finance.exported" || e.Extra["rows"] != 1 || e.Extra["format"] != "csv" {
		t.Fatalf("audit = %+v", e)
	}
	if filters, _ := e.Extra["filters"].(map[string]any); filters["kind"] != domain.QueueUnverified {
		t.Fatalf("filters = %+v", e.Extra["filters"])
	}

	f.audit.fail = errors.New("audit down")
	if out, err := f.svc.ExportQueueCSV(ctx, nil, domain.QueueUnverified); err == nil || out != nil {
		t.Fatalf("export must fail closed, got %d bytes, %v", len(out), err)
	}
}
