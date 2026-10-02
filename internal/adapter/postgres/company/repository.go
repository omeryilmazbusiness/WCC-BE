package company

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

const uniqueViolation = "23505"

// conflicts maps unique constraints to the JSON field they protect.
var conflicts = map[string]string{
	"companies_slug_key":        "slug",
	"branches_company_slug_key": "slug",
	"branches_code_key":         "code",
	"branches_one_main_center":  "kind",
}

func mapConflict(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return err
	}
	field, ok := conflicts[pgErr.ConstraintName]
	if !ok {
		return shared.NewConflict("already exists")
	}
	c := shared.NewConflict(field + " already in use")
	c.Details = map[string]any{field: "already in use"}
	return c
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

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const companyColumns = `id, slug, name_en, name_ar, legal_name, phone, email, website, country, city,
	address, currency, timezone, is_active, created_at, updated_at`

func scanCompany(row pgx.Row, c *domain.Company) error {
	return row.Scan(&c.ID, &c.Slug, &c.NameEN, &c.NameAR, &c.LegalName, &c.Phone, &c.Email, &c.Website,
		&c.Country, &c.City, &c.Address, &c.Currency, &c.Timezone, &c.IsActive, &c.CreatedAt, &c.UpdatedAt)
}

// CreateCompany is a platform operation (global scope only).
func (r *Repository) CreateCompany(ctx context.Context, c *domain.Company) error {
	if s := access.From(ctx); s.Level != access.LevelGlobal {
		return shared.NewForbidden("creating companies needs platform scope")
	}
	err := tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO companies (id, slug, name_en, name_ar, legal_name, phone, email, website, country, city,
			address, currency, timezone, is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,TRUE)
		RETURNING is_active, created_at, updated_at`,
		c.ID, c.Slug, c.NameEN, c.NameAR, c.LegalName, c.Phone, c.Email, c.Website, c.Country, c.City,
		c.Address, c.Currency, c.Timezone,
	).Scan(&c.IsActive, &c.CreatedAt, &c.UpdatedAt)
	return mapConflict(err)
}

func (r *Repository) GetCompany(ctx context.Context, id uuid.UUID) (*domain.Company, error) {
	if err := ensureCompany(ctx, id); err != nil {
		return nil, err
	}
	var c domain.Company
	err := scanCompany(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT `+companyColumns+` FROM companies WHERE id = $1`, id), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("company")
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) UpdateCompany(ctx context.Context, c *domain.Company, actor uuid.UUID) error {
	if err := ensureCompany(ctx, c.ID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	var oldTZ string
	err := q.QueryRow(ctx, `SELECT timezone FROM companies WHERE id = $1 FOR UPDATE`, c.ID).Scan(&oldTZ)
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewNotFound("company")
	}
	if err != nil {
		return err
	}
	err = q.QueryRow(ctx, `
		UPDATE companies SET slug=$2, name_en=$3, name_ar=$4, legal_name=$5, phone=$6, email=$7,
			website=$8, country=$9, city=$10, address=$11, currency=$12, timezone=$13,
			updated_by=$14, updated_at=NOW()
		WHERE id=$1
		RETURNING updated_at`,
		c.ID, c.Slug, c.NameEN, c.NameAR, c.LegalName, c.Phone, c.Email, c.Website, c.Country, c.City,
		c.Address, c.Currency, c.Timezone, actor,
	).Scan(&c.UpdatedAt)
	if err != nil {
		return mapConflict(err)
	}
	if oldTZ != c.Timezone {
		_, err = q.Exec(ctx, `UPDATE branches SET timezone=$3 WHERE company_id=$1 AND timezone=$2`,
			c.ID, oldTZ, c.Timezone)
	}
	return err
}

