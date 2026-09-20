package customer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Customer struct {
	ID                  uuid.UUID
	BranchID            uuid.UUID
	FullName            string
	FullNameAR          string
	Phone               string
	Email               string
	Nationality         string
	PassportNo          string
	DateOfBirth         *time.Time
	Preferences         json.RawMessage
	SpecialRequirements string
	Notes               string
	MergedIntoID        *uuid.UUID
	IsActive            bool
	CreatedBy           uuid.UUID
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type CompanionLink struct {
	ID          uuid.UUID
	CustomerID  uuid.UUID
	CompanionID uuid.UUID
	Relation    string
	Notes       string
	CreatedAt   time.Time
	// Hydrated companion summary (optional)
	Companion *Customer
}

type TimelineItem struct {
	Kind      string          `json:"kind"` // lead|booking|payment|document|task|note|activity
	ID        uuid.UUID       `json:"id"`
	Title     string          `json:"title"`
	Status    string          `json:"status,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
	Meta      json.RawMessage `json:"meta,omitempty"`
}

type SearchFilter struct {
	Query    string
	BranchID *uuid.UUID
	Limit    int
	Offset   int
	// IncludeMerged when true returns soft-merged inactive records too.
	IncludeMerged bool
}

type DuplicateMatch struct {
	Customer *Customer
	Reasons  []string // phone|email|passport|name_fuzzy
	Score    int
}

// Repository port — implemented by adapter/postgres.
type Repository interface {
	Create(ctx context.Context, c *Customer) error
	Update(ctx context.Context, c *Customer) error
	FindByID(ctx context.Context, id uuid.UUID) (*Customer, error)
	Search(ctx context.Context, f SearchFilter) ([]Customer, int, error)
	FindByPhone(ctx context.Context, phone string, branchID uuid.UUID) (*Customer, error)
	FindByEmail(ctx context.Context, email string, branchID uuid.UUID) (*Customer, error)
	FindByPassport(ctx context.Context, passport string, branchID uuid.UUID) (*Customer, error)
	FindNameCandidates(ctx context.Context, name string, branchID uuid.UUID, limit int) ([]Customer, error)
	MarkMerged(ctx context.Context, sourceID, targetID uuid.UUID) error
	ReassignLeads(ctx context.Context, from, to uuid.UUID) error
	ReassignBookings(ctx context.Context, from, to uuid.UUID) error
	ListCompanions(ctx context.Context, customerID uuid.UUID) ([]CompanionLink, error)
	LinkCompanion(ctx context.Context, link *CompanionLink) error
	UnlinkCompanion(ctx context.Context, customerID, companionID uuid.UUID) error
	ListTimeline(ctx context.Context, customerID uuid.UUID, limit int) ([]TimelineItem, error)
}
