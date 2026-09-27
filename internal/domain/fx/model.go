package fx

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// JobRatesSync pulls rates from the configured RateProvider.
const JobRatesSync shared.JobName = "fx.rates_sync"

// ErrRateExists means the pair already has a rate for that effective date.
var ErrRateExists = errors.New("fx: rate exists for pair and date")

// StoredRate is a persisted rate row. Rates are company-wide, not per branch.
type StoredRate struct {
	ID uuid.UUID
	Rate
	CreatedBy *uuid.UUID
	CreatedAt time.Time
}

type ListFilter struct {
	Base   string
	Quote  string
	From   *time.Time
	To     *time.Time
	Limit  int
	Offset int
}

type Repository interface {
	// Insert returns ErrRateExists when (base, quote, effective_date) is taken.
	Insert(ctx context.Context, r *StoredRate) error
	// InsertIfAbsent never overwrites an existing rate for the same date.
	InsertIfAbsent(ctx context.Context, r *StoredRate) (bool, error)
	UpdateRate(ctx context.Context, id uuid.UUID, scaled int64, source string) error
	Delete(ctx context.Context, id uuid.UUID) error
	Get(ctx context.Context, id uuid.UUID) (*StoredRate, error)
	List(ctx context.Context, f ListFilter) ([]StoredRate, int64, error)
	// Latest returns the newest rate with effective_date <= on, or nil.
	Latest(ctx context.Context, base, quote string, on time.Time) (*Rate, error)
}

// RateProvider fetches published rates for a date (optional adapter).
type RateProvider interface {
	Name() string
	Fetch(ctx context.Context, on time.Time) ([]Rate, error)
}
