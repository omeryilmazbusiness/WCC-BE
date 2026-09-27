package payment

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type EventType string

const (
	EventCharge  EventType = "charge"
	EventReverse EventType = "reverse"
	EventAdjust  EventType = "adjust"
	EventRefund  EventType = "refund"
)

type Status string

const (
	StatusUnverified      Status = "unverified"
	StatusVerified        Status = "verified"
	StatusPendingApproval Status = "pending_approval"
	StatusApproved        Status = "approved"
	StatusRejected        Status = "rejected"
)

// Payment is an immutable ledger entry (append-only). Never update amount/type; only status transitions.
type Payment struct {
	ID                uuid.UUID
	BookingID         uuid.UUID
	Amount            int64 // signed: charge/adjust+ >0, reverse/refund <0
	Currency          string
	Method            string
	Reference         string
	RecordedBy        uuid.UUID
	IdempotencyKey    string
	EventType         EventType
	ReversesPaymentID *uuid.UUID
	Status            Status
	ApprovedBy        *uuid.UUID
	ApprovedAt        *time.Time
	Note              string
	ReceivedAt        time.Time // calendar date the money was received
	Reporting         *ReportingSnapshot
	CreatedAt         time.Time
}

// ReportingSnapshot freezes an amount in the branch reporting currency with
// the rate that produced it. Nil means no rate was available (fx_missing).
type ReportingSnapshot struct {
	Currency      string
	Amount        int64
	RateScaled    int64
	EffectiveDate time.Time
}

func NewReportingSnapshot(currency string, c fx.Conversion) *ReportingSnapshot {
	return &ReportingSnapshot{
		Currency: currency, Amount: c.Amount, RateScaled: c.Rate.Scaled, EffectiveDate: c.Rate.EffectiveDate,
	}
}

func (p Payment) CountsTowardCollected() bool {
	switch p.Status {
	case StatusVerified, StatusApproved:
		return true
	default:
		return false
	}
}

type ScheduleStatus string

const (
	ScheduleOpen      ScheduleStatus = "open"
	SchedulePaid      ScheduleStatus = "paid"
	ScheduleCancelled ScheduleStatus = "cancelled"
	ScheduleOverdue   ScheduleStatus = "overdue"
)

type Schedule struct {
	ID             uuid.UUID
	BookingID      uuid.UUID
	BranchID       uuid.UUID // from the booking; only set by MarkSchedulesOverdue
	DueAt          time.Time
	Amount         int64
	Currency       string
	Label          string
	Status         ScheduleStatus
	ReminderSentAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Components are a booking's line-item totals by kind (item, tax, fee).
type Components struct {
	Items    int64
	Tax      int64
	Fees     int64
	HasLines bool
}

// FinancialSummary is the booking money breakdown (T-117 / T-274). Balance is
// Total - Collected; a negative balance is customer credit.
type FinancialSummary struct {
	BookingID uuid.UUID
	Currency  string
	Subtotal  int64
	Discount  int64
	Tax       int64
	Fees      int64
	Total     int64
	Cost      int64
	Margin    int64
	Collected int64
	Pending   int64 // unverified charges
	Balance   int64
	Reporting *ReportingSummary
	Promises  PromiseSummary
}

// ReportingSummary restates the booking in reporting currency at one rate:
// the confirmation snapshot when present, otherwise the latest rate today.
type ReportingSummary struct {
	Currency      string
	Total         int64
	Collected     int64
	Balance       int64
	RateScaled    int64
	EffectiveDate time.Time
}

// Subtotal is the pre-discount item total. Bookings without line items fall
// back to total + discount - tax - fees so the breakdown still adds up.
func (c Components) Subtotal(total, discount int64) int64 {
	if !c.HasLines {
		return total + discount - c.Tax - c.Fees
	}
	return c.Items
}

type QueueKind string

const (
	QueueOverdue    QueueKind = "overdue"
	QueueUnverified QueueKind = "unverified"
	QueueRefunds    QueueKind = "refunds"
	QueueCredit     QueueKind = "credit"
)

type QueueItem struct {
	Kind         QueueKind  `json:"kind"`
	BookingID    uuid.UUID  `json:"booking_id"`
	BookingRef   string     `json:"booking_ref"`
	CustomerName string     `json:"customer_name"`
	PaymentID    *uuid.UUID `json:"payment_id,omitempty"`
	ScheduleID   *uuid.UUID `json:"schedule_id,omitempty"`
	Amount       int64      `json:"amount"`
	Currency     string     `json:"currency"`
	DueAt        *time.Time `json:"due_at,omitempty"`
	Status       string     `json:"status"`
	Note         string     `json:"note,omitempty"`
}

const (
	// JobPromisesCheck resolves open payment promises (kept / broken).
	JobPromisesCheck shared.JobName = "finance.promises_check"
	// JobSchedulesOverdue marks open schedules past due_at as overdue.
	JobSchedulesOverdue shared.JobName = "finance.schedules_overdue"
)

type Repository interface {
	Insert(ctx context.Context, p *Payment) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status Status, approvedBy *uuid.UUID, approvedAt *time.Time) error
	Get(ctx context.Context, id uuid.UUID) (*Payment, error)
	FindByIdempotencyKey(ctx context.Context, key string) (*Payment, error)
	SumCollectedByBooking(ctx context.Context, bookingID uuid.UUID) (int64, error)
	SumByBookingStatus(ctx context.Context, bookingID uuid.UUID, status Status) (int64, error)
	ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]Payment, error)
	ListByStatus(ctx context.Context, branchID *uuid.UUID, status Status, eventType *EventType, limit int) ([]Payment, error)

	UpsertSchedule(ctx context.Context, s *Schedule) error
	ListSchedules(ctx context.Context, bookingID uuid.UUID) ([]Schedule, error)
	GetSchedule(ctx context.Context, id uuid.UUID) (*Schedule, error)
	UpdateScheduleStatus(ctx context.Context, id uuid.UUID, status ScheduleStatus) error
	ListOverdueSchedules(ctx context.Context, branchID *uuid.UUID, now time.Time, limit int) ([]Schedule, error)
	// MarkSchedulesOverdue flips up to limit open schedules with due_at < now
	// to overdue and returns them with BranchID set.
	MarkSchedulesOverdue(ctx context.Context, now time.Time, limit int) ([]Schedule, error)
	ListDueForReminder(ctx context.Context, now time.Time, within time.Duration, limit int) ([]Schedule, error)
	MarkReminderSent(ctx context.Context, id uuid.UUID, at time.Time) error

	ListCreditBookings(ctx context.Context, branchID *uuid.UUID, limit int) ([]QueueItem, error)
	GetFinanceSettings(ctx context.Context, branchID uuid.UUID) (reportingCurrency string, err error)
	UpsertFinanceSettings(ctx context.Context, branchID uuid.UUID, reportingCurrency string) error
}

// BookingFinance reads booking money components and stores the booking's
// reporting snapshot (ISP: the booking module owns everything else).
type BookingFinance interface {
	Components(ctx context.Context, bookingID uuid.UUID) (Components, error)
	ReportingSnapshot(ctx context.Context, bookingID uuid.UUID) (*ReportingSnapshot, error)
	SetReportingSnapshot(ctx context.Context, bookingID uuid.UUID, s ReportingSnapshot) error
}
