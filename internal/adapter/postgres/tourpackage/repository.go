package tourpackage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

var packageScope = pgscope.Columns{Branch: "branch_id"}

// packageVisible renders an EXISTS guard on the parent package of a child row.
func packageVisible(ctx context.Context, packageCol string, args []any) (string, []any, error) {
	clause, args, err := pgscope.Clause(ctx, pgscope.Columns{Branch: "pk.branch_id"}, args)
	if err != nil {
		return "", args, err
	}
	return ` AND EXISTS (SELECT 1 FROM packages pk WHERE pk.id=` + packageCol + clause + `)`, args, nil
}

// departureVisible renders an EXISTS guard on a departure through its package.
func departureVisible(ctx context.Context, departureCol string, args []any) (string, []any, error) {
	clause, args, err := pgscope.Clause(ctx, pgscope.Columns{Branch: "pk.branch_id"}, args)
	if err != nil {
		return "", args, err
	}
	return ` AND EXISTS (SELECT 1 FROM departures dp JOIN packages pk ON pk.id=dp.package_id
		WHERE dp.id=` + departureCol + clause + `)`, args, nil
}

func (r *Repository) ensureVisible(ctx context.Context, guard func(context.Context, string, []any) (string, []any, error), id uuid.UUID, entity string) error {
	q := tx.QuerierFrom(ctx, r.pool)
	g, args, err := guard(ctx, "$1", []any{id})
	if err != nil {
		return err
	}
	var ok bool
	err = q.QueryRow(ctx, `SELECT TRUE WHERE $1::uuid IS NOT NULL`+g, args...).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewNotFound(entity)
	}
	return err
}

const packageColumns = `p.id, p.branch_id, p.code, p.name_en, p.name_ar, p.description, p.is_active,
	p.kind, p.category, p.duration_days, p.transport_mode, p.capacity_total, p.sales_open, p.base_currency, p.spec,
	p.created_at, p.updated_at,
	COALESCE(ds.departures, 0), COALESCE(ds.reserved, 0), COALESCE(ds.seats, 0), ds.next_depart,
	COALESCE(fp.amount, 0), COALESCE(fp.currency, '')`

// packageJoins aggregates active departures and the cheapest active room tier per package.
const packageJoins = `
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS departures,
		       SUM(d.capacity_sold) AS reserved,
		       SUM(d.capacity_total) AS seats,
		       MIN(d.depart_date) FILTER (WHERE d.depart_date >= CURRENT_DATE) AS next_depart
		FROM departures d WHERE d.package_id = p.id AND d.is_active
	) ds ON TRUE
	LEFT JOIN LATERAL (
		SELECT t.amount, t.currency FROM package_pricing_tiers t
		WHERE t.package_id = p.id AND t.is_active AND t.kind = 'room' AND t.amount > 0
		ORDER BY t.amount LIMIT 1
	) fp ON TRUE`

type rowScanner interface{ Scan(dest ...any) error }

func scanPackage(row rowScanner) (*domain.Package, error) {
	var p domain.Package
	var spec []byte
	var next *time.Time
	err := row.Scan(&p.ID, &p.BranchID, &p.Code, &p.NameEN, &p.NameAR, &p.Description, &p.IsActive,
		&p.Kind, &p.Category, &p.DurationDays, &p.TransportMode, &p.CapacityTotal, &p.SalesOpen, &p.BaseCurrency, &spec,
		&p.CreatedAt, &p.UpdatedAt,
		&p.Stats.Departures, &p.Stats.Reserved, &p.Stats.DepartureSeats, &next,
		&p.Stats.FromPrice, &p.Stats.FromCurrency)
	if err != nil {
		return nil, err
	}
	p.Stats.NextDepartDate = next
	if len(spec) > 0 {
		if err := json.Unmarshal(spec, &p.Spec); err != nil {
			return nil, fmt.Errorf("package %s spec: %w", p.ID, err)
		}
	}
	return &p, nil
}

