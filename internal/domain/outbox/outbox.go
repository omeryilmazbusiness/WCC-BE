// Package outbox models durable domain events awaiting delivery (T-281).
package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusDispatched Status = "dispatched"
	StatusDead       Status = "dead"
)

// ErrNotFound is returned when a record to requeue is missing or not dead.
var ErrNotFound = errors.New("outbox record not found")

// MaxAttempts is how many deliveries are tried before a record is dead-lettered.
const MaxAttempts = 8

const (
	baseBackoff = 5 * time.Second
	maxBackoff  = 30 * time.Minute
)

type Record struct {
	ID            uuid.UUID
	Name          string
	Payload       []byte
	BranchID      *uuid.UUID
	Status        Status
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	CreatedAt     time.Time
	DispatchedAt  *time.Time
}

// Backoff is the wait after the n-th failed attempt: 5s doubling, capped at 30m.
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := baseBackoff
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= maxBackoff {
			return maxBackoff
		}
	}
	return d
}

// AfterFailure decides the next attempt time, or dead-letters the record.
func AfterFailure(attempts int, now time.Time) (next time.Time, dead bool) {
	if attempts >= MaxAttempts {
		return now, true
	}
	return now.Add(Backoff(attempts)), false
}

// TruncateError keeps stored errors bounded.
func TruncateError(msg string) string {
	const limit = 2000
	if len(msg) <= limit {
		return msg
	}
	return msg[:limit]
}

type Stats struct {
	Pending    int        `json:"pending"`
	Processing int        `json:"processing"`
	Dead       int        `json:"dead"`
	OldestDue  *time.Time `json:"oldest_due,omitempty"`
}

type Repository interface {
	Insert(ctx context.Context, name string, payload []byte, branchID *uuid.UUID) error
	// Claim leases up to limit due records (pending, or processing with an
	// expired lease) and increments their attempt counter.
	Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Record, error)
	MarkDispatched(ctx context.Context, id uuid.UUID, at time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, next time.Time, dead bool, errMsg string) error
	Stats(ctx context.Context) (Stats, error)
	ListDead(ctx context.Context, limit int) ([]Record, error)
	Requeue(ctx context.Context, id uuid.UUID) error
	PurgeDispatched(ctx context.Context, before time.Time) (int64, error)
}
