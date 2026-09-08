package payment

import (
	"context"
	"time"

	"github.com/google/uuid"

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
	RecordedBy     uuid.UUID
	IdempotencyKey string
}

// Service implements immutable ledger insert + booking balance recompute
// inside one transaction (ACID: Atomicity + Consistency).
type Service struct {
	payments domain.Repository
	bookings bookingdomain.Repository
	tx       *tx.Manager
	bus      *events.Bus
}

func NewService(
	payments domain.Repository,
	bookings bookingdomain.Repository,
	txm *tx.Manager,
	bus *events.Bus,
) *Service {
	return &Service{payments: payments, bookings: bookings, tx: txm, bus: bus}
}

func (s *Service) Record(ctx context.Context, in RecordInput) (*domain.Payment, error) {
	if in.Amount <= 0 {
		return nil, shared.NewValidation("amount must be > 0")
	}
	if in.IdempotencyKey == "" {
		return nil, shared.NewValidation("idempotency_key is required")
	}

	if existing, err := s.payments.FindByIdempotencyKey(ctx, in.IdempotencyKey); err == nil && existing != nil {
		return existing, nil
	}

	var out *domain.Payment
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.bookings.FindByID(ctx, in.BookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		p := &domain.Payment{
			ID:             uuid.New(),
			BookingID:      in.BookingID,
			Amount:         in.Amount,
			Currency:       in.Currency,
			Method:         in.Method,
			Reference:      in.Reference,
			RecordedBy:     in.RecordedBy,
			IdempotencyKey: in.IdempotencyKey,
			CreatedAt:      time.Now().UTC(),
		}
		if err := s.payments.Insert(ctx, p); err != nil {
			return err
		}
		sum, err := s.payments.SumByBooking(ctx, in.BookingID)
		if err != nil {
			return err
		}
		b.CollectedAmt = sum
		b.RecomputeBalance()
		if err := s.bookings.Update(ctx, b); err != nil {
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
