package tourpackage

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreatePackageInput struct {
	BranchID    uuid.UUID
	Code        string
	NameEN      string
	NameAR      string
	Description string
}

type CreateDepartureInput struct {
	PackageID     uuid.UUID
	Code          string
	DepartDate    time.Time
	ReturnDate    time.Time
	CapacityTotal int
	BasePrice     int64
	Currency      string
}

type CloneDepartureInput struct {
	SourceID   uuid.UUID
	Code       string
	DepartDate time.Time
	ReturnDate time.Time
}

type Service struct {
	repo domain.Repository
	tx   *tx.Manager
}

func NewService(repo domain.Repository, txm *tx.Manager) *Service {
	return &Service{repo: repo, tx: txm}
}

func (s *Service) CreatePackage(ctx context.Context, in CreatePackageInput) (*domain.Package, error) {
	code := strings.TrimSpace(in.Code)
	name := strings.TrimSpace(in.NameEN)
	if code == "" || name == "" {
		return nil, shared.NewValidation("code and name_en are required")
	}
	now := time.Now().UTC()
	p := &domain.Package{
		ID:          uuid.New(),
		BranchID:    in.BranchID,
		Code:        code,
		NameEN:      name,
		NameAR:      strings.TrimSpace(in.NameAR),
		Description: strings.TrimSpace(in.Description),
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.CreatePackage(ctx, p)
	}); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) ListPackages(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]domain.Package, error) {
	return s.repo.ListPackages(ctx, branchID, activeOnly)
}

func (s *Service) GetPackage(ctx context.Context, id uuid.UUID) (*domain.Package, error) {
	p, err := s.repo.FindPackage(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("package")
	}
	return p, nil
}

func (s *Service) CreateDeparture(ctx context.Context, in CreateDepartureInput) (*domain.Departure, error) {
	if _, err := s.repo.FindPackage(ctx, in.PackageID); err != nil {
		return nil, shared.NewNotFound("package")
	}
	if err := domain.ValidateDepartureDates(in.DepartDate, in.ReturnDate); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, shared.NewValidation("code is required")
	}
	if in.CapacityTotal < 0 {
		return nil, shared.NewValidation("capacity_total must be >= 0")
	}
	currency := in.Currency
	if currency == "" {
		currency = "USD"
	}
	now := time.Now().UTC()
	d := &domain.Departure{
		ID:            uuid.New(),
		PackageID:     in.PackageID,
		Code:          code,
		DepartDate:    in.DepartDate,
		ReturnDate:    in.ReturnDate,
		CapacityTotal: in.CapacityTotal,
		CapacitySold:  0,
		BasePrice:     in.BasePrice,
		Currency:      currency,
		IsActive:      true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.CreateDeparture(ctx, d)
	}); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) CloneDeparture(ctx context.Context, in CloneDepartureInput) (*domain.Departure, error) {
	src, err := s.repo.FindDeparture(ctx, in.SourceID)
	if err != nil {
		return nil, shared.NewNotFound("departure")
	}
	if err := domain.ValidateDepartureDates(in.DepartDate, in.ReturnDate); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, shared.NewValidation("code is required")
	}
	d := src.Clone(uuid.New(), code, in.DepartDate, in.ReturnDate)
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.CreateDeparture(ctx, d)
	}); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) GetDeparture(ctx context.Context, id uuid.UUID) (*domain.Departure, error) {
	d, err := s.repo.FindDeparture(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("departure")
	}
	return d, nil
}

func (s *Service) ListDepartures(ctx context.Context, packageID uuid.UUID) ([]domain.Departure, error) {
	return s.repo.ListDepartures(ctx, packageID)
}

// SyncCapacitySold recomputes capacity_sold from confirmed bookings (single source of truth).
func (s *Service) SyncCapacitySold(ctx context.Context, departureID uuid.UUID, sold int) error {
	if sold < 0 {
		return shared.NewValidation("sold capacity cannot be negative")
	}
	return s.repo.UpdateDepartureCapacitySold(ctx, departureID, sold)
}
