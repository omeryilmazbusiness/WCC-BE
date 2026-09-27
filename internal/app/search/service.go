package search

import (
	"context"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/search"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Service struct {
	searcher domain.Searcher
}

func NewService(searcher domain.Searcher) *Service {
	return &Service{searcher: searcher}
}

// Search looks up records visible to the caller; requested nil means all
// branches for global callers and the caller's branch otherwise.
func (s *Service) Search(ctx context.Context, requested *uuid.UUID, q string, limit int) ([]domain.Hit, error) {
	nq := domain.NormalizeQuery(q)
	if nq == "" {
		return nil, shared.NewValidation("q must be at least 2 characters")
	}
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, err
	}
	branchID, err := scope.ResolveBranch(requested)
	if err != nil {
		return nil, err
	}
	return s.searcher.Search(ctx, branchID, nq, limit)
}
