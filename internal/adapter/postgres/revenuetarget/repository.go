package revenuetarget

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/revenuetarget"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const targetCols = `id, branch_id, owner_id, team_id, label, target_amount, currency,
	COALESCE(metric,'collected'), COALESCE(scope_type,'branch'), COALESCE(curve_type,'linear'),
	period_kind, period_start, period_end, created_by, created_at, COALESCE(updated_at, created_at)`

// targetScope renders visibility for revenue targets aliased t. Branch and
// global scopes see every target in reach; own scopes see branch-level goals
// plus targets they own or hold a share in; team scopes (managers) also see
// their team's targets and targets owned by team members.
func targetScope(ctx context.Context, args []any) (string, []any, error) {
	clause, args, err := pgscope.Clause(ctx, pgscope.Columns{Branch: "t.branch_id"}, args)
	if err != nil {
		return "", args, err
	}
	s := access.From(ctx)
	switch s.Level {
	case access.LevelOwn:
		args = append(args, s.UserID)
		clause += fmt.Sprintf(` AND (COALESCE(t.scope_type,'branch')='branch' OR t.owner_id=$%[1]d
			OR EXISTS (SELECT 1 FROM revenue_target_shares sh WHERE sh.target_id=t.id AND sh.user_id=$%[1]d))`, len(args))
	case access.LevelTeam:
		args = append(args, s.UserID, s.TeamID)
		clause += fmt.Sprintf(` AND (COALESCE(t.scope_type,'branch')='branch' OR t.team_id=$%[2]d OR t.owner_id=$%[1]d
			OR t.owner_id IN (SELECT id FROM users WHERE team_id=$%[2]d))`, len(args)-1, len(args))
	}
	return clause, args, nil
}

// targetVisible renders an EXISTS guard on the parent target of a child row.
func targetVisible(ctx context.Context, targetCol string, args []any) (string, []any, error) {
	clause, args, err := targetScope(ctx, args)
	if err != nil {
		return "", args, err
	}
	return ` AND EXISTS (SELECT 1 FROM revenue_targets t WHERE t.id=` + targetCol + clause + `)`, args, nil
}

func (r *Repository) ensureTarget(ctx context.Context, targetID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	guard, args, err := targetVisible(ctx, "$1", []any{targetID})
	if err != nil {
		return err
	}
	var ok bool
	err = q.QueryRow(ctx, `SELECT TRUE WHERE $1::uuid IS NOT NULL`+guard, args...).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewNotFound("revenue_target")
	}
	return err
}

func scanTarget(scan func(dest ...any) error) (*domain.Target, error) {
	var t domain.Target
	var metric, scope, curve, kind string
	err := scan(
		&t.ID, &t.BranchID, &t.OwnerID, &t.TeamID, &t.Label, &t.TargetAmount, &t.Currency,
		&metric, &scope, &curve, &kind, &t.PeriodStart, &t.PeriodEnd, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.Metric = domain.Metric(metric)
	t.ScopeType = domain.ScopeType(scope)
	t.CurveType = domain.CurveType(curve)
	t.PeriodKind = domain.PeriodKind(kind)
	return &t, nil
}

func (r *Repository) Create(ctx context.Context, t *domain.Target) error {
	if err := pgscope.EnsureBranch(ctx, t.BranchID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO revenue_targets (
			id, branch_id, owner_id, team_id, label, target_amount, currency,
			metric, scope_type, curve_type, period_start, period_end, created_by, created_at, updated_at,
			period_kind
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		t.ID, t.BranchID, t.OwnerID, t.TeamID, t.Label, t.TargetAmount, t.Currency,
		string(t.Metric), string(t.ScopeType), string(t.CurveType),
		t.PeriodStart, t.PeriodEnd, t.CreatedBy, t.CreatedAt, t.UpdatedAt, string(t.PeriodKind),
	)
	return err
}

func (r *Repository) Update(ctx context.Context, t *domain.Target) error {
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := targetScope(ctx, []any{
		t.ID, t.OwnerID, t.TeamID, t.Label, t.TargetAmount, t.Currency,
		string(t.Metric), string(t.ScopeType), string(t.CurveType),
		t.PeriodStart, t.PeriodEnd, t.UpdatedAt, string(t.PeriodKind),
	})
	if err != nil {
		return err
	}
	ct, err := q.Exec(ctx, `
		UPDATE revenue_targets t SET
			owner_id=$2, team_id=$3, label=$4, target_amount=$5, currency=$6,
			metric=$7, scope_type=$8, curve_type=$9, period_start=$10, period_end=$11, updated_at=$12,
			period_kind=$13
		WHERE t.id=$1`+clause, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return shared.NewNotFound("revenue_target")
	}
	return nil
}

// Delete removes a visible target; weights, shares, snapshots and revisions cascade.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := targetScope(ctx, []any{id})
	if err != nil {
		return err
	}
	ct, err := q.Exec(ctx, `DELETE FROM revenue_targets t WHERE t.id=$1`+clause, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return shared.NewNotFound("revenue_target")
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*domain.Target, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := targetScope(ctx, []any{id})
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `SELECT `+targetCols+` FROM revenue_targets t WHERE t.id=$1`+clause, args...)
	t, err := scanTarget(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// List returns visible targets; a nil branch means every branch in scope.
func (r *Repository) List(ctx context.Context, branchID *uuid.UUID) ([]domain.Target, error) {
	return r.listTargets(ctx, branchID)
}

func (r *Repository) ListTargetsForBranch(ctx context.Context, branchID uuid.UUID) ([]domain.Target, error) {
	return r.listTargets(ctx, &branchID)
}

func (r *Repository) listTargets(ctx context.Context, branchID *uuid.UUID) ([]domain.Target, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := targetScope(ctx, []any{branchID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT `+targetCols+` FROM revenue_targets t
		WHERE ($1::uuid IS NULL OR t.branch_id=$1)`+clause+`
		ORDER BY t.period_start DESC, t.label`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Target
	for rows.Next() {
		t, err := scanTarget(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *Repository) ReplaceWeights(ctx context.Context, targetID uuid.UUID, weights []domain.Weight) error {
	if err := r.ensureTarget(ctx, targetID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `DELETE FROM revenue_target_weights WHERE target_id=$1`, targetID); err != nil {
		return err
	}
	for _, w := range weights {
		if _, err := q.Exec(ctx, `
			INSERT INTO revenue_target_weights (id, target_id, bucket, weight_bps)
			VALUES ($1,$2,$3,$4)`, uuid.New(), targetID, w.Bucket, w.WeightBps); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListWeights(ctx context.Context, targetID uuid.UUID) ([]domain.Weight, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	guard, args, err := targetVisible(ctx, "w.target_id", []any{targetID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT w.bucket, w.weight_bps FROM revenue_target_weights w
		WHERE w.target_id=$1`+guard+` ORDER BY w.bucket`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Weight
	for rows.Next() {
		var w domain.Weight
		if err := rows.Scan(&w.Bucket, &w.WeightBps); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *Repository) ReplaceShares(ctx context.Context, targetID uuid.UUID, shares []domain.Share) error {
	if err := r.ensureTarget(ctx, targetID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `DELETE FROM revenue_target_shares WHERE target_id=$1`, targetID); err != nil {
		return err
	}
	for _, s := range shares {
		if _, err := q.Exec(ctx, `
			INSERT INTO revenue_target_shares (id, target_id, user_id, share_bps)
			VALUES ($1,$2,$3,$4)`, uuid.New(), targetID, s.UserID, s.ShareBps); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListShares(ctx context.Context, targetID uuid.UUID) ([]domain.Share, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	guard, args, err := targetVisible(ctx, "s.target_id", []any{targetID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT s.user_id, COALESCE(u.full_name,''), s.share_bps
		FROM revenue_target_shares s
		LEFT JOIN users u ON u.id = s.user_id
		WHERE s.target_id=$1`+guard+`
		ORDER BY s.share_bps DESC, u.full_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Share
	for rows.Next() {
		var s domain.Share
		if err := rows.Scan(&s.UserID, &s.UserName, &s.ShareBps); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertSnapshot(ctx context.Context, s *domain.Snapshot) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO revenue_target_snapshots (
			id, target_id, as_of, actual_amount, expected_to_date, variance,
			progress_bps, pace_bps, forecast_amount, status, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (target_id, as_of) DO UPDATE SET
			actual_amount=EXCLUDED.actual_amount,
			expected_to_date=EXCLUDED.expected_to_date,
			variance=EXCLUDED.variance,
			progress_bps=EXCLUDED.progress_bps,
			pace_bps=EXCLUDED.pace_bps,
			forecast_amount=EXCLUDED.forecast_amount,
			status=EXCLUDED.status,
			updated_at=EXCLUDED.updated_at`,
		uuid.New(), s.TargetID, s.AsOf, s.ActualAmount, s.ExpectedToDate, s.Variance,
		s.ProgressBps, s.PaceBps, s.ForecastAmount, string(s.Status), s.UpdatedAt,
	)
	return err
}

func (r *Repository) LatestSnapshot(ctx context.Context, targetID uuid.UUID) (*domain.Snapshot, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	guard, args, err := targetVisible(ctx, "sn.target_id", []any{targetID})
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `
		SELECT sn.target_id, sn.as_of, sn.actual_amount, sn.expected_to_date, sn.variance,
			sn.progress_bps, sn.pace_bps, sn.forecast_amount, sn.status, sn.updated_at
		FROM revenue_target_snapshots sn
		WHERE sn.target_id=$1`+guard+`
		ORDER BY sn.as_of DESC LIMIT 1`, args...)
	var s domain.Snapshot
	var status string
	err = row.Scan(
		&s.TargetID, &s.AsOf, &s.ActualAmount, &s.ExpectedToDate, &s.Variance,
		&s.ProgressBps, &s.PaceBps, &s.ForecastAmount, &status, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Status = domain.Status(status)
	return &s, nil
}

func (r *Repository) InsertRevision(ctx context.Context, rev *domain.Revision) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if rev.BeforeJSON == nil {
		rev.BeforeJSON = []byte("{}")
	}
	if rev.AfterJSON == nil {
		rev.AfterJSON = []byte("{}")
	}
	_, err := q.Exec(ctx, `
		INSERT INTO revenue_target_revisions (id, target_id, actor_id, action, before_json, after_json, created_at)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7)`,
		rev.ID, rev.TargetID, rev.ActorID, rev.Action, rev.BeforeJSON, rev.AfterJSON, rev.CreatedAt,
	)
	return err
}

func (r *Repository) ListRevisions(ctx context.Context, targetID uuid.UUID, limit int) ([]domain.Revision, error) {
	if limit <= 0 {
		limit = 50
	}
	q := tx.QuerierFrom(ctx, r.pool)
	guard, args, err := targetVisible(ctx, "rv.target_id", []any{targetID, limit})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT rv.id, rv.target_id, rv.actor_id, rv.action, rv.before_json, rv.after_json, rv.created_at
		FROM revenue_target_revisions rv
		WHERE rv.target_id=$1`+guard+`
		ORDER BY rv.created_at DESC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Revision
	for rows.Next() {
		var rev domain.Revision
		if err := rows.Scan(&rev.ID, &rev.TargetID, &rev.ActorID, &rev.Action, &rev.BeforeJSON, &rev.AfterJSON, &rev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func ownerFilter(t *domain.Target) (apply bool, ownerID uuid.UUID) {
	if t.ScopeType == domain.ScopeEmployee && t.OwnerID != nil {
		return true, *t.OwnerID
	}
	return false, uuid.Nil
}

// Actuals prefer the FX snapshot frozen on each row when it is already in the
// target currency ($2); other rows keep their own currency for the service to convert.
const (
	bookedCur = `CASE WHEN b.reporting_currency = $2 AND b.fx_rate_scaled IS NOT NULL THEN $2 ELSE COALESCE(b.currency, 'SAR') END`
	bookedAmt = `CASE WHEN b.reporting_currency = $2 AND b.fx_rate_scaled IS NOT NULL
		THEN ROUND(b.total_amount::numeric * b.fx_rate_scaled / 100000000)::bigint ELSE b.total_amount END`
	paidCur = `CASE WHEN p.reporting_currency = $2 AND p.amount_reporting IS NOT NULL THEN $2 ELSE p.currency END`
	paidAmt = `CASE WHEN p.reporting_currency = $2 AND p.amount_reporting IS NOT NULL THEN p.amount_reporting ELSE p.amount END`
)

// SumActual totals the metric over [from, to] by currency. Booked counts
// confirmed-or-later bookings by creation time; collected nets verified and
// approved ledger rows (charges, reversals, refunds) by their received date.
func (r *Repository) SumActual(ctx context.Context, t *domain.Target, from, to time.Time) ([]domain.CurrencyAmount, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	applyOwner, ownerID := ownerFilter(t)
	fromDay := from.UTC().Truncate(24 * time.Hour)
	toDay := to.UTC().Truncate(24 * time.Hour)
	args := []any{t.BranchID, t.Currency, fromDay, toDay}
	owner := ""
	if applyOwner {
		args = append(args, ownerID)
		owner = ` AND b.owner_id = $5`
	}

	var query string
	if t.Metric == domain.MetricBooked {
		query = `SELECT ` + bookedCur + `, COALESCE(SUM(` + bookedAmt + `), 0)
			FROM bookings b
			WHERE b.branch_id = $1` + owner + `
			  AND b.status IN ('confirmed','partially_paid','ready','travelled','completed')
			  AND b.created_at >= $3 AND b.created_at < $4::date + 1
			GROUP BY 1`
	} else {
		query = `SELECT ` + paidCur + `, COALESCE(SUM(` + paidAmt + `), 0)
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			WHERE b.branch_id = $1` + owner + `
			  AND p.status IN ('verified','approved')
			  AND p.received_at >= $3::date AND p.received_at <= $4::date
			GROUP BY 1`
	}
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CurrencyAmount
	for rows.Next() {
		var a domain.CurrencyAmount
		if err := rows.Scan(&a.Currency, &a.Minor); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) SumActualByOwner(ctx context.Context, t *domain.Target, from, to time.Time) ([]domain.Contribution, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	toExclusive := to.UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
	applyOwner, ownerID := ownerFilter(t)

	var rows pgx.Rows
	var err error
	if t.Metric == domain.MetricBooked {
		if applyOwner {
			rows, err = q.Query(ctx, `
				SELECT b.owner_id, COALESCE(u.full_name,''), COALESCE(SUM(b.total_amount),0)
				FROM bookings b
				LEFT JOIN users u ON u.id = b.owner_id
				WHERE b.branch_id=$1 AND b.owner_id=$2
				  AND b.status IN ('confirmed','partially_paid','ready','travelled','completed')
				  AND b.created_at >= $3 AND b.created_at < $4
				GROUP BY b.owner_id, u.full_name
				ORDER BY SUM(b.total_amount) DESC`, t.BranchID, ownerID, from, toExclusive)
		} else {
			rows, err = q.Query(ctx, `
				SELECT b.owner_id, COALESCE(u.full_name,''), COALESCE(SUM(b.total_amount),0)
				FROM bookings b
				LEFT JOIN users u ON u.id = b.owner_id
				WHERE b.branch_id=$1
				  AND b.status IN ('confirmed','partially_paid','ready','travelled','completed')
				  AND b.created_at >= $2 AND b.created_at < $3
				GROUP BY b.owner_id, u.full_name
				ORDER BY SUM(b.total_amount) DESC`, t.BranchID, from, toExclusive)
		}
	} else if applyOwner {
		rows, err = q.Query(ctx, `
			SELECT b.owner_id, COALESCE(u.full_name,''), COALESCE(SUM(p.amount),0)
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			LEFT JOIN users u ON u.id = b.owner_id
			WHERE b.branch_id=$1 AND b.owner_id=$2
			  AND p.status IN ('verified','approved')
			  AND p.created_at >= $3 AND p.created_at < $4
			GROUP BY b.owner_id, u.full_name
			ORDER BY SUM(p.amount) DESC`, t.BranchID, ownerID, from, toExclusive)
	} else {
		rows, err = q.Query(ctx, `
			SELECT b.owner_id, COALESCE(u.full_name,''), COALESCE(SUM(p.amount),0)
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			LEFT JOIN users u ON u.id = b.owner_id
			WHERE b.branch_id=$1
			  AND p.status IN ('verified','approved')
			  AND p.created_at >= $2 AND p.created_at < $3
			GROUP BY b.owner_id, u.full_name
			ORDER BY SUM(p.amount) DESC`, t.BranchID, from, toExclusive)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Contribution
	rank := 0
	for rows.Next() {
		rank++
		var c domain.Contribution
		if err := rows.Scan(&c.UserID, &c.UserName, &c.ActualAmount); err != nil {
			return nil, err
		}
		c.Rank = rank
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) ListSources(ctx context.Context, t *domain.Target, from, to time.Time, limit int) ([]domain.SourceRow, error) {
	if limit <= 0 {
		limit = 100
	}
	q := tx.QuerierFrom(ctx, r.pool)
	toExclusive := to.UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
	applyOwner, ownerID := ownerFilter(t)

	sqlBranch := `
		SELECT kind, id, booking_id, amount, currency, owner_id, owner_name, occurred_at, label FROM (
			SELECT 'booking'::text AS kind, b.id, b.id AS booking_id, b.total_amount AS amount,
				b.currency, b.owner_id, COALESCE(u.full_name,'') AS owner_name, b.created_at AS occurred_at,
				'Booking '||LEFT(b.id::text,8) AS label
			FROM bookings b
			LEFT JOIN users u ON u.id = b.owner_id
			WHERE b.branch_id=$1
			  AND b.status IN ('confirmed','partially_paid','ready','travelled','completed')
			  AND b.created_at >= $2 AND b.created_at < $3
			UNION ALL
			SELECT 'payment'::text, p.id, p.booking_id, p.amount, p.currency,
				b.owner_id, COALESCE(u.full_name,''), p.created_at,
				'Payment '||LEFT(p.id::text,8)
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			LEFT JOIN users u ON u.id = b.owner_id
			WHERE b.branch_id=$1
			  AND p.status IN ('verified','approved')
			  AND p.created_at >= $2 AND p.created_at < $3
		) src%s
		ORDER BY occurred_at DESC
		LIMIT $4`

	sqlOwner := `
		SELECT kind, id, booking_id, amount, currency, owner_id, owner_name, occurred_at, label FROM (
			SELECT 'booking'::text AS kind, b.id, b.id AS booking_id, b.total_amount AS amount,
				b.currency, b.owner_id, COALESCE(u.full_name,'') AS owner_name, b.created_at AS occurred_at,
				'Booking '||LEFT(b.id::text,8) AS label
			FROM bookings b
			LEFT JOIN users u ON u.id = b.owner_id
			WHERE b.branch_id=$1 AND b.owner_id=$2
			  AND b.status IN ('confirmed','partially_paid','ready','travelled','completed')
			  AND b.created_at >= $3 AND b.created_at < $4
			UNION ALL
			SELECT 'payment'::text, p.id, p.booking_id, p.amount, p.currency,
				b.owner_id, COALESCE(u.full_name,''), p.created_at,
				'Payment '||LEFT(p.id::text,8)
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			LEFT JOIN users u ON u.id = b.owner_id
			WHERE b.branch_id=$1 AND b.owner_id=$2
			  AND p.status IN ('verified','approved')
			  AND p.created_at >= $3 AND p.created_at < $4
		) src%s
		ORDER BY occurred_at DESC
		LIMIT $5`

	query, args := sqlBranch, []any{t.BranchID, from, toExclusive, limit}
	if applyOwner {
		query, args = sqlOwner, []any{t.BranchID, ownerID, from, toExclusive, limit}
	}
	// Source rows expose individual bookings, so own/team callers only see
	// rows owned by users inside their scope.
	set, args, restricted, err := pgscope.Owners(ctx, args)
	if err != nil {
		return nil, err
	}
	guard := ""
	if restricted {
		guard = " WHERE src.owner_id IN (" + set + ")"
	}
	rows, err := q.Query(ctx, fmt.Sprintf(query, guard), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SourceRow
	for rows.Next() {
		var row domain.SourceRow
		var at time.Time
		if err := rows.Scan(&row.Kind, &row.ID, &row.BookingID, &row.Amount, &row.Currency,
			&row.OwnerID, &row.OwnerName, &at, &row.Label); err != nil {
			return nil, err
		}
		row.OccurredAt = at.UTC().Format(time.RFC3339Nano)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) ManagerUserIDs(ctx context.Context, branchID uuid.UUID) ([]uuid.UUID, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	if err := pgscope.EnsureBranch(ctx, branchID); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT id FROM users
		WHERE branch_id=$1 AND is_active=TRUE AND role IN ('manager','gm')
		ORDER BY role, full_name`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
