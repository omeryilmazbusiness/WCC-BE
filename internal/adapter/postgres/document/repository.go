package document

import (
	"context"
	"errors"
	"fmt"
	"time"

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

const docCols = `id, branch_id, related_type, related_id, kind, file_name, content_type, size_bytes,
	storage_key, uploaded_by, created_at, status, review_note, reviewed_by, reviewed_at,
	expires_at, replaces_id, participant_id, version`

func scanDoc(row pgx.Row) (*domain.Document, error) {
	var d domain.Document
	var expiresAt *time.Time
	err := row.Scan(
		&d.ID, &d.BranchID, &d.RelatedType, &d.RelatedID, &d.Kind, &d.FileName, &d.ContentType, &d.SizeBytes,
		&d.StorageKey, &d.UploadedBy, &d.CreatedAt, &d.State, &d.ReviewNote, &d.ReviewedBy, &d.ReviewedAt,
		&expiresAt, &d.ReplacesID, &d.ParticipantID, &d.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	if err != nil {
		return nil, err
	}
	d.ExpiresAt = expiresAt
	return &d, nil
}

func (r *Repository) Create(ctx context.Context, d *domain.Document) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if d.State == "" {
		d.State = domain.StatusPending
	}
	if d.Version <= 0 {
		d.Version = 1
	}
	_, err := q.Exec(ctx, `
		INSERT INTO documents (
			id, branch_id, related_type, related_id, kind, file_name, content_type, size_bytes,
			storage_key, uploaded_by, created_at, status, review_note, reviewed_by, reviewed_at,
			expires_at, replaces_id, participant_id, version
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		d.ID, d.BranchID, d.RelatedType, d.RelatedID, d.Kind, d.FileName, d.ContentType, d.SizeBytes,
		d.StorageKey, d.UploadedBy, d.CreatedAt, d.State, d.ReviewNote, d.ReviewedBy, d.ReviewedAt,
		d.ExpiresAt, d.ReplacesID, d.ParticipantID, d.Version,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, d *domain.Document) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		UPDATE documents SET
			kind=$2, file_name=$3, content_type=$4, size_bytes=$5, status=$6, review_note=$7,
			reviewed_by=$8, reviewed_at=$9, expires_at=$10, replaces_id=$11, participant_id=$12, version=$13
		WHERE id=$1`,
		d.ID, d.Kind, d.FileName, d.ContentType, d.SizeBytes, d.State, d.ReviewNote,
		d.ReviewedBy, d.ReviewedAt, d.ExpiresAt, d.ReplacesID, d.ParticipantID, d.Version,
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
	return scanDoc(q.QueryRow(ctx, `SELECT `+docCols+` FROM documents WHERE id=$1`, id))
}

func (r *Repository) ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]domain.Document, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `SELECT `+docCols+` FROM documents WHERE related_type=$1 AND related_id=$2 ORDER BY created_at DESC`, relatedType, relatedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDocs(rows)
}