func (r *Repository) CreatePackage(ctx context.Context, p *domain.Package) error {
	if err := pgscope.EnsureBranch(ctx, p.BranchID); err != nil {
		return err
	}
	spec, err := json.Marshal(p.Spec)
	if err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err = q.Exec(ctx, `
		INSERT INTO packages (id, branch_id, code, name_en, name_ar, description, is_active,
			kind, category, duration_days, transport_mode, capacity_total, sales_open, base_currency, spec,
			created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		p.ID, p.BranchID, p.Code, p.NameEN, p.NameAR, p.Description, p.IsActive,
		p.Kind, p.Category, p.DurationDays, p.TransportMode, p.CapacityTotal, p.SalesOpen, p.BaseCurrency, spec,
		p.CreatedAt, p.UpdatedAt,
	)
	return mapUniqueCode(err)
}

func (r *Repository) UpdatePackage(ctx context.Context, p *domain.Package) error {
	spec, err := json.Marshal(p.Spec)
	if err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := pgscope.Clause(ctx, packageScope, []any{
		p.ID, p.Code, p.NameEN, p.NameAR, p.Description, p.IsActive, p.UpdatedAt,
		p.Kind, p.Category, p.DurationDays, p.TransportMode, p.CapacityTotal, p.SalesOpen, p.BaseCurrency, spec,
	})
	if err != nil {
		return err
	}
	ct, err := q.Exec(ctx, `
		UPDATE packages SET code=$2, name_en=$3, name_ar=$4, description=$5, is_active=$6, updated_at=$7,
			kind=$8, category=$9, duration_days=$10, transport_mode=$11, capacity_total=$12,
			sales_open=$13, base_currency=$14, spec=$15
		WHERE id=$1`+clause, args...)
	if err != nil {
		return mapUniqueCode(err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) FindPackage(ctx context.Context, id uuid.UUID) (*domain.Package, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := pgscope.Clause(ctx, pgscope.Columns{Branch: "p.branch_id"}, []any{id})
	if err != nil {
		return nil, err
	}
	p, err := scanPackage(q.QueryRow(ctx, `SELECT `+packageColumns+` FROM packages p`+packageJoins+`
		WHERE p.id=$1`+clause, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return p, err
}

func (r *Repository) ListPackages(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]domain.Package, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := pgscope.Clause(ctx, pgscope.Columns{Branch: "p.branch_id"}, []any{branchID, activeOnly})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+packageColumns+` FROM packages p`+packageJoins+`
		WHERE p.branch_id=$1 AND ($2::bool = FALSE OR p.is_active = TRUE)`+clause+`
		ORDER BY p.is_active DESC, ds.next_depart NULLS LAST, p.code`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Package
	for rows.Next() {
		p, err := scanPackage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// mapUniqueCode turns the (branch_id, code) unique violation into a field error.
func mapUniqueCode(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		e := shared.NewConflict("package code already exists")
		e.Details = map[string]any{"code": "package code already exists"}
		return e
	}
	return err
}

func (r *Repository) CreateDeparture(ctx context.Context, d *domain.Departure) error {
	if err := r.ensureVisible(ctx, packageVisible, d.PackageID, "package"); err != nil {
		return err
	}
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
	guard, args, err := packageVisible(ctx, "d.package_id", []any{
		d.ID, d.Code, d.DepartDate, d.ReturnDate, d.CapacityTotal,
		d.BasePrice, d.Currency, d.IsActive, d.SalesClosed, d.SoftThresholdPct,
		d.AllowOversell, d.UpdatedAt,
	})
	if err != nil {
		return err
	}
	ct, err := q.Exec(ctx, `
		UPDATE departures d SET code=$2, depart_date=$3, return_date=$4, capacity_total=$5,
			base_price=$6, currency=$7, is_active=$8, sales_closed=$9, soft_threshold_pct=$10,
			allow_oversell=$11, updated_at=$12
		WHERE d.id=$1`+guard, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
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

const depCols = `d.id, d.package_id, d.code, d.depart_date, d.return_date, d.capacity_total, d.capacity_sold,
	d.base_price, d.currency, d.is_active, d.sales_closed, d.soft_threshold_pct, d.allow_oversell, d.created_at, d.updated_at`

func (r *Repository) FindDeparture(ctx context.Context, id uuid.UUID) (*domain.Departure, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	guard, args, err := packageVisible(ctx, "d.package_id", []any{id})
	if err != nil {
		return nil, err
	}
	d, err := scanDeparture(q.QueryRow(ctx, `SELECT `+depCols+` FROM departures d WHERE d.id=$1`+guard, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return d, err
}

func (r *Repository) UpdateDepartureCapacitySold(ctx context.Context, id uuid.UUID, sold int) error {
	q := tx.QuerierFrom(ctx, r.pool)
	guard, args, err := packageVisible(ctx, "d.package_id", []any{id, sold})
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE departures d SET capacity_sold=$2, updated_at=NOW() WHERE d.id=$1`+guard, args...)
	return err
}

func (r *Repository) ListDepartures(ctx context.Context, packageID uuid.UUID) ([]domain.Departure, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	guard, args, err := packageVisible(ctx, "d.package_id", []any{packageID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+depCols+` FROM departures d WHERE d.package_id=$1`+guard+` ORDER BY d.depart_date`, args...)
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
	if err := r.ensureVisible(ctx, packageVisible, packageID, "package"); err != nil {
		return err
	}
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
	guard, args, err := packageVisible(ctx, "t.package_id", []any{packageID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT t.id, t.package_id, t.code, t.label, t.kind, t.amount, t.currency, t.sort_order, t.is_active, t.created_at, t.updated_at
		FROM package_pricing_tiers t WHERE t.package_id=$1`+guard+` ORDER BY t.sort_order, t.code`, args...)
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
	if err := r.ensureVisible(ctx, departureVisible, departureID, "departure"); err != nil {
		return err
	}
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
	guard, args, err := departureVisible(ctx, "t.departure_id", []any{departureID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT t.id, t.departure_id, t.code, t.label, t.kind, t.amount, t.currency, t.sort_order, t.created_at
		FROM departure_pricing_tiers t WHERE t.departure_id=$1`+guard+` ORDER BY t.sort_order, t.code`, args...)
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
