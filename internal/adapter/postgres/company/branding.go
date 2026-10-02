package company

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

const brandingSelect = `
	SELECT c.slug, c.name_en, c.name_ar, l.updated_at
	FROM companies c
	LEFT JOIN company_logos l ON l.company_id = c.id`

func scanBranding(row pgx.Row) (*domain.Branding, error) {
	var b domain.Branding
	err := row.Scan(&b.Slug, &b.NameEN, &b.NameAR, &b.LogoUpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("company")
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// BrandingBySlug is public (pre sign-in): no tenant scope, active companies only.
func (r *Repository) BrandingBySlug(ctx context.Context, slug string) (*domain.Branding, error) {
	return scanBranding(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		brandingSelect+` WHERE c.slug = $1 AND c.is_active`, slug))
}

func (r *Repository) BrandingOf(ctx context.Context, companyID uuid.UUID) (*domain.Branding, error) {
	if err := ensureCompany(ctx, companyID); err != nil {
		return nil, err
	}
	return scanBranding(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, brandingSelect+` WHERE c.id = $1`, companyID))
}

// LogoBySlug is public (pre sign-in): no tenant scope, active companies only.
func (r *Repository) LogoBySlug(ctx context.Context, slug string) (*domain.Logo, error) {
	var l domain.Logo
	err := tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT l.content_type, l.data, l.updated_at
		FROM company_logos l
		JOIN companies c ON c.id = l.company_id
		WHERE c.slug = $1 AND c.is_active`, slug).
		Scan(&l.ContentType, &l.Data, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("logo")
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *Repository) SetLogo(ctx context.Context, companyID uuid.UUID, logo domain.Logo) error {
	if err := ensureCompany(ctx, companyID); err != nil {
		return err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO company_logos (company_id, content_type, data, updated_at)
		SELECT id, $2, $3, $4 FROM companies WHERE id = $1
		ON CONFLICT (company_id) DO UPDATE SET
			content_type=EXCLUDED.content_type, data=EXCLUDED.data, updated_at=EXCLUDED.updated_at`,
		companyID, logo.ContentType, logo.Data, logo.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("company")
	}
	return nil
}

func (r *Repository) DeleteLogo(ctx context.Context, companyID uuid.UUID) error {
	if err := ensureCompany(ctx, companyID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `DELETE FROM company_logos WHERE company_id = $1`, companyID)
	return err
}

// CompanySlugOfBranch backs the sign-in company check, which runs before any scope exists.
func (r *Repository) CompanySlugOfBranch(ctx context.Context, branchID uuid.UUID) (string, error) {
	var slug string
	err := tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT c.slug FROM branches b JOIN companies c ON c.id = b.company_id WHERE b.id = $1`, branchID).
		Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", shared.NewNotFound("company")
	}
	return slug, err
}
