package payment

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
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
	AutoVerify     bool // managers may auto-verify
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

type Service struct {
	payments domain.Repository
	bookings bookingdomain.Repository
	tx       *tx.Manager
	bus      *events.Bus
	audit    audit.Recorder
	fx       domain.ReportingCurrencyPolicy
	tasks    PaymentDueTaskCreator
}

func NewService(
	payments domain.Repository,
	bookings bookingdomain.Repository,
	txm *tx.Manager,
	bus *events.Bus,
) *Service {
	return &Service{
		payments: payments, bookings: bookings, tx: txm, bus: bus,
		fx: domain.IdentityFX{DefaultCurrency: "SAR"},
	}
}

func (s *Service) SetAuditor(a audit.Recorder)                 { s.audit = a }
func (s *Service) SetFX(p domain.ReportingCurrencyPolicy)      { s.fx = p }
func (s *Service) SetTaskCreator(t PaymentDueTaskCreator)      { s.tasks = t }

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
		if b.Status == bookingdomain.StatusCancelled {
			return shared.NewInvalidState("cannot record payment on cancelled booking")
		}
		currency := in.Currency
		if currency == "" {
			currency = b.Currency
		}
		status := domain.StatusUnverified
		if in.AutoVerify {
			status = domain.StatusVerified
		}
		p := &domain.Payment{
			ID: uuid.New(), BookingID: in.BookingID, Amount: in.Amount, Currency: currency,
			Method: in.Method, Reference: in.Reference, RecordedBy: in.RecordedBy,
			IdempotencyKey: in.IdempotencyKey, EventType: domain.EventCharge, Status: status,
			Note: in.Note, CreatedAt: time.Now().UTC(),
		}
		if err := s.payments.Insert(ctx, p); err != nil {
			return err
		}
		if err := s.recomputeBooking(ctx, b); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.auditPayment(ctx, in.RecordedBy, "payment.recorded", out)
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
		if p.Status != domain.StatusUnverified {
			return shared.NewInvalidState("only unverified payments can be verified")
		}
		if err := s.payments.UpdateStatus(ctx, paymentID, domain.StatusVerified, nil, nil); err != nil {
			return err
		}
		p.Status = domain.StatusVerified
		b, err := s.bookings.FindByID(ctx, p.BookingID)
		if err != nil {
			return err
		}
		if err := s.recomputeBooking(ctx, b); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.auditPayment(ctx, actorID, "payment.verified", out)
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
			CreatedAt: time.Now().UTC(),
		}
		if err := s.payments.Insert(ctx, p); err != nil {
			return err
		}
		b, err := s.bookings.FindByID(ctx, orig.BookingID)
		if err != nil {
			return err
		}
		if err := s.recomputeBooking(ctx, b); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.auditPayment(ctx, in.ActorID, "payment.reversed", out)
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
		p := &domain.Payment{
			ID: uuid.New(), BookingID: in.BookingID, Amount: in.Amount, Currency: cur,
			RecordedBy: in.ActorID, IdempotencyKey: in.IdempotencyKey,
			EventType: domain.EventAdjust, Status: domain.StatusVerified, Note: in.Note,
			CreatedAt: time.Now().UTC(),
		}
		if err := s.payments.Insert(ctx, p); err != nil {
			return err
		}
		if err := s.recomputeBooking(ctx, b); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.auditPayment(ctx, in.ActorID, "payment.adjusted", out)
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
		p := &domain.Payment{
			ID: uuid.New(), BookingID: in.BookingID, Amount: -in.Amount, Currency: cur,
			RecordedBy: in.ActorID, IdempotencyKey: in.IdempotencyKey,
			EventType: domain.EventRefund, Status: domain.StatusPendingApproval, Note: in.Note,
			CreatedAt: time.Now().UTC(),
		}
		if err := s.payments.Insert(ctx, p); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.auditPayment(ctx, in.ActorID, "payment.refund_requested", out)
	return out, nil
}

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
		now := time.Now().UTC()
		st := domain.StatusRejected
		if approve {
			st = domain.StatusApproved
		}
		if err := s.payments.UpdateStatus(ctx, paymentID, st, &actorID, &now); err != nil {
			return err
		}
		p.Status = st
		p.ApprovedBy = &actorID
		p.ApprovedAt = &now
		if approve {
			b, err := s.bookings.FindByID(ctx, p.BookingID)
			if err != nil {
				return err
			}
			if err := s.recomputeBooking(ctx, b); err != nil {
				return err
			}
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	action := "payment.refund_rejected"
	if approve {
		action = "payment.refund_approved"
	}
	s.auditPayment(ctx, actorID, action, out)
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
	unverified, _ := s.payments.SumByBookingStatus(ctx, bookingID, domain.StatusUnverified)
	pending, _ := s.payments.SumByBookingStatus(ctx, bookingID, domain.StatusPendingApproval)
	schedules, _ := s.payments.ListSchedules(ctx, bookingID)
	var openSched int64
	for _, sc := range schedules {
		if sc.Status == domain.ScheduleOpen || sc.Status == domain.ScheduleOverdue {
			openSched += sc.Amount
		}
	}
	balance := b.TotalAmount - collected
	credit := int64(0)
	if balance < 0 {
		credit = -balance
		balance = 0
	}
	recognized := int64(0)
	if b.Status == bookingdomain.StatusConfirmed || b.Status == bookingdomain.StatusCompleted {
		recognized = collected
		if recognized < 0 {
			recognized = 0
		}
	}
	repCur, _ := s.fx.ReportingCurrency(ctx, b.BranchID)
	bookedR, _, _ := s.fx.Convert(ctx, b.BranchID, b.TotalAmount, b.Currency)
	collR, _, _ := s.fx.Convert(ctx, b.BranchID, collected, b.Currency)
	recR, _, _ := s.fx.Convert(ctx, b.BranchID, recognized, b.Currency)
	marR, _, _ := s.fx.Convert(ctx, b.BranchID, b.Margin(), b.Currency)
	balR, _, _ := s.fx.Convert(ctx, b.BranchID, balance, b.Currency)
	credR, _, _ := s.fx.Convert(ctx, b.BranchID, credit, b.Currency)
	return &domain.FinancialSummary{
		BookingID: bookingID, Currency: b.Currency, ReportingCurrency: repCur,
		Booked: bookedR, Collected: collR, Recognized: recR, Margin: marR,
		Balance: balR, Credit: credR, UnverifiedAmt: unverified, PendingRefundAmt: pending,
		ScheduleOpenAmt: openSched,
	}, nil
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
	if err := s.payments.UpsertSchedule(ctx, sc); err != nil {
		return nil, err
	}
	if s.audit != nil {
		id := sc.ID
		_ = s.audit.Record(ctx, audit.RecordInput{
			ActorID: in.ActorID, Action: "payment.schedule_created", EntityType: "payment_schedule", EntityID: &id,
			After: map[string]any{"booking_id": in.BookingID, "amount": sc.Amount, "due_at": sc.DueAt},
		})
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
	if err := s.payments.UpdateScheduleStatus(ctx, id, domain.ScheduleCancelled); err != nil {
		return nil, err
	}
	sc.Status = domain.ScheduleCancelled
	return sc, nil
}

func (s *Service) FinanceQueue(ctx context.Context, branchID uuid.UUID, kind domain.QueueKind, limit int) ([]domain.QueueItem, error) {
	if limit <= 0 {
		limit = 50
	}
	now := time.Now().UTC()
	switch kind {
	case domain.QueueOverdue:
		items, err := s.payments.ListOverdueSchedules(ctx, branchID, now, limit)
		if err != nil {
			return nil, err
		}
		out := make([]domain.QueueItem, 0, len(items))
		for i := range items {
			sc := items[i]
			id := sc.ID
			due := sc.DueAt
			if sc.Status == domain.ScheduleOpen {
				_ = s.payments.UpdateScheduleStatus(ctx, sc.ID, domain.ScheduleOverdue)
				sc.Status = domain.ScheduleOverdue
			}
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

func (s *Service) ExportQueueCSV(ctx context.Context, branchID uuid.UUID, kind domain.QueueKind) (string, error) {
	items, err := s.FinanceQueue(ctx, branchID, kind, 500)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("kind,booking_id,booking_ref,customer_name,amount,currency,status,due_at,note\n")
	for _, it := range items {
		due := ""
		if it.DueAt != nil {
			due = it.DueAt.UTC().Format(time.RFC3339)
		}
		b.WriteString(fmt.Sprintf("%s,%s,%s,%s,%d,%s,%s,%s,%s\n",
			it.Kind, it.BookingID, csvEscape(it.BookingRef), csvEscape(it.CustomerName),
			it.Amount, it.Currency, it.Status, due, csvEscape(it.Note)))
	}
	return b.String(), nil
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
			_ = s.tasks.EnsurePaymentDueTask(ctx, b.BranchID, sc.BookingID, b.OwnerID, sc.DueAt, sc.Amount, sc.Currency)
		}
		if err := s.payments.MarkReminderSent(ctx, sc.ID, now); err != nil {
			continue
		}
		n++
	}
	return n, nil
}

func (s *Service) SetReportingCurrency(ctx context.Context, branchID uuid.UUID, currency string, actorID uuid.UUID) error {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return shared.NewValidation("currency must be ISO-4217 (3 letters)")
	}
	if err := s.payments.UpsertFinanceSettings(ctx, branchID, currency); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.RecordInput{
			ActorID: actorID, Action: "finance.reporting_currency_set", EntityType: "finance_settings",
			After: map[string]any{"branch_id": branchID, "reporting_currency": currency},
		})
	}
	return nil
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

func (s *Service) auditPayment(ctx context.Context, actorID uuid.UUID, action string, p *domain.Payment) {
	if s.audit == nil || p == nil {
		return
	}
	id := p.ID
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: actorID, Action: action, EntityType: "payment", EntityID: &id,
		After: map[string]any{
			"booking_id": p.BookingID, "amount": p.Amount, "currency": p.Currency,
			"event_type": p.EventType, "status": p.Status,
		},
	})
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

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
