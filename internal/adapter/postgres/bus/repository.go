// Package bus persists the operations coach-operator catalogue.
package bus

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/bus"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

var _ domain.Repository = (*Repository)(nil)

var busScope = pgscope.Columns{Branch: "c.branch_id"}

const columns = `c.id, c.branch_id, c.name, c.phone, c.email, c.website, c.country, c.city, c.address,
	c.vehicle_class, c.license_no, c.contact_name, c.contact_phone,
	c.notes, c.is_active, c.created_at, c.updated_at`

func scan(row interface{ Scan(dest ...any) error }) (*domain.Company, error) {
	var c domain.Company
	err := row.Scan(
		&c.ID, &c.BranchID, &c.Name, &c.Phone, &c.Email, &c.Website, &c.Country, &c.City, &c.Address,
		&c.VehicleClass, &c.LicenseNo, &c.ContactName, &c.ContactPhone,
		&c.Notes, &c.IsActive, &c.CreatedAt, &c.UpdatedAt,
	)
	return &c, err
}

func args(c *domain.Company) []any {
	return []any{
		c.ID, c.Name, c.Phone, c.Email, c.Website, c.Country, c.City, c.Address,
		c.VehicleClass, c.LicenseNo, c.ContactName, c.ContactPhone,
		c.Notes, c.IsActive, c.UpdatedAt,
	}
}

func (r *Repository) Create(ctx context.Context, c *domain.Company) error {
	if err := pgscope.EnsureBranch(ctx, c.BranchID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO bus_companies (id, name, phone, email, website, country, city, address,
			vehicle_class, license_no, contact_name, contact_phone,
			notes, is_active, updated_at, branch_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		append(args(c), c.BranchID, c.CreatedAt)...)
	return err
}

func (r *Repository) Update(ctx context.Context, c *domain.Company) error {
	a := args(c)
	clause, a, err := pgscope.Clause(ctx, pgscope.Columns{Branch: "branch_id"}, a)
	if err != nil {
		return err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		UPDATE bus_companies SET name=$2, phone=$3, email=$4, website=$5, country=$6, city=$7, address=$8,
			vehicle_class=$9, license_no=$10, contact_name=$11, contact_phone=$12,
			notes=$13, is_active=$14, updated_at=$15
		WHERE id=$1`+clause, a...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("bus company")
	}
	return nil
}

func (r *Repository) Find(ctx context.Context, id uuid.UUID) (*domain.Company, error) {
	clause, a, err := pgscope.Clause(ctx, busScope, []any{id})
	if err != nil {
		return nil, err
	}
	c, err := scan(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT `+columns+` FROM bus_companies c WHERE c.id=$1`+clause, a...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("bus company")
	}
	return c, err
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Company, error) {
	args := []any{f.BranchID}
	where := []string{"c.branch_id=$1"}
	if f.ActiveOnly {
		where = append(where, "c.is_active")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(c.name ILIKE $%d OR c.city ILIKE $%d OR c.country ILIKE $%d OR c.phone ILIKE $%d OR c.contact_name ILIKE $%d)", n, n, n, n, n))
	}
	where, args, err := pgscope.Append(ctx, busScope, where, args)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT `+columns+` FROM bus_companies c
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY c.is_active DESC, lower(c.city), lower(c.name)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Company{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
