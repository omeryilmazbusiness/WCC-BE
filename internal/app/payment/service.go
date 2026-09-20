package payment

import (
	"context"
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
	audit    audit.Recorder
}

func NewService(
	payments domain.Repository,
	bookings bookingdomain.Repository,
	txm *tx.Manager,
	bus *events.Bus,
) *Service {
	return &Service{payments: payments, bookings: bookings, tx: txm, bus: bus}
}

func (s *Service) SetAuditor(a audit.Recorder) { s.audit = a }

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
		// Re-check idempotency inside the transaction (race-safe).
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
			ID:             uuid.New(),
			BookingID:      in.BookingID,
			Amount:         in.Amount,
			Currency:       currency,
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
		b.UpdatedAt = time.Now().UTC()
		if err := s.bookings.Update(ctx, b); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.audit != nil && out != nil {
		id := out.ID
		bid := out.BookingID
		_ = s.audit.Record(ctx, audit.RecordInput{
			ActorID: in.RecordedBy, Action: "payment.recorded", EntityType: "payment", EntityID: &id,
			After: map[string]any{"booking_id": bid, "amount": out.Amount, "currency": out.Currency, "method": out.Method},
		})
	}
	s.bus.Publish(ctx, events.Event{Name: events.PaymentRecorded, Payload: out})
	return out, nil
}

func (s *Service) ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]domain.Payment, error) {
	if _, err := s.bookings.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	return s.payments.ListByBooking(ctx, bookingID)
}
