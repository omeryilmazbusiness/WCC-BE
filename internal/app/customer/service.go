package customer

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreateInput struct {
	BranchID    uuid.UUID
	FullName    string
	FullNameAR  string
	Phone       string
	Email       string
	Nationality string
	Notes       string
	CreatedBy   uuid.UUID
}

type CreateResult struct {
	Customer       *domain.Customer
	DuplicateWarn  bool
	DuplicateOfID  *uuid.UUID
}

type Service struct {
	repo domain.Repository
	tx   *tx.Manager
}

func NewService(repo domain.Repository, txm *tx.Manager) *Service {
	return &Service{repo: repo, tx: txm}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	name := strings.TrimSpace(in.FullName)
	phone := strings.TrimSpace(in.Phone)
	if name == "" || phone == "" {
		return nil, shared.NewValidation("full_name and phone are required")
	}

	var dupWarn bool
	var dupID *uuid.UUID
	if existing, err := s.repo.FindByPhone(ctx, phone, in.BranchID); err == nil && existing != nil {
		dupWarn = true
		id := existing.ID
		dupID = &id
	}

	now := time.Now().UTC()
	c := &domain.Customer{
		ID:          uuid.New(),
		BranchID:    in.BranchID,
		FullName:    name,
		FullNameAR:  strings.TrimSpace(in.FullNameAR),
		Phone:       phone,
		Email:       strings.TrimSpace(strings.ToLower(in.Email)),
		Nationality: strings.TrimSpace(in.Nationality),
		Notes:       in.Notes,
		CreatedBy:   in.CreatedBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.Create(ctx, c)
	}); err != nil {
		return nil, err
	}

	return &CreateResult{Customer: c, DuplicateWarn: dupWarn, DuplicateOfID: dupID}, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Customer, error) {
	c, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("customer")
	}
	return c, nil
}

func (s *Service) Search(ctx context.Context, f domain.SearchFilter) ([]domain.Customer, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	return s.repo.Search(ctx, f)
}