func scanDocs(rows pgx.Rows) ([]domain.Document, error) {
	var out []domain.Document
	for rows.Next() {
		var d domain.Document
		var expiresAt *time.Time
		if err := rows.Scan(
			&d.ID, &d.BranchID, &d.RelatedType, &d.RelatedID, &d.Kind, &d.FileName, &d.ContentType, &d.SizeBytes,
			&d.StorageKey, &d.UploadedBy, &d.CreatedAt, &d.State, &d.ReviewNote, &d.ReviewedBy, &d.ReviewedAt,
			&expiresAt, &d.ReplacesID, &d.ParticipantID, &d.Version,
		); err != nil {
			return nil, err
		}
		d.ExpiresAt = expiresAt
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) ListExpiring(ctx context.Context, onOrBefore time.Time, limit int) ([]domain.Document, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT `+docCols+` FROM documents
		WHERE expires_at IS NOT NULL AND expires_at <= $1::date
		  AND status IN ('uploaded','submitted','approved')
		ORDER BY expires_at ASC
		LIMIT $2`, onOrBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDocs(rows)
}

func (r *Repository) ListApprovedBySubjects(ctx context.Context, subjects []domain.SubjectRef) ([]domain.Document, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	q := tx.QuerierFrom(ctx, r.pool)
	types := make([]string, len(subjects))
	ids := make([]uuid.UUID, len(subjects))
	for i, s := range subjects {
		types[i] = s.RelatedType
		ids[i] = s.RelatedID
	}
	rows, err := q.Query(ctx, `
		SELECT `+docCols+` FROM documents d
		WHERE d.status = 'approved'
		  AND EXISTS (
			SELECT 1 FROM unnest($1::text[], $2::uuid[]) AS s(related_type, related_id)
			WHERE s.related_type = d.related_type AND s.related_id = d.related_id
		  )
		ORDER BY d.created_at DESC`, types, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDocs(rows)
}

func (r *Repository) CreatePolicy(ctx context.Context, p *domain.Policy) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO document_policies (id, branch_id, name, package_id, nationality, is_active, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.ID, p.BranchID, p.Name, p.PackageID, p.Nationality, p.IsActive, p.CreatedAt,
	)
	return err
}

func (r *Repository) UpdatePolicy(ctx context.Context, p *domain.Policy) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		UPDATE document_policies SET name=$2, package_id=$3, nationality=$4, is_active=$5 WHERE id=$1`,
		p.ID, p.Name, p.PackageID, p.Nationality, p.IsActive,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) ReplaceRequirements(ctx context.Context, policyID uuid.UUID, reqs []domain.Requirement) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `DELETE FROM document_policy_requirements WHERE policy_id=$1`, policyID); err != nil {
		return err
	}
	for _, req := range reqs {
		if _, err := q.Exec(ctx, `
			INSERT INTO document_policy_requirements (id, policy_id, kind, required, label)
			VALUES ($1,$2,$3,$4,$5)`, req.ID, policyID, req.Kind, req.Required, req.Label); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) loadRequirements(ctx context.Context, policyID uuid.UUID) ([]domain.Requirement, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, policy_id, kind, required, label FROM document_policy_requirements
		WHERE policy_id=$1 ORDER BY kind`, policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Requirement
	for rows.Next() {
		var req domain.Requirement
		if err := rows.Scan(&req.ID, &req.PolicyID, &req.Kind, &req.Required, &req.Label); err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

func (r *Repository) FindPolicyByID(ctx context.Context, id uuid.UUID) (*domain.Policy, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, name, package_id, nationality, is_active, created_at
		FROM document_policies WHERE id=$1`, id)
	var p domain.Policy
	err := row.Scan(&p.ID, &p.BranchID, &p.Name, &p.PackageID, &p.Nationality, &p.IsActive, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	if err != nil {
		return nil, err
	}
	reqs, err := r.loadRequirements(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	p.Requirements = reqs
	return &p, nil
}

func (r *Repository) ListPolicies(ctx context.Context, branchID uuid.UUID) ([]domain.Policy, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, name, package_id, nationality, is_active, created_at
		FROM document_policies WHERE branch_id=$1 ORDER BY created_at DESC`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Policy
	for rows.Next() {
		var p domain.Policy
		if err := rows.Scan(&p.ID, &p.BranchID, &p.Name, &p.PackageID, &p.Nationality, &p.IsActive, &p.CreatedAt); err != nil {
			return nil, err
		}
		reqs, err := r.loadRequirements(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		p.Requirements = reqs
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) FindActivePolicy(ctx context.Context, branchID uuid.UUID, packageID *uuid.UUID, nationality string) (*domain.Policy, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	// Prefer nationality+package match, then package, then nationality, then default branch policy.
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, name, package_id, nationality, is_active, created_at
		FROM document_policies
		WHERE branch_id=$1 AND is_active=TRUE
		  AND (
			(package_id IS NOT DISTINCT FROM $2 AND nationality = $3)
			OR (package_id IS NOT DISTINCT FROM $2 AND nationality = '')
			OR (package_id IS NULL AND nationality = $3)
			OR (package_id IS NULL AND nationality = '')
		  )
		ORDER BY
			CASE WHEN package_id IS NOT DISTINCT FROM $2 AND nationality = $3 THEN 0
			     WHEN package_id IS NOT DISTINCT FROM $2 AND nationality = '' THEN 1
			     WHEN package_id IS NULL AND nationality = $3 THEN 2
			     ELSE 3 END,
			created_at DESC
		LIMIT 1`, branchID, packageID, nationality)
	var p domain.Policy
	err := row.Scan(&p.ID, &p.BranchID, &p.Name, &p.PackageID, &p.Nationality, &p.IsActive, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	reqs, err := r.loadRequirements(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	p.Requirements = reqs
	return &p, nil
}
