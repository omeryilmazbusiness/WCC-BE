package payment

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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

type RecordInput struct {
	BookingID      uuid.UUID
	Amount         int64
	Currency       string
	Method         string
	Reference      string
	Note           string
	RecordedBy     uuid.UUID
	IdempotencyKey string
	AutoVerify     bool
	// MayApprove is true when the caller holds payments.approve.
	MayApprove bool
	// ReceivedAt is the calendar date the money arrived (today when nil).
	ReceivedAt *time.Time
}

type ReverseInput struct {
	PaymentID      uuid.UUID
	ActorID        uuid.UUID
	Note           string
	IdempotencyKey string
}

type AdjustInput struct {
	BookingID      uuid.UUID
	Amount         int64 // signed nonzero
	Currency       string
	Note           string
	ActorID        uuid.UUID
	IdempotencyKey string
}

type RefundInput struct {
	BookingID      uuid.UUID
	Amount         int64 // positive; stored negative
	Currency       string
	Note           string
	ActorID        uuid.UUID
	IdempotencyKey string
}

type ScheduleInput struct {
	BookingID uuid.UUID
	DueAt     time.Time
	Amount    int64
	Currency  string
	Label     string
	ActorID   uuid.UUID
}

// PaymentDueTaskCreator creates reminder tasks (T-123) — ISP.
type PaymentDueTaskCreator interface {
	EnsurePaymentDueTask(ctx context.Context, branchID, bookingID, actorID uuid.UUID, dueAt time.Time, amount int64, currency string) error
}

// PromiseSummarizer aggregates a booking's open payment promises.
type PromiseSummarizer interface {
	Summary(ctx context.Context, bookingID uuid.UUID) (domain.PromiseSummary, error)
}

// FinanceDeps are the Epic 21 collaborators. Without a Converter no
// reporting snapshot is taken (every entry is fx_missing).
type FinanceDeps struct {
	Converter fx.Converter
	Bookings  domain.BookingFinance
	Promises  PromiseSummarizer
	Clock     Clock
	Log       *slog.Logger
}

type Service struct {
	payments  domain.Repository
	bookings  bookingdomain.Repository
	tx        tx.Runner
	bus       *events.Bus
	outbox    events.Outbox
	audit     audit.Recorder
	tasks     PaymentDueTaskCreator
	dueAlerts PaymentDueAlerts
	fin       FinanceDeps
}

// PaymentDueAlerts tells the booking owner a scheduled payment is due (DIP).
type PaymentDueAlerts interface {
	PaymentDue(ctx context.Context, branchID, bookingID, ownerID uuid.UUID, dueAt time.Time, amount int64, currency string) error
}

// SetOutbox enables durable payment.reversed events.
func (s *Service) SetOutbox(o events.Outbox) { s.outbox = o }

func (s *Service) SetDueAlerts(a PaymentDueAlerts) { s.dueAlerts = a }

func NewService(
	payments domain.Repository,
	bookings bookingdomain.Repository,
	txm tx.Runner,
	bus *events.Bus,
) *Service {
	return &Service{
		payments: payments, bookings: bookings, tx: txm, bus: bus,
		fin: FinanceDeps{Log: slog.Default()},
	}
}

func (s *Service) SetAuditor(a audit.Recorder)            { s.audit = a }
func (s *Service) SetTaskCreator(t PaymentDueTaskCreator) { s.tasks = t }
func (s *Service) SetFinance(d FinanceDeps) {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	s.fin = d
}

