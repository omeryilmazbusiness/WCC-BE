package tourpackage

import (
	"context"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

type Service struct {
	repo domain.Repository
}

func NewService(repo domain.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetPackage(ctx context.Context, id uuid.UUID) (*domain.Package, error) {
	p, err := s.repo.FindPackage(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("package")
	}
	return p, nil
}

func (s *Service) ListDepartures(ctx context.Context, packageID uuid.UUID) ([]domain.Departure, error) {
	return s.repo.ListDepartures(ctx, packageID)
}
