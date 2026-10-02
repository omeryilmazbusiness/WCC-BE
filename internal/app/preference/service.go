// Package preference serves the caller's own UI preferences.
package preference

import (
	"context"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/preference"
)

type Service struct {
	repo domain.Repository
	now  func() time.Time
}

func NewService(repo domain.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// Get returns the stored preferences or an empty default (nil favorites).
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (*domain.Preferences, error) {
	p, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return &domain.Preferences{UserID: userID}, nil
	}
	return p, nil
}

// SetNavFavorites stores the pinned shortcuts; nil resets to the role default.
func (s *Service) SetNavFavorites(ctx context.Context, userID uuid.UUID, favorites []string) (*domain.Preferences, error) {
	p := &domain.Preferences{UserID: userID, UpdatedAt: s.now().UTC()}
	if favorites != nil {
		clean, err := domain.NormalizeNavFavorites(favorites)
		if err != nil {
			return nil, err
		}
		p.NavFavorites = clean
	}
	if err := s.repo.Upsert(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}