func (s *Service) Record(ctx context.Context, in RecordInput) (*domain.Payment, error) {
	if in.Amount <= 0 {
		return nil, shared.NewValidation("amount must be > 0")
	}
	if in.IdempotencyKey == "" {
		return nil, shared.NewValidation("idempotency_key is required")
	}
	if in.BookingID == uuid.Nil {
		return nil, shared.NewValidation("booking_id is required")
	}
	status, err := domain.InitialChargeStatus(in.AutoVerify, in.MayApprove)
	if err != nil {
		return nil, err
	}
	receivedAt, err := domain.ResolveReceivedAt(in.ReceivedAt, s.fin.Clock.Today())
	if err != nil {
		return nil, err
	}
	if in.Currency != "" {
		if in.Currency, err = fx.NormalizeCurrency(in.Currency); err != nil {
			return nil, err
		}
	}
	if existing, err := s.payments.FindByIdempotencyKey(ctx, in.IdempotencyKey); err == nil && existing != nil {
		return existing, nil
	}

	var out *domain.Payment
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if existing, err := s.payments.FindByIdempotencyKey(ctx, in.IdempotencyKey); err == nil && existing != nil {
			out = existing
			return nil
		}
		b, err := s.bookings.FindByID(ctx, in.BookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if b.Status == bookingdomain.StatusCancelled {
			return shared.NewInvalidState("cannot record payment on cancelled booking")
		}
		currency := in.Currency
		if currency == "" {
			currency = b.Currency
		}
		p := &domain.Payment{
			ID: uuid.New(), BookingID: in.BookingID, Amount: in.Amount, Currency: currency,
			Method: in.Method, Reference: in.Reference, RecordedBy: in.RecordedBy,
			IdempotencyKey: in.IdempotencyKey, EventType: domain.EventCharge, Status: status,
			Note: in.Note, ReceivedAt: receivedAt, CreatedAt: time.Now().UTC(),
		}
		if p.Reporting, err = s.reportingSnapshot(ctx, b.BranchID, p.Amount, p.Currency, receivedAt); err != nil {
			return err
		}
		if err := s.payments.Insert(ctx, p); err != nil {
			return err
		}
		money := bookingMoney(b)
		if err := s.recomputeBooking(ctx, b); err != nil {
			return err
		}
		if err := s.recordPayment(ctx, in.RecordedBy, "payment.recorded", p, b, nil, money, nil); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.bus.Publish(ctx, events.Event{Name: events.PaymentRecorded, Payload: out})
	return out, nil
}

func (s *Service) Verify(ctx context.Context, paymentID, actorID uuid.UUID) (*domain.Payment, error) {
	var out *domain.Payment
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		p, err := s.payments.Get(ctx, paymentID)
		if err != nil || p == nil {
			return shared.NewNotFound("payment")
		}
		before := paymentSnapshot(p)
		if err := p.TransitionTo(domain.StatusVerified); err != nil {
			return err
		}
		if err := s.payments.UpdateStatus(ctx, paymentID, p.Status, nil, nil); err != nil {
			return err
		}
		b, err := s.bookings.FindByID(ctx, p.BookingID)
		if err != nil {
			return err
		}
		money := bookingMoney(b)
		if err := s.recomputeBooking(ctx, b); err != nil {
			return err
		}
		if err := s.recordPayment(ctx, actorID, "payment.verified", p, b, before, money, nil); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) Reverse(ctx context.Context, in ReverseInput) (*domain.Payment, error) {
	if in.IdempotencyKey == "" {
		return nil, shared.NewValidation("idempotency_key is required")
	}
	if existing, err := s.payments.FindByIdempotencyKey(ctx, in.IdempotencyKey); err == nil && existing != nil {
		return existing, nil
	}
	var out *domain.Payment
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if existing, err := s.payments.FindByIdempotencyKey(ctx, in.IdempotencyKey); err == nil && existing != nil {
			out = existing
			return nil
		}
		orig, err := s.payments.Get(ctx, in.PaymentID)
		if err != nil || orig == nil {
			return shared.NewNotFound("payment")
		}
		if orig.EventType != domain.EventCharge && orig.EventType != domain.EventAdjust {
			return shared.NewInvalidState("only charge/adjust entries can be reversed")
		}
		if orig.Amount <= 0 {
			return shared.NewInvalidState("cannot reverse a non-positive entry")
		}
		revID := orig.ID
		p := &domain.Payment{
			ID: uuid.New(), BookingID: orig.BookingID, Amount: -orig.Amount, Currency: orig.Currency,
			Method: orig.Method, Reference: orig.Reference, RecordedBy: in.ActorID,
			IdempotencyKey: in.IdempotencyKey, EventType: domain.EventReverse,
			ReversesPaymentID: &revID, Status: domain.StatusVerified, Note: in.Note,
			ReceivedAt: s.fin.Clock.Today(), Reporting: negated(orig.Reporting), CreatedAt: time.Now().UTC(),
		}
		if err := s.payments.Insert(ctx, p); err != nil {
			return err
		}
		b, err := s.bookings.FindByID(ctx, orig.BookingID)
		if err != nil {
			return err
		}
		money := bookingMoney(b)
		if err := s.recomputeBooking(ctx, b); err != nil {
			return err
		}
		if err := s.recordPayment(ctx, in.ActorID, "payment.reversed", p, b, nil, money,
			map[string]any{"reversed_entry": paymentSnapshot(orig)}); err != nil {
			return err
		}
		if err := events.Record(ctx, s.outbox, events.Event{Name: events.PaymentReversed, Payload: events.PaymentReversedPayload{
			PaymentID: orig.ID, ReversalID: p.ID, BookingID: b.ID, BranchID: b.BranchID, Amount: orig.Amount, Currency: orig.Currency,
		}}); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) Adjust(ctx context.Context, in AdjustInput) (*domain.Payment, error) {
	if in.Amount == 0 {
		return nil, shared.NewValidation("amount must be nonzero")
	}
	if in.IdempotencyKey == "" {
		return nil, shared.NewValidation("idempotency_key is required")
	}
	if existing, err := s.payments.FindByIdempotencyKey(ctx, in.IdempotencyKey); err == nil && existing != nil {
		return existing, nil
	}
	var out *domain.Payment
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if existing, err := s.payments.FindByIdempotencyKey(ctx, in.IdempotencyKey); err == nil && existing != nil {
			out = existing
			return nil
		}
		b, err := s.bookings.FindByID(ctx, in.BookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		cur := in.Currency
		if cur == "" {
			cur = b.Currency
		}
		today := s.fin.Clock.Today()
		p := &domain.Payment{
			ID: uuid.New(), BookingID: in.BookingID, Amount: in.Amount, Currency: cur,
			RecordedBy: in.ActorID, IdempotencyKey: in.IdempotencyKey,
			EventType: domain.EventAdjust, Status: domain.StatusVerified, Note: in.Note,
			ReceivedAt: today, CreatedAt: time.Now().UTC(),
		}
		if p.Reporting, err = s.reportingSnapshot(ctx, b.BranchID, p.Amount, p.Currency, today); err != nil {
			return err
		}
		if err := s.payments.Insert(ctx, p); err != nil {
			return err
		}
		money := bookingMoney(b)
		if err := s.recomputeBooking(ctx, b); err != nil {
			return err
		}
		if err := s.recordPayment(ctx, in.ActorID, "payment.adjusted", p, b, nil, money, nil); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) RequestRefund(ctx context.Context, in RefundInput) (*domain.Payment, error) {
	if in.Amount <= 0 {
		return nil, shared.NewValidation("refund amount must be > 0")
	}
	if in.IdempotencyKey == "" {
		return nil, shared.NewValidation("idempotency_key is required")
	}
	if existing, err := s.payments.FindByIdempotencyKey(ctx, in.IdempotencyKey); err == nil && existing != nil {
		return existing, nil
	}
	var out *domain.Payment
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if existing, err := s.payments.FindByIdempotencyKey(ctx, in.IdempotencyKey); err == nil && existing != nil {
			out = existing
			return nil
		}
		b, err := s.bookings.FindByID(ctx, in.BookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		cur := in.Currency
		if cur == "" {
			cur = b.Currency
		}
		today := s.fin.Clock.Today()
		p := &domain.Payment{
			ID: uuid.New(), BookingID: in.BookingID, Amount: -in.Amount, Currency: cur,
			RecordedBy: in.ActorID, IdempotencyKey: in.IdempotencyKey,
			EventType: domain.EventRefund, Status: domain.StatusPendingApproval, Note: in.Note,
			ReceivedAt: today, CreatedAt: time.Now().UTC(),
		}
		if p.Reporting, err = s.reportingSnapshot(ctx, b.BranchID, p.Amount, p.Currency, today); err != nil {
			return err
		}
		if err := s.payments.Insert(ctx, p); err != nil {
			return err
		}
		if err := s.recordPayment(ctx, in.ActorID, "payment.refund_requested", p, b, nil, nil, nil); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ApproveRefund approves or rejects a pending refund. Approval is subject to
// segregation of duties; the requester may still reject (withdraw) it.
func (s *Service) ApproveRefund(ctx context.Context, paymentID, actorID uuid.UUID, approve bool) (*domain.Payment, error) {
	var out *domain.Payment
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		p, err := s.payments.Get(ctx, paymentID)
		if err != nil || p == nil {
			return shared.NewNotFound("payment")
		}
		if p.EventType != domain.EventRefund || p.Status != domain.StatusPendingApproval {
			return shared.NewInvalidState("not a pending refund")
		}
		if approve {
			if err := domain.CheckRefundApprover(p, actorID); err != nil {
				return err
			}
		}
		before := paymentSnapshot(p)
		now := time.Now().UTC()
		st, action := domain.StatusRejected, "payment.refund_rejected"
		if approve {
			st, action = domain.StatusApproved, "payment.refund_approved"
		}
		if err := p.TransitionTo(st); err != nil {
			return err
		}
		if err := s.payments.UpdateStatus(ctx, paymentID, st, &actorID, &now); err != nil {
			return err
		}
		p.ApprovedBy = &actorID
		p.ApprovedAt = &now
		b, err := s.bookings.FindByID(ctx, p.BookingID)
		if err != nil {
			return err
		}
		var money map[string]any
		if approve {
			money = bookingMoney(b)
			if err := s.recomputeBooking(ctx, b); err != nil {
				return err
			}
		}
		if err := s.recordPayment(ctx, actorID, action, p, b, before, money, nil); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]domain.Payment, error) {
	if _, err := s.bookings.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	return s.payments.ListByBooking(ctx, bookingID)
}

func (s *Service) FinancialSummary(ctx context.Context, bookingID uuid.UUID) (*domain.FinancialSummary, error) {
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil {
		return nil, shared.NewNotFound("booking")
	}
	collected, err := s.payments.SumCollectedByBooking(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	pending, err := s.payments.SumByBookingStatus(ctx, bookingID, domain.StatusUnverified)
	if err != nil {
		return nil, err
	}
	var comp domain.Components
	if s.fin.Bookings != nil {
		if comp, err = s.fin.Bookings.Components(ctx, bookingID); err != nil {
			return nil, err
		}
	}
	sum := &domain.FinancialSummary{
		BookingID: bookingID, Currency: b.Currency,
		Subtotal: comp.Subtotal(b.TotalAmount, b.DiscountAmt), Discount: b.DiscountAmt,
		Tax: comp.Tax, Fees: comp.Fees, Total: b.TotalAmount,
		Cost: b.CostAmt, Margin: b.Margin(),
		Collected: collected, Pending: pending, Balance: b.TotalAmount - collected,
	}
	if sum.Reporting, err = s.reportingSummary(ctx, b, collected); err != nil {
		return nil, err
	}
	if s.fin.Promises != nil {
		if sum.Promises, err = s.fin.Promises.Summary(ctx, bookingID); err != nil {
			return nil, err
		}
	}
	return sum, nil
}

// reportingSummary uses the confirmation snapshot rate when the booking has
// one, otherwise today's rate; nil when no rate exists.
func (s *Service) reportingSummary(ctx context.Context, b *bookingdomain.Booking, collected int64) (*domain.ReportingSummary, error) {
	var snap *domain.ReportingSnapshot
	var err error
	if s.fin.Bookings != nil {
		if snap, err = s.fin.Bookings.ReportingSnapshot(ctx, b.ID); err != nil {
			return nil, err
		}
	}
	if snap == nil {
		if snap, err = s.reportingSnapshot(ctx, b.BranchID, b.TotalAmount, b.Currency, s.fin.Clock.Today()); err != nil || snap == nil {
			return nil, err
		}
	}
	total, errT := fx.Apply(b.TotalAmount, snap.RateScaled)
	coll, errC := fx.Apply(collected, snap.RateScaled)
	if errT != nil || errC != nil {
		return nil, nil
	}
	return &domain.ReportingSummary{
		Currency: snap.Currency, Total: total, Collected: coll, Balance: total - coll,
		RateScaled: snap.RateScaled, EffectiveDate: snap.EffectiveDate,
	}, nil
}

// reportingSnapshot converts amount into the branch reporting currency as of
// on. A missing rate (or an amount/currency the converter rejects) yields nil
// so cash collection is never blocked; storage errors propagate.
func (s *Service) reportingSnapshot(ctx context.Context, branchID uuid.UUID, amount int64, currency string, on time.Time) (*domain.ReportingSnapshot, error) {
	if s.fin.Converter == nil {
		return nil, nil
	}
	rep, err := s.payments.GetFinanceSettings(ctx, branchID)
	if err != nil {
		return nil, err
	}
	c, err := s.fin.Converter.Convert(ctx, amount, currency, rep, on)
	switch {
	case errors.Is(err, fx.ErrRateNotFound), errors.Is(err, fx.ErrOverflow), errors.Is(err, shared.ErrValidation):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return domain.NewReportingSnapshot(c.Rate.Quote, c), nil
}

// RegisterReactors subscribes the booking reporting snapshot to confirmations.
func (s *Service) RegisterReactors(bus *events.Bus) {
	bus.Subscribe(events.BookingConfirmed, s.onBookingConfirmed)
}

func (s *Service) onBookingConfirmed(ctx context.Context, ev events.Event) error {
	b, ok := ev.Payload.(*bookingdomain.Booking)
	if !ok || b == nil {
		return nil
	}
	return s.SnapshotBookingFX(ctx, b)
}

// SnapshotBookingFX freezes the booking total in reporting currency at
// today's rate. Without a rate nothing is stored and the financial summary
// falls back to the live rate.
func (s *Service) SnapshotBookingFX(ctx context.Context, b *bookingdomain.Booking) error {
	if s.fin.Bookings == nil {
		return nil
	}
	snap, err := s.reportingSnapshot(ctx, b.BranchID, b.TotalAmount, b.Currency, s.fin.Clock.Today())
	if err != nil {
		return err
	}
	if snap == nil {
		s.fin.Log.WarnContext(ctx, "booking confirmed without fx rate", "booking_id", b.ID, "currency", b.Currency)
		return nil
	}
	return s.fin.Bookings.SetReportingSnapshot(ctx, b.ID, *snap)
}

func (s *Service) UpsertSchedule(ctx context.Context, in ScheduleInput) (*domain.Schedule, error) {
	if in.Amount <= 0 {
		return nil, shared.NewValidation("amount must be > 0")
	}
	if in.DueAt.IsZero() {
		return nil, shared.NewValidation("due_at is required")
	}
	b, err := s.bookings.FindByID(ctx, in.BookingID)
	if err != nil {
		return nil, shared.NewNotFound("booking")
	}
	cur := in.Currency
	if cur == "" {
		cur = b.Currency
	}
	now := time.Now().UTC()
	sc := &domain.Schedule{
		ID: uuid.New(), BookingID: in.BookingID, DueAt: in.DueAt.UTC(), Amount: in.Amount,
		Currency: cur, Label: strings.TrimSpace(in.Label), Status: domain.ScheduleOpen,
		CreatedAt: now, UpdatedAt: now,
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.payments.UpsertSchedule(ctx, sc); err != nil {
			return err
		}
		return s.record(ctx, in.ActorID, "payment.schedule_created", "payment_schedule", sc.ID, b.BranchID,
			nil, scheduleSnapshot(sc), nil)
	})
	if err != nil {
		return nil, err
	}
	return sc, nil
}

func (s *Service) ListSchedules(ctx context.Context, bookingID uuid.UUID) ([]domain.Schedule, error) {
	if _, err := s.bookings.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	return s.payments.ListSchedules(ctx, bookingID)
}

func (s *Service) CancelSchedule(ctx context.Context, id, actorID uuid.UUID) (*domain.Schedule, error) {
	sc, err := s.payments.GetSchedule(ctx, id)
	if err != nil || sc == nil {
		return nil, shared.NewNotFound("schedule")
	}
	b, err := s.bookings.FindByID(ctx, sc.BookingID)
	if err != nil {
		return nil, shared.NewNotFound("booking")
	}
	before := scheduleSnapshot(sc)
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.payments.UpdateScheduleStatus(ctx, id, domain.ScheduleCancelled); err != nil {
			return err
		}
		sc.Status = domain.ScheduleCancelled
		return s.record(ctx, actorID, "payment.schedule_cancelled", "payment_schedule", sc.ID, b.BranchID,
			before, scheduleSnapshot(sc), nil)
	})
	if err != nil {
		return nil, err
	}
	return sc, nil
}

// MarkOverdueSchedules flips open schedules past due_at to overdue in
// batches (T-277). Each batch and its audit rows commit together; rerunning
// is a no-op once nothing is left.
func (s *Service) MarkOverdueSchedules(ctx context.Context, batch int) (int, error) {
	if batch <= 0 {
		batch = 200
	}
	total := 0
	for {
		var marked []domain.Schedule
		err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
			var err error
			if marked, err = s.payments.MarkSchedulesOverdue(ctx, time.Now().UTC(), batch); err != nil {
				return err
			}
			for i := range marked {
				sc := &marked[i]
				before := scheduleSnapshot(sc)
				before["status"] = domain.ScheduleOpen
				if err := s.record(ctx, uuid.Nil, "payment.schedule_overdue", "payment_schedule", sc.ID, sc.BranchID,
					before, scheduleSnapshot(sc), nil); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return total, err
		}
		total += len(marked)
		if len(marked) < batch {
			return total, nil
		}
	}
}

// FinanceQueue lists queue items visible to the caller; requested nil means
// all branches for global callers and the caller's branch otherwise. It never
// writes: overdue status is maintained by MarkOverdueSchedules.
func (s *Service) FinanceQueue(ctx context.Context, requested *uuid.UUID, kind domain.QueueKind, limit int) ([]domain.QueueItem, error) {
	branchID, err := resolveBranch(ctx, requested)
	if err != nil {
		return nil, err
	}
	return s.queue(ctx, branchID, kind, limit)
}

func (s *Service) queue(ctx context.Context, branchID *uuid.UUID, kind domain.QueueKind, limit int) ([]domain.QueueItem, error) {
	if limit <= 0 {
		limit = 50
	}
	switch kind {
	case domain.QueueOverdue:
		items, err := s.payments.ListOverdueSchedules(ctx, branchID, time.Now().UTC(), limit)
		if err != nil {
			return nil, err
		}
		out := make([]domain.QueueItem, 0, len(items))
		for i := range items {
			sc := items[i]
			id := sc.ID
			due := sc.DueAt
			out = append(out, domain.QueueItem{
				Kind: domain.QueueOverdue, BookingID: sc.BookingID, ScheduleID: &id,
				Amount: sc.Amount, Currency: sc.Currency, DueAt: &due, Status: string(sc.Status),
				Note: sc.Label, BookingRef: shortID(sc.BookingID),
			})
		}
		return out, nil
	case domain.QueueUnverified:
		ps, err := s.payments.ListByStatus(ctx, branchID, domain.StatusUnverified, nil, limit)
		if err != nil {
			return nil, err
		}
		return mapPaymentsQueue(domain.QueueUnverified, ps), nil
	case domain.QueueRefunds:
		et := domain.EventRefund
		ps, err := s.payments.ListByStatus(ctx, branchID, domain.StatusPendingApproval, &et, limit)
		if err != nil {
			return nil, err
		}
		return mapPaymentsQueue(domain.QueueRefunds, ps), nil
	case domain.QueueCredit:
		return s.payments.ListCreditBookings(ctx, branchID, limit)
	default:
		return nil, shared.NewValidation("unknown queue kind")
	}
}

// ExportQueueCSV builds the queue CSV and audits the export (fail closed)
// before any byte is returned to the caller.
func (s *Service) ExportQueueCSV(ctx context.Context, requested *uuid.UUID, kind domain.QueueKind) ([]byte, error) {
	branchID, err := resolveBranch(ctx, requested)
	if err != nil {
		return nil, err
	}
	items, err := s.queue(ctx, branchID, kind, 500)
	if err != nil {
		return nil, err
	}
	out, err := domain.BuildQueueCSV(items)
	if err != nil {
		return nil, err
	}
	filters := map[string]any{"kind": kind}
	if branchID != nil {
		filters["branch_id"] = *branchID
	}
	if s.audit != nil {
		if err := s.audit.Record(ctx, audit.RecordInput{
			Action: "finance.exported", EntityType: "finance_export", BranchID: branchID,
			Extra: map[string]any{"filters": filters, "rows": len(items), "format": "csv"},
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ProcessPaymentDueReminders marks due schedules and creates tasks (T-123).
func (s *Service) ProcessPaymentDueReminders(ctx context.Context, within time.Duration, limit int) (int, error) {
	now := time.Now().UTC()
	items, err := s.payments.ListDueForReminder(ctx, now, within, limit)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range items {
		sc := items[i]
		b, err := s.bookings.FindByID(ctx, sc.BookingID)
		if err != nil {
			continue
		}
		if s.tasks != nil {
			if err := s.tasks.EnsurePaymentDueTask(ctx, b.BranchID, sc.BookingID, b.OwnerID, sc.DueAt, sc.Amount, sc.Currency); err != nil {
				return n, err
			}
		}
		if s.dueAlerts != nil {
			if err := s.dueAlerts.PaymentDue(ctx, b.BranchID, sc.BookingID, b.OwnerID, sc.DueAt, sc.Amount, sc.Currency); err != nil {
				return n, err
			}
		}
		if err := s.payments.MarkReminderSent(ctx, sc.ID, now); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SetReportingCurrency updates branchID's setting (the caller's branch when
// zero); only global callers may target another branch.
func (s *Service) SetReportingCurrency(ctx context.Context, branchID uuid.UUID, currency string, actorID uuid.UUID) error {
	currency, err := fx.NormalizeCurrency(currency)
	if err != nil {
		return err
	}
	scope, err := access.Require(ctx)
	if err != nil {
		return err
	}
	if branchID, err = scope.WriteBranch(branchID); err != nil {
		return err
	}
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var before any
		if prev, err := s.payments.GetFinanceSettings(ctx, branchID); err == nil && prev != "" {
			before = map[string]any{"reporting_currency": prev}
		}
		if err := s.payments.UpsertFinanceSettings(ctx, branchID, currency); err != nil {
			return err
		}
		return s.record(ctx, actorID, "finance.reporting_currency_set", "finance_settings", branchID, branchID,
			before, map[string]any{"reporting_currency": currency}, nil)
	})
}

func (s *Service) recomputeBooking(ctx context.Context, b *bookingdomain.Booking) error {
	sum, err := s.payments.SumCollectedByBooking(ctx, b.ID)
	if err != nil {
		return err
	}
	b.CollectedAmt = sum
	b.RecomputeBalance()
	b.UpdatedAt = time.Now().UTC()
	return s.bookings.Update(ctx, b)
}

func resolveBranch(ctx context.Context, requested *uuid.UUID) (*uuid.UUID, error) {
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, err
	}
	return scope.ResolveBranch(requested)
}

func negated(s *domain.ReportingSnapshot) *domain.ReportingSnapshot {
	if s == nil {
		return nil
	}
	n := *s
	n.Amount = -n.Amount
	return &n
}

func mapPaymentsQueue(kind domain.QueueKind, ps []domain.Payment) []domain.QueueItem {
	out := make([]domain.QueueItem, 0, len(ps))
	for i := range ps {
		p := ps[i]
		id := p.ID
		out = append(out, domain.QueueItem{
			Kind: kind, BookingID: p.BookingID, PaymentID: &id,
			Amount: p.Amount, Currency: p.Currency, Status: string(p.Status),
			Note: p.Note, BookingRef: shortID(p.BookingID),
		})
	}
	return out
}

func shortID(id uuid.UUID) string {
	s := id.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}
