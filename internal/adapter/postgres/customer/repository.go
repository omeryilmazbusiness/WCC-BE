package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, c *domain.Customer) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO customers (
			id, branch_id, full_name, full_name_ar, phone, email, nationality, notes, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		c.ID, c.BranchID, c.FullName, c.FullNameAR, c.Phone, c.Email, c.Nationality, c.Notes,
		c.CreatedBy, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, c *domain.Customer) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE customers SET full_name=$2, full_name_ar=$3, phone=$4, email=$5, nationality=$6, notes=$7, updated_at=$8
		WHERE id=$1`,
		c.ID, c.FullName, c.FullNameAR, c.Phone, c.Email, c.Nationality, c.Notes, c.UpdatedAt,
	)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Customer, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, full_name, full_name_ar, phone, email, nationality, notes, created_by, created_at, updated_at
		FROM customers WHERE id=$1`, id)
	return scan(row)
}

func (r *Repository) FindByPhone(ctx context.Context, phone string, branchID uuid.UUID) (*domain.Customer, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, full_name, full_name_ar, phone, email, nationality, notes, created_by, created_at, updated_at
		FROM customers WHERE phone=$1 AND branch_id=$2 LIMIT 1`, phone, branchID)
	c, err := scan(row)
	if errors.Is(err, pgx.ErrNoRows) || (err != nil && errors.Is(err, pgx.ErrNoRows)) {
		return nil, pgx.ErrNoRows
	}
	return c, err
}

func (r *Repository) Search(ctx context.Context, f domain.SearchFilter) ([]domain.Customer, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	pattern := "%" + f.Query + "%"
	var total int
	countSQL := `SELECT COUNT(*) FROM customers WHERE ($1 = '' OR full_name ILIKE $2 OR phone ILIKE $2 OR full_name_ar ILIKE $2)
		AND ($3::uuid IS NULL OR branch_id = $3)`
	if err := q.QueryRow(ctx, countSQL, f.Query, pattern, f.BranchID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, full_name, full_name_ar, phone, email, nationality, notes, created_by, created_at, updated_at
		FROM customers
		WHERE ($1 = '' OR full_name ILIKE $2 OR phone ILIKE $2 OR full_name_ar ILIKE $2)
		  AND ($3::uuid IS NULL OR branch_id = $3)
		ORDER BY updated_at DESC
		LIMIT $4 OFFSET $5`, f.Query, pattern, f.BranchID, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Customer
	for rows.Next() {
		c, err := scanRows(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

func scan(row pgx.Row) (*domain.Customer, error) {
	var c domain.Customer
	err := row.Scan(&c.ID, &c.BranchID, &c.FullName, &c.FullNameAR, &c.Phone, &c.Email, &c.Nationality, &c.Notes, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return &c, err
}

func scanRows(rows pgx.Rows) (*domain.Customer, error) {
	var c domain.Customer
	err := rows.Scan(&c.ID, &c.BranchID, &c.FullName, &c.FullNameAR, &c.Phone, &c.Email, &c.Nationality, &c.Notes, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	return &c, err
}
