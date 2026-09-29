package setup

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/setup"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func ensureCompany(ctx context.Context, id uuid.UUID) error {
	s, err := access.Require(ctx)
	if err != nil {
		return shared.NewForbidden("access scope missing")
	}
	if !s.CanAccessCompany(id) {
		return shared.NewNotFound("company")
	}
	return nil
}

func (r *Repository) Lock(ctx context.Context, companyID uuid.UUID) error {
	if err := ensureCompany(ctx, companyID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('company_setup:' || $1::text, 0))`, companyID)
	return err
}

func (r *Repository) GetState(ctx context.Context, companyID uuid.UUID) (*domain.State, error) {
	if err := ensureCompany(ctx, companyID); err != nil {
		return nil, err
	}
	s := domain.State{CompanyID: companyID}
	err := tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT company_done_at, staff_done_at, ai_skipped_at, channels_skipped_at,
		       dismissed_at, completed_at, completed_by, updated_at
		FROM company_setup WHERE company_id = $1`, companyID,
	).Scan(&s.CompanyDoneAt, &s.StaffDoneAt, &s.AISkippedAt, &s.ChannelsSkippedAt,
		&s.DismissedAt, &s.CompletedAt, &s.CompletedBy, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &s, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CountStaff counts active users of the given branches other than GMs and
// platform admins, limited to branches the caller may see.
func (r *Repository) CountStaff(ctx context.Context, branchIDs []uuid.UUID) (int, error) {
	s, err := access.Require(ctx)
	if err != nil {
		return 0, shared.NewForbidden("access scope missing")
	}
	visible := make([]uuid.UUID, 0, len(branchIDs))
	for _, id := range branchIDs {
		if s.CanAccessBranch(id) {
			visible = append(visible, id)
		}
	}
	if len(visible) == 0 {
		return 0, nil
	}
	var n int
	err = tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT COUNT(*) FROM users
		WHERE branch_id = ANY($1::uuid[]) AND is_active AND role NOT IN ('gm', 'admin')`, visible,
	).Scan(&n)
	return n, err
}

func (r *Repository) SaveState(ctx context.Context, s *domain.State) error {
	if err := ensureCompany(ctx, s.CompanyID); err != nil {
		return err
	}
	return tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO company_setup (company_id, company_done_at, staff_done_at, ai_skipped_at,
			channels_skipped_at, dismissed_at, completed_at, completed_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
		ON CONFLICT (company_id) DO UPDATE SET
			company_done_at = EXCLUDED.company_done_at, staff_done_at = EXCLUDED.staff_done_at,
			ai_skipped_at = EXCLUDED.ai_skipped_at, channels_skipped_at = EXCLUDED.channels_skipped_at,
			dismissed_at = EXCLUDED.dismissed_at, completed_at = EXCLUDED.completed_at,
			completed_by = EXCLUDED.completed_by, updated_at = NOW()
		RETURNING updated_at`,
		s.CompanyID, s.CompanyDoneAt, s.StaffDoneAt, s.AISkippedAt,
		s.ChannelsSkippedAt, s.DismissedAt, s.CompletedAt, s.CompletedBy,
	).Scan(&s.UpdatedAt)
}