func (r *Repository) ListCompanies(ctx context.Context, query string, limit, offset int) (domain.Page, error) {
	if s := access.From(ctx); s.Level != access.LevelGlobal {
		return domain.Page{}, shared.NewForbidden("listing companies needs platform scope")
	}
	q := tx.QuerierFrom(ctx, r.pool)
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	rows, err := q.Query(ctx, `
		SELECT c.id, c.slug, c.name_en, c.name_ar, c.legal_name, c.phone, c.email, c.website, c.country, c.city,
		       c.address, c.currency, c.timezone, c.is_active, c.created_at, c.updated_at,
		       (SELECT count(*) FROM branches b WHERE b.company_id = c.id),
		       (SELECT count(*) FROM users u JOIN branches b ON b.id = u.branch_id WHERE b.company_id = c.id),
		       COALESCE((SELECT u.email FROM users u JOIN branches b ON b.id = u.branch_id
		                 WHERE b.company_id = c.id AND u.role = 'gm' ORDER BY u.created_at LIMIT 1), ''),
		       (SELECT l.updated_at FROM company_logos l WHERE l.company_id = c.id),
		       count(*) OVER ()
		FROM companies c
		WHERE lower(c.name_en) LIKE $1 OR c.slug LIKE $1
		ORDER BY c.created_at DESC
		LIMIT $2 OFFSET $3`, pattern, limit, offset)
	if err != nil {
		return domain.Page{}, err
	}
	defer rows.Close()
	var page domain.Page
	for rows.Next() {
		var s domain.Summary
		c := &s.Company
		if err := rows.Scan(&c.ID, &c.Slug, &c.NameEN, &c.NameAR, &c.LegalName, &c.Phone, &c.Email, &c.Website,
			&c.Country, &c.City, &c.Address, &c.Currency, &c.Timezone, &c.IsActive, &c.CreatedAt, &c.UpdatedAt,
			&s.BranchCount, &s.UserCount, &s.GMEmail, &s.LogoUpdatedAt, &page.Total); err != nil {
			return domain.Page{}, err
		}
		page.Items = append(page.Items, s)
	}
	return page, rows.Err()
}

const branchColumns = `id, company_id, code, slug, name_en, name_ar, kind, timezone, is_active, created_at`

func scanBranch(row pgx.Row, b *domain.Branch) error {
	return row.Scan(&b.ID, &b.CompanyID, &b.Code, &b.Slug, &b.NameEN, &b.NameAR, &b.Kind, &b.Timezone,
		&b.IsActive, &b.CreatedAt)
}

func (r *Repository) CreateBranch(ctx context.Context, b *domain.Branch) error {
	if err := ensureCompany(ctx, b.CompanyID); err != nil {
		return err
	}
	// A code clash must not abort the surrounding transaction: callers retry
	// generated codes within it.
	err := tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO branches (id, company_id, code, slug, name_en, name_ar, kind, timezone, is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (code) DO NOTHING
		RETURNING created_at`,
		b.ID, b.CompanyID, b.Code, b.Slug, b.NameEN, b.NameAR, b.Kind, b.Timezone, b.IsActive,
	).Scan(&b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		conflict := shared.NewConflict("branch code already in use")
		conflict.Details = map[string]any{"code": "already in use"}
		return conflict
	}
	return mapConflict(err)
}

func (r *Repository) UpdateBranch(ctx context.Context, b *domain.Branch) error {
	if err := ensureCompany(ctx, b.CompanyID); err != nil {
		return err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		UPDATE branches SET code=$3, slug=$4, name_en=$5, name_ar=$6, kind=$7, timezone=$8, is_active=$9
		WHERE id=$1 AND company_id=$2`,
		b.ID, b.CompanyID, b.Code, b.Slug, b.NameEN, b.NameAR, b.Kind, b.Timezone, b.IsActive)
	if err != nil {
		return mapConflict(err)
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("branch")
	}
	return nil
}

func (r *Repository) DemoteMainCenter(ctx context.Context, companyID, keep uuid.UUID) error {
	if err := ensureCompany(ctx, companyID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		UPDATE branches SET kind='branch' WHERE company_id=$1 AND kind='main_center' AND id<>$2`,
		companyID, keep)
	return err
}

func (r *Repository) ListBranches(ctx context.Context, companyID uuid.UUID) ([]domain.Branch, error) {
	if err := ensureCompany(ctx, companyID); err != nil {
		return nil, err
	}
	return r.branchesOf(ctx, companyID)
}

func (r *Repository) branchesOf(ctx context.Context, companyID uuid.UUID) ([]domain.Branch, error) {
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT `+branchColumns+` FROM branches WHERE company_id=$1
		ORDER BY (kind = 'main_center') DESC, created_at, id`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Branch
	for rows.Next() {
		var b domain.Branch
		if err := scanBranch(rows, &b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// WorkspaceOf runs before a request scope exists (authentication), so it is
// deliberately unscoped: it only reveals the caller's own tenant.
func (r *Repository) WorkspaceOf(ctx context.Context, branchID uuid.UUID) (*domain.Workspace, error) {
	var w domain.Workspace
	err := scanCompany(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT c.id, c.slug, c.name_en, c.name_ar, c.legal_name, c.phone, c.email, c.website, c.country, c.city,
		       c.address, c.currency, c.timezone, c.is_active, c.created_at, c.updated_at
		FROM branches b JOIN companies c ON c.id = b.company_id
		WHERE b.id = $1`, branchID), &w.Company)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("branch")
	}
	if err != nil {
		return nil, err
	}
	if w.Branches, err = r.branchesOf(ctx, w.Company.ID); err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *Repository) LockCompany(ctx context.Context, companyID uuid.UUID) error {
	if err := ensureCompany(ctx, companyID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('company:' || $1::text, 0))`, companyID)
	return err
}
