package tourpackage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreatePackage(ctx context.Context, p *domain.Package) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO packages (id, branch_id, code, name_en, name_ar, description, is_active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		p.ID, p.BranchID, p.Code, p.NameEN, p.NameAR, p.Description, p.IsActive, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdatePackage(ctx context.Context, p *domain.Package) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE packages SET code=$2, name_en=$3, name_ar=$4, description=$5, is_active=$6, updated_at=$7
		WHERE id=$1`,
		p.ID, p.Code, p.NameEN, p.NameAR, p.Description, p.IsActive, p.UpdatedAt,
	)
	return err
}

func (r *Repository) FindPackage(ctx context.Context, id uuid.UUID) (*domain.Package, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, code, name_en, name_ar, description, is_active, created_at, updated_at
		FROM packages WHERE id=$1`, id)
	var p domain.Package
	err := row.Scan(&p.ID, &p.BranchID, &p.Code, &p.NameEN, &p.NameAR, &p.Description, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return &p, err
}

func (r *Repository) ListPackages(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]domain.Package, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, code, name_en, name_ar, description, is_active, created_at, updated_at
		FROM packages
		WHERE branch_id=$1 AND ($2::bool = FALSE OR is_active = TRUE)
		ORDER BY code`, branchID, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Package
	for rows.Next() {
		var p domain.Package
		if err := rows.Scan(&p.ID, &p.BranchID, &p.Code, &p.NameEN, &p.NameAR, &p.Description, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) CreateDeparture(ctx context.Context, d *domain.Departure) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO departures (
			id, package_id, code, depart_date, return_date, capacity_total, capacity_sold,
			base_price, currency, is_active, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		d.ID, d.PackageID, d.Code, d.DepartDate, d.ReturnDate, d.CapacityTotal, d.CapacitySold,
		d.BasePrice, d.Currency, d.IsActive, d.CreatedAt, d.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdateDeparture(ctx context.Context, d *domain.Departure) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE departures SET code=$2, depart_date=$3, return_date=$4, capacity_total=$5,
			base_price=$6, currency=$7, is_active=$8, updated_at=$9
		WHERE id=$1`,
		d.ID, d.Code, d.DepartDate, d.ReturnDate, d.CapacityTotal,
		d.BasePrice, d.Currency, d.IsActive, d.UpdatedAt,
	)
	return err
}

func (r *Repository) FindDeparture(ctx context.Context, id uuid.UUID) (*domain.Departure, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, package_id, code, depart_date, return_date, capacity_total, capacity_sold,
			base_price, currency, is_active, created_at, updated_at
		FROM departures WHERE id=$1`, id)
	var d domain.Departure
	err := row.Scan(&d.ID, &d.PackageID, &d.Code, &d.DepartDate, &d.ReturnDate, &d.CapacityTotal, &d.CapacitySold,
		&d.BasePrice, &d.Currency, &d.IsActive, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return &d, err
}

func (r *Repository) UpdateDepartureCapacitySold(ctx context.Context, id uuid.UUID, sold int) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `UPDATE departures SET capacity_sold=$2, updated_at=NOW() WHERE id=$1`, id, sold)
	return err
}

func (r *Repository) ListDepartures(ctx context.Context, packageID uuid.UUID) ([]domain.Departure, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, package_id, code, depart_date, return_date, capacity_total, capacity_sold,
			base_price, currency, is_active, created_at, updated_at
		FROM departures WHERE package_id=$1 ORDER BY depart_date`, packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Departure
	for rows.Next() {
		var d domain.Departure
		if err := rows.Scan(&d.ID, &d.PackageID, &d.Code, &d.DepartDate, &d.ReturnDate, &d.CapacityTotal, &d.CapacitySold,
			&d.BasePrice, &d.Currency, &d.IsActive, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

