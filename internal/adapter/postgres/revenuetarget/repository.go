package revenuetarget

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/revenuetarget"
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
	period_start, period_end, created_by, created_at, COALESCE(updated_at, created_at)`

func scanTarget(scan func(dest ...any) error) (*domain.Target, error) {
	var t domain.Target
	var metric, scope, curve string
	err := scan(
		&t.ID, &t.BranchID, &t.OwnerID, &t.TeamID, &t.Label, &t.TargetAmount, &t.Currency,
		&metric, &scope, &curve, &t.PeriodStart, &t.PeriodEnd, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.Metric = domain.Metric(metric)
	t.ScopeType = domain.ScopeType(scope)
	t.CurveType = domain.CurveType(curve)
	return &t, nil
}

func (r *Repository) Create(ctx context.Context, t *domain.Target) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO revenue_targets (
			id, branch_id, owner_id, team_id, label, target_amount, currency,
			metric, scope_type, curve_type, period_start, period_end, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		t.ID, t.BranchID, t.OwnerID, t.TeamID, t.Label, t.TargetAmount, t.Currency,
		string(t.Metric), string(t.ScopeType), string(t.CurveType),
		t.PeriodStart, t.PeriodEnd, t.CreatedBy, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, t *domain.Target) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE revenue_targets SET
			owner_id=$2, team_id=$3, label=$4, target_amount=$5, currency=$6,
			metric=$7, scope_type=$8, curve_type=$9, period_start=$10, period_end=$11, updated_at=$12
		WHERE id=$1`,
		t.ID, t.OwnerID, t.TeamID, t.Label, t.TargetAmount, t.Currency,
		string(t.Metric), string(t.ScopeType), string(t.CurveType),
		t.PeriodStart, t.PeriodEnd, t.UpdatedAt,
	)
	return err
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*domain.Target, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+targetCols+` FROM revenue_targets WHERE id=$1`, id)
	t, err := scanTarget(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (r *Repository) List(ctx context.Context, branchID uuid.UUID) ([]domain.Target, error) {
	return r.ListTargetsForBranch(ctx, branchID)
}

func (r *Repository) ListTargetsForBranch(ctx context.Context, branchID uuid.UUID) ([]domain.Target, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT `+targetCols+` FROM revenue_targets
		WHERE branch_id=$1
		ORDER BY period_start DESC, label`, branchID)
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
	rows, err := q.Query(ctx, `
		SELECT bucket, weight_bps FROM revenue_target_weights
		WHERE target_id=$1 ORDER BY bucket`, targetID)
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
	rows, err := q.Query(ctx, `
		SELECT s.user_id, COALESCE(u.full_name,''), s.share_bps
		FROM revenue_target_shares s
		LEFT JOIN users u ON u.id = s.user_id
		WHERE s.target_id=$1
		ORDER BY s.share_bps DESC, u.full_name`, targetID)
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
	row := q.QueryRow(ctx, `
		SELECT target_id, as_of, actual_amount, expected_to_date, variance,
			progress_bps, pace_bps, forecast_amount, status, updated_at
		FROM revenue_target_snapshots
		WHERE target_id=$1
		ORDER BY as_of DESC LIMIT 1`, targetID)
	var s domain.Snapshot
	var status string
	err := row.Scan(
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
	rows, err := q.Query(ctx, `
		SELECT id, target_id, actor_id, action, before_json, after_json, created_at
		FROM revenue_target_revisions
		WHERE target_id=$1
		ORDER BY created_at DESC LIMIT $2`, targetID, limit)
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

func (r *Repository) SumActual(ctx context.Context, t *domain.Target, from, to time.Time) (int64, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	applyOwner, ownerID := ownerFilter(t)
	toExclusive := to.UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)

	if t.Metric == domain.MetricBooked {
		var sum int64
		var err error
		if applyOwner {
			err = q.QueryRow(ctx, `
				SELECT COALESCE(SUM(total_amount),0) FROM bookings
				WHERE branch_id=$1 AND owner_id=$2
				  AND status IN ('confirmed','completed')
				  AND created_at >= $3 AND created_at < $4`,
				t.BranchID, ownerID, from, toExclusive).Scan(&sum)
		} else {
			err = q.QueryRow(ctx, `
				SELECT COALESCE(SUM(total_amount),0) FROM bookings
				WHERE branch_id=$1
				  AND status IN ('confirmed','completed')
				  AND created_at >= $2 AND created_at < $3`,
				t.BranchID, from, toExclusive).Scan(&sum)
		}
		return sum, err
	}

	// collected (default)
	var sum int64
	var err error
	if applyOwner {
		err = q.QueryRow(ctx, `
			SELECT COALESCE(SUM(p.amount),0)
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			WHERE b.branch_id=$1 AND b.owner_id=$2
			  AND p.status IN ('verified','approved')
			  AND p.created_at >= $3 AND p.created_at < $4`,
			t.BranchID, ownerID, from, toExclusive).Scan(&sum)
	} else {
		err = q.QueryRow(ctx, `
			SELECT COALESCE(SUM(p.amount),0)
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			WHERE b.branch_id=$1
			  AND p.status IN ('verified','approved')
			  AND p.created_at >= $2 AND p.created_at < $3`,
			t.BranchID, from, toExclusive).Scan(&sum)
	}
	return sum, err
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
				  AND b.status IN ('confirmed','completed')
				  AND b.created_at >= $3 AND b.created_at < $4
				GROUP BY b.owner_id, u.full_name
				ORDER BY SUM(b.total_amount) DESC`, t.BranchID, ownerID, from, toExclusive)
		} else {
			rows, err = q.Query(ctx, `
				SELECT b.owner_id, COALESCE(u.full_name,''), COALESCE(SUM(b.total_amount),0)
				FROM bookings b
				LEFT JOIN users u ON u.id = b.owner_id
				WHERE b.branch_id=$1
				  AND b.status IN ('confirmed','completed')
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
	toExclusive := to.UTC().Truncate(24 * time.Hour).AddDate(0, 0, 1)
	applyOwner, ownerID := ownerFilter(t)

	const sqlBranch = `
		SELECT kind, id, booking_id, amount, currency, owner_id, owner_name, occurred_at, label FROM (
			SELECT 'booking'::text AS kind, b.id, b.id AS booking_id, b.total_amount AS amount,
				b.currency, b.owner_id, COALESCE(u.full_name,'') AS owner_name, b.created_at AS occurred_at,
				'Booking '||LEFT(b.id::text,8) AS label
			FROM bookings b
			LEFT JOIN users u ON u.id = b.owner_id
			WHERE b.branch_id=$1
			  AND b.status IN ('confirmed','completed')
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
		) src
		ORDER BY occurred_at DESC
		LIMIT $4`

	const sqlOwner = `
		SELECT kind, id, booking_id, amount, currency, owner_id, owner_name, occurred_at, label FROM (
			SELECT 'booking'::text AS kind, b.id, b.id AS booking_id, b.total_amount AS amount,
				b.currency, b.owner_id, COALESCE(u.full_name,'') AS owner_name, b.created_at AS occurred_at,
				'Booking '||LEFT(b.id::text,8) AS label
			FROM bookings b
			LEFT JOIN users u ON u.id = b.owner_id
			WHERE b.branch_id=$1 AND b.owner_id=$2
			  AND b.status IN ('confirmed','completed')
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
		) src
		ORDER BY occurred_at DESC
		LIMIT $5`

	var rows pgx.Rows
	var err error
	if applyOwner {
		rows, err = q.Query(ctx, sqlOwner, t.BranchID, ownerID, from, toExclusive, limit)
	} else {
		rows, err = q.Query(ctx, sqlBranch, t.BranchID, from, toExclusive, limit)
	}
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
