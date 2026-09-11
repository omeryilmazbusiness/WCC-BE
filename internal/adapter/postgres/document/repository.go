package document

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, d *domain.Document) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO documents (
			id, branch_id, related_type, related_id, kind, file_name, content_type, size_bytes, storage_key, uploaded_by, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		d.ID, d.BranchID, d.RelatedType, d.RelatedID, d.Kind, d.FileName, d.ContentType, d.SizeBytes, d.StorageKey, d.UploadedBy, d.CreatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, d *domain.Document) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		UPDATE documents
		SET kind = $2, file_name = $3, content_type = $4, size_bytes = $5
		WHERE id = $1`,
		d.ID, d.Kind, d.FileName, d.ContentType, d.SizeBytes,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Document, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, related_type, related_id, kind, file_name, content_type, size_bytes, storage_key, uploaded_by, created_at
		FROM documents WHERE id=$1`, id)
	var d domain.Document
	err := row.Scan(&d.ID, &d.BranchID, &d.RelatedType, &d.RelatedID, &d.Kind, &d.FileName, &d.ContentType, &d.SizeBytes, &d.StorageKey, &d.UploadedBy, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return &d, err
}

func (r *Repository) ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]domain.Document, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, related_type, related_id, kind, file_name, content_type, size_bytes, storage_key, uploaded_by, created_at
		FROM documents WHERE related_type=$1 AND related_id=$2 ORDER BY created_at DESC`, relatedType, relatedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Document
	for rows.Next() {
		var d domain.Document
		if err := rows.Scan(&d.ID, &d.BranchID, &d.RelatedType, &d.RelatedID, &d.Kind, &d.FileName, &d.ContentType, &d.SizeBytes, &d.StorageKey, &d.UploadedBy, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
