package payment

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Payment is an immutable ledger entry (append-only). Never update/delete.
type Payment struct {
	ID            uuid.UUID
	BookingID     uuid.UUID
	Amount        int64
	Currency      string
	Method        string
	Reference     string
	RecordedBy    uuid.UUID
	IdempotencyKey string
	CreatedAt     time.Time
}

type Repository interface {
	Insert(ctx context.Context, p *Payment) error
	FindByIdempotencyKey(ctx context.Context, key string) (*Payment, error)
	SumByBooking(ctx context.Context, bookingID uuid.UUID) (int64, error)
	ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]Payment, error)
}
