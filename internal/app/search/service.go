package search

import (
	"context"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/search"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Service struct {
	searcher domain.Searcher
}

func NewService(searcher domain.Searcher) *Service {
	return &Service{searcher: searcher}
}

func (s *Service) Search(ctx context.Context, branchID uuid.UUID, q string, limit int) ([]domain.Hit, error) {
	nq := domain.NormalizeQuery(q)
	if nq == "" {
		return nil, shared.NewValidation("q must be at least 2 characters")
	}
	if branchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	return s.searcher.Search(ctx, branchID, nq, limit)
}
