package payment

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	CreatedAt         time.Time
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
	DueAt          time.Time
	Amount         int64
	Currency       string
	Label          string
	Status         ScheduleStatus
	ReminderSentAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// FinancialSummary is the booking money snapshot (T-117 / T-121).
type FinancialSummary struct {
	BookingID          uuid.UUID `json:"booking_id"`
	Currency           string    `json:"currency"`
	ReportingCurrency  string    `json:"reporting_currency"`
	Booked             int64     `json:"booked"`
	Collected          int64     `json:"collected"`
	Recognized         int64     `json:"recognized"`
	Margin             int64     `json:"margin"`
	Balance            int64     `json:"balance"`
	Credit             int64     `json:"credit"`
	UnverifiedAmt      int64     `json:"unverified_amt"`
	PendingRefundAmt   int64     `json:"pending_refund_amt"`
	ScheduleOpenAmt    int64     `json:"schedule_open_amt"`
}

type QueueKind string

const (
	QueueOverdue    QueueKind = "overdue"
	QueueUnverified QueueKind = "unverified"
	QueueRefunds    QueueKind = "refunds"
	QueueCredit     QueueKind = "credit"
)

type QueueItem struct {
	Kind          QueueKind  `json:"kind"`
	BookingID     uuid.UUID  `json:"booking_id"`
	BookingRef    string     `json:"booking_ref"`
	CustomerName  string     `json:"customer_name"`
	PaymentID     *uuid.UUID `json:"payment_id,omitempty"`
	ScheduleID    *uuid.UUID `json:"schedule_id,omitempty"`
	Amount        int64      `json:"amount"`
	Currency      string     `json:"currency"`
	DueAt         *time.Time `json:"due_at,omitempty"`
	Status        string     `json:"status"`
	Note          string     `json:"note,omitempty"`
}

// ReportingCurrencyPolicy converts amounts into branch reporting currency (T-120).
type ReportingCurrencyPolicy interface {
	ReportingCurrency(ctx context.Context, branchID uuid.UUID) (string, error)
	// Convert returns amount in reporting currency (identity by default).
	Convert(ctx context.Context, branchID uuid.UUID, amount int64, fromCurrency string) (int64, string, error)
}

// IdentityFX is the default no-op FX policy.
type IdentityFX struct {
	DefaultCurrency string
}

func (IdentityFX) ReportingCurrency(_ context.Context, _ uuid.UUID) (string, error) {
	return "SAR", nil
}

func (p IdentityFX) Convert(_ context.Context, _ uuid.UUID, amount int64, from string) (int64, string, error) {
	cur := from
	if cur == "" {
		cur = "SAR"
	}
	if p.DefaultCurrency != "" {
		return amount, p.DefaultCurrency, nil
	}
	return amount, cur, nil
}

type Repository interface {
	Insert(ctx context.Context, p *Payment) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status Status, approvedBy *uuid.UUID, approvedAt *time.Time) error
	Get(ctx context.Context, id uuid.UUID) (*Payment, error)
	FindByIdempotencyKey(ctx context.Context, key string) (*Payment, error)
	SumCollectedByBooking(ctx context.Context, bookingID uuid.UUID) (int64, error)
	SumByBookingStatus(ctx context.Context, bookingID uuid.UUID, status Status) (int64, error)
	ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]Payment, error)
	ListByStatus(ctx context.Context, branchID uuid.UUID, status Status, eventType *EventType, limit int) ([]Payment, error)

	UpsertSchedule(ctx context.Context, s *Schedule) error
	ListSchedules(ctx context.Context, bookingID uuid.UUID) ([]Schedule, error)
	GetSchedule(ctx context.Context, id uuid.UUID) (*Schedule, error)
	UpdateScheduleStatus(ctx context.Context, id uuid.UUID, status ScheduleStatus) error
	ListOverdueSchedules(ctx context.Context, branchID uuid.UUID, now time.Time, limit int) ([]Schedule, error)
	ListDueForReminder(ctx context.Context, now time.Time, within time.Duration, limit int) ([]Schedule, error)
	MarkReminderSent(ctx context.Context, id uuid.UUID, at time.Time) error

	ListCreditBookings(ctx context.Context, branchID uuid.UUID, limit int) ([]QueueItem, error)
	GetFinanceSettings(ctx context.Context, branchID uuid.UUID) (reportingCurrency string, err error)
	UpsertFinanceSettings(ctx context.Context, branchID uuid.UUID, reportingCurrency string) error
}
