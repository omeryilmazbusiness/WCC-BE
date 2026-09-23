package tourpackage

import (
	"context"
	"errors"
	"fmt"
	"time"

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
			base_price, currency, is_active, sales_closed, soft_threshold_pct, allow_oversell,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		d.ID, d.PackageID, d.Code, d.DepartDate, d.ReturnDate, d.CapacityTotal, d.CapacitySold,
		d.BasePrice, d.Currency, d.IsActive, d.SalesClosed, d.SoftThresholdPct, d.AllowOversell,
		d.CreatedAt, d.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdateDeparture(ctx context.Context, d *domain.Departure) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE departures SET code=$2, depart_date=$3, return_date=$4, capacity_total=$5,
			base_price=$6, currency=$7, is_active=$8, sales_closed=$9, soft_threshold_pct=$10,
			allow_oversell=$11, updated_at=$12
		WHERE id=$1`,
		d.ID, d.Code, d.DepartDate, d.ReturnDate, d.CapacityTotal,
		d.BasePrice, d.Currency, d.IsActive, d.SalesClosed, d.SoftThresholdPct,
		d.AllowOversell, d.UpdatedAt,
	)
	return err
}

func scanDeparture(row pgx.Row) (*domain.Departure, error) {
	var d domain.Departure
	err := row.Scan(
		&d.ID, &d.PackageID, &d.Code, &d.DepartDate, &d.ReturnDate, &d.CapacityTotal, &d.CapacitySold,
		&d.BasePrice, &d.Currency, &d.IsActive, &d.SalesClosed, &d.SoftThresholdPct, &d.AllowOversell,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

const depCols = `id, package_id, code, depart_date, return_date, capacity_total, capacity_sold,
	base_price, currency, is_active, sales_closed, soft_threshold_pct, allow_oversell, created_at, updated_at`

func (r *Repository) FindDeparture(ctx context.Context, id uuid.UUID) (*domain.Departure, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	d, err := scanDeparture(q.QueryRow(ctx, `SELECT `+depCols+` FROM departures WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return d, err
}

func (r *Repository) UpdateDepartureCapacitySold(ctx context.Context, id uuid.UUID, sold int) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `UPDATE departures SET capacity_sold=$2, updated_at=NOW() WHERE id=$1`, id, sold)
	return err
}

func (r *Repository) ListDepartures(ctx context.Context, packageID uuid.UUID) ([]domain.Departure, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `SELECT `+depCols+` FROM departures WHERE package_id=$1 ORDER BY depart_date`, packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Departure
	for rows.Next() {
		d, err := scanDeparture(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *Repository) ReplacePackageTiers(ctx context.Context, packageID uuid.UUID, tiers []domain.PricingTier) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `DELETE FROM package_pricing_tiers WHERE package_id=$1`, packageID); err != nil {
		return err
	}
	now := time.Now().UTC()
	for i, t := range tiers {
		id := t.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO package_pricing_tiers (
				id, package_id, code, label, kind, amount, currency, sort_order, is_active, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			id, packageID, t.Code, t.Label, t.Kind, t.Amount, t.Currency, i, t.IsActive, now, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListPackageTiers(ctx context.Context, packageID uuid.UUID) ([]domain.PricingTier, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, package_id, code, label, kind, amount, currency, sort_order, is_active, created_at, updated_at
		FROM package_pricing_tiers WHERE package_id=$1 ORDER BY sort_order, code`, packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PricingTier
	for rows.Next() {
		var t domain.PricingTier
		if err := rows.Scan(&t.ID, &t.PackageID, &t.Code, &t.Label, &t.Kind, &t.Amount, &t.Currency,
			&t.SortOrder, &t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) SnapshotTiersToDeparture(ctx context.Context, packageID, departureID uuid.UUID) error {
	tiers, err := r.ListPackageTiers(ctx, packageID)
	if err != nil {
		return err
	}
	return r.ReplaceDepartureTiers(ctx, departureID, tiers)
}

func (r *Repository) ReplaceDepartureTiers(ctx context.Context, departureID uuid.UUID, tiers []domain.PricingTier) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `DELETE FROM departure_pricing_tiers WHERE departure_id=$1`, departureID); err != nil {
		return err
	}
	now := time.Now().UTC()
	for i, t := range tiers {
		id := uuid.New()
		depID := departureID
		if _, err := q.Exec(ctx, `
			INSERT INTO departure_pricing_tiers (
				id, departure_id, code, label, kind, amount, currency, sort_order, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			id, depID, t.Code, t.Label, t.Kind, t.Amount, t.Currency, i, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListDepartureTiers(ctx context.Context, departureID uuid.UUID) ([]domain.PricingTier, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, departure_id, code, label, kind, amount, currency, sort_order, created_at
		FROM departure_pricing_tiers WHERE departure_id=$1 ORDER BY sort_order, code`, departureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PricingTier
	for rows.Next() {
		var t domain.PricingTier
		var depID uuid.UUID
		var created time.Time
		if err := rows.Scan(&t.ID, &depID, &t.Code, &t.Label, &t.Kind, &t.Amount, &t.Currency, &t.SortOrder, &created); err != nil {
			return nil, err
		}
		t.DepartureID = &depID
		t.IsActive = true
		t.CreatedAt = created
		t.UpdatedAt = created
		out = append(out, t)
	}
	return out, rows.Err()
}
