package customer

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Customer struct {
	ID          uuid.UUID
	BranchID    uuid.UUID
	FullName    string
	FullNameAR  string
	Phone       string
	Email       string
	Nationality string
	Notes       string
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type SearchFilter struct {
	Query    string
	BranchID *uuid.UUID
	Limit    int
	Offset   int
}

// Repository port — implemented by adapter/postgres.
type Repository interface {
	Create(ctx context.Context, c *Customer) error
	Update(ctx context.Context, c *Customer) error
	FindByID(ctx context.Context, id uuid.UUID) (*Customer, error)
	Search(ctx context.Context, f SearchFilter) ([]Customer, int, error)
	FindByPhone(ctx context.Context, phone string, branchID uuid.UUID) (*Customer, error)
}
