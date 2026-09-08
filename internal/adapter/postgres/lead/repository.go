package lead

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, l *domain.Lead) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO leads (
			id, branch_id, customer_id, full_name, phone, source, stage, owner_id, lost_reason, notes,
			converted_booking_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		l.ID, l.BranchID, l.CustomerID, l.FullName, l.Phone, l.Source, l.Stage, l.OwnerID, l.LostReason, l.Notes,
		l.ConvertedBookingID, l.CreatedAt, l.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, l *domain.Lead) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE leads SET customer_id=$2, full_name=$3, phone=$4, source=$5, stage=$6, owner_id=$7,
			lost_reason=$8, notes=$9, converted_booking_id=$10, updated_at=$11
		WHERE id=$1`,
		l.ID, l.CustomerID, l.FullName, l.Phone, l.Source, l.Stage, l.OwnerID, l.LostReason, l.Notes,
		l.ConvertedBookingID, l.UpdatedAt,
	)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Lead, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, customer_id, full_name, phone, source, stage, owner_id, lost_reason, notes,
			converted_booking_id, created_at, updated_at
		FROM leads WHERE id=$1`, id)
	var l domain.Lead
	var stage string
	err := row.Scan(&l.ID, &l.BranchID, &l.CustomerID, &l.FullName, &l.Phone, &l.Source, &stage, &l.OwnerID,
		&l.LostReason, &l.Notes, &l.ConvertedBookingID, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	if err != nil {
		return nil, err
	}
	l.Stage = domain.Stage(stage)
	return &l, nil
}

func (r *Repository) AppendStageHistory(ctx context.Context, h *domain.StageHistory) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO lead_stage_history (id, lead_id, from_stage, to_stage, changed_by, note, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		h.ID, h.LeadID, h.FromStage, h.ToStage, h.ChangedBy, h.Note, h.CreatedAt,
	)
	return err
}

func (r *Repository) ListByOwner(ctx context.Context, ownerID uuid.UUID, limit, offset int) ([]domain.Lead, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM leads WHERE owner_id=$1`, ownerID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, customer_id, full_name, phone, source, stage, owner_id, lost_reason, notes,
			converted_booking_id, created_at, updated_at
		FROM leads WHERE owner_id=$1 ORDER BY updated_at DESC LIMIT $2 OFFSET $3`, ownerID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Lead
	for rows.Next() {
		var l domain.Lead
		var stage string
		if err := rows.Scan(&l.ID, &l.BranchID, &l.CustomerID, &l.FullName, &l.Phone, &l.Source, &stage, &l.OwnerID,
			&l.LostReason, &l.Notes, &l.ConvertedBookingID, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, 0, err
		}
		l.Stage = domain.Stage(stage)
		out = append(out, l)
	}
	return out, total, rows.Err()
}
