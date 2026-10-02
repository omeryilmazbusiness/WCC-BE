package preference

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/preference"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Get(ctx context.Context, userID uuid.UUID) (*domain.Preferences, error) {
	var p domain.Preferences
	err := tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT user_id, nav_favorites, welcome_seen_at, updated_at
		FROM user_preferences WHERE user_id=$1`, userID).
		Scan(&p.UserID, &p.NavFavorites, &p.WelcomeSeenAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) Upsert(ctx context.Context, p *domain.Preferences) error {
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO user_preferences (user_id, nav_favorites, updated_at)
		VALUES ($1,$2,$3)
		ON CONFLICT (user_id) DO UPDATE SET
			nav_favorites=EXCLUDED.nav_favorites,
			updated_at=EXCLUDED.updated_at`,
		p.UserID, p.NavFavorites, p.UpdatedAt,
	)
	return err
}

func (r *Repository) MarkWelcomeSeen(ctx context.Context, userID uuid.UUID, at time.Time) error {
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO user_preferences (user_id, welcome_seen_at, updated_at)
		VALUES ($1,$2,$2)
		ON CONFLICT (user_id) DO UPDATE SET
			welcome_seen_at=COALESCE(user_preferences.welcome_seen_at, EXCLUDED.welcome_seen_at),
			updated_at=EXCLUDED.updated_at`,
		userID, at,
	)
	return err
}
