package identity

import (
	"context"
	"time"

	"github.com/google/uuid"

	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	FullName     string
	Role         platformauth.Role
	BranchID     uuid.UUID
	TeamID       *uuid.UUID
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Branch struct {
	ID        uuid.UUID
	Code      string
	NameEN    string
	NameAR    string
	IsActive  bool
	CreatedAt time.Time
}

// Repository is the persistence port (Dependency Inversion).
type Repository interface {
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	FindUserByID(ctx context.Context, id uuid.UUID) (*User, error)
	CreateUser(ctx context.Context, user *User) error
	ListBranches(ctx context.Context) ([]Branch, error)
}
