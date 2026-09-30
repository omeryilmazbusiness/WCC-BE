package lead

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var (
	scopeAliased = pgscope.Columns{Branch: "l.branch_id", Owner: "l.owner_id"}
	scopeBare    = pgscope.Columns{Branch: "branch_id", Owner: "owner_id"}
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const leadCols = `l.id, l.branch_id, l.customer_id, l.full_name, l.phone, l.source, l.stage, l.owner_id,
	COALESCE(u.full_name, ''), l.lost_reason_code, l.lost_reason, l.notes, l.no_follow_up,
	l.converted_booking_id, l.created_at, l.updated_at,
	l.travel_date, l.travel_window, l.pax_count, l.budget_amount, l.budget_currency, l.package_id, l.package_interest`

func scanLead(row pgx.Row) (*domain.Lead, error) {
	var l domain.Lead
	var stage string
	err := row.Scan(
		&l.ID, &l.BranchID, &l.CustomerID, &l.FullName, &l.Phone, &l.Source, &stage, &l.OwnerID,
		&l.OwnerName, &l.LostReasonCode, &l.LostReason, &l.Notes, &l.NoFollowUp,
		&l.ConvertedBookingID, &l.CreatedAt, &l.UpdatedAt,
		&l.Interest.TravelDate, &l.Interest.TravelWindow, &l.Interest.PaxCount, &l.Interest.BudgetAmount,
		&l.Interest.BudgetCurrency, &l.Interest.PackageID, &l.Interest.PackageInterest,
	)
	if err != nil {
		return nil, err
	}
	l.Stage = domain.Stage(stage)
	return &l, nil
}

func (r *Repository) Create(ctx context.Context, l *domain.Lead) error {
	if err := pgscope.EnsureBranch(ctx, l.BranchID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO leads (
			id, branch_id, customer_id, full_name, phone, source, stage, owner_id,
			lost_reason_code, lost_reason, notes, no_follow_up, converted_booking_id, created_at, updated_at,
			travel_date, travel_window, pax_count, budget_amount, budget_currency, package_id, package_interest
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
		l.ID, l.BranchID, l.CustomerID, l.FullName, l.Phone, l.Source, l.Stage, l.OwnerID,
		l.LostReasonCode, l.LostReason, l.Notes, l.NoFollowUp, l.ConvertedBookingID, l.CreatedAt, l.UpdatedAt,
		l.Interest.TravelDate, l.Interest.TravelWindow, l.Interest.PaxCount, l.Interest.BudgetAmount,
		l.Interest.BudgetCurrency, l.Interest.PackageID, l.Interest.PackageInterest,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, l *domain.Lead) error {
	q := tx.QuerierFrom(ctx, r.pool)
	args := []any{
		l.ID, l.CustomerID, l.FullName, l.Phone, l.Source, l.Stage, l.OwnerID,
		l.LostReasonCode, l.LostReason, l.Notes, l.NoFollowUp, l.ConvertedBookingID, l.UpdatedAt,
		l.Interest.TravelDate, l.Interest.TravelWindow, l.Interest.PaxCount, l.Interest.BudgetAmount,
		l.Interest.BudgetCurrency, l.Interest.PackageID, l.Interest.PackageInterest,
	}
	scope, args, err := pgscope.Clause(ctx, scopeBare, args)
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE leads SET customer_id=$2, full_name=$3, phone=$4, source=$5, stage=$6, owner_id=$7,
			lost_reason_code=$8, lost_reason=$9, notes=$10, no_follow_up=$11, converted_booking_id=$12, updated_at=$13,
			travel_date=$14, travel_window=$15, pax_count=$16, budget_amount=$17, budget_currency=$18,
			package_id=$19, package_interest=$20
		WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("lead")
	}
	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Lead, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeAliased, []any{id})
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `
		SELECT `+leadCols+`
		FROM leads l
		LEFT JOIN users u ON u.id = l.owner_id
		WHERE l.id=$1`+scope, args...)
	l, err := scanLead(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return l, err
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Lead, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where := []string{"1=1"}
	args := []any{}
	if f.BranchID != nil {
		args = append(args, *f.BranchID)
		where = append(where, fmt.Sprintf("l.branch_id=$%d", len(args)))
	}
	if f.OwnerID != nil {
		args = append(args, *f.OwnerID)
		where = append(where, fmt.Sprintf("l.owner_id=$%d", len(args)))
	}
	if f.Stage != "" {
		args = append(args, string(f.Stage))
		where = append(where, fmt.Sprintf("l.stage=$%d", len(args)))
	}
	if f.NoFollowUp != nil {
		args = append(args, *f.NoFollowUp)
		where = append(where, fmt.Sprintf("l.no_follow_up=$%d", len(args)))
	}
	if qstr := strings.TrimSpace(f.Query); qstr != "" {
		args = append(args, "%"+qstr+"%")
		i := len(args)
		where = append(where, fmt.Sprintf(
			`(l.full_name ILIKE $%d OR l.phone ILIKE $%d OR l.source ILIKE $%d OR COALESCE(u.full_name,'') ILIKE $%d)`,
			i, i, i, i,
		))
	}
	where, args, err := pgscope.Append(ctx, scopeAliased, where, args)
	if err != nil {
		return nil, 0, err
	}
	i := len(args) + 1
	clause := strings.Join(where, " AND ")
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}

	countSQL := `SELECT COUNT(*) FROM leads l LEFT JOIN users u ON u.id = l.owner_id WHERE ` + clause
	var total int
	if err := q.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listSQL := fmt.Sprintf(`
		SELECT %s FROM leads l
		LEFT JOIN users u ON u.id = l.owner_id
		WHERE %s
		ORDER BY l.updated_at DESC
		LIMIT $%d OFFSET $%d`, leadCols, clause, i, i+1)
	args = append(args, limit, offset)
	rows, err := q.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Lead
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *l)
	}
	return out, total, rows.Err()
}

func (r *Repository) AppendStageHistory(ctx context.Context, h *domain.StageHistory) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO lead_stage_history (id, lead_id, from_stage, to_stage, changed_by, note, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		h.ID, h.LeadID, h.FromStage, h.ToStage, h.ChangedBy, h.Note, h.CreatedAt,
	)
	return err
}

// LostBetween lists leads of a branch that are lost and entered the lost
// stage in [from, to), latest first.
func (r *Repository) LostBetween(ctx context.Context, branchID uuid.UUID, from, to time.Time, limit int) ([]domain.LostRecord, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeAliased, []any{branchID, from, to, limit})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT code, note, source, from_stage, lost_at FROM (
			SELECT DISTINCT ON (l.id) l.lost_reason_code AS code, l.lost_reason AS note, l.source,
			       COALESCE(h.from_stage, '') AS from_stage, h.created_at AS lost_at
			FROM leads l
			JOIN lead_stage_history h ON h.lead_id = l.id AND h.to_stage = 'lost'
			WHERE l.branch_id = $1 AND l.stage = 'lost' AND h.created_at >= $2 AND h.created_at < $3`+scope+`
			ORDER BY l.id, h.created_at DESC
		) lost
		ORDER BY lost_at DESC LIMIT $4`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LostRecord
	for rows.Next() {
		var rec domain.LostRecord
		if err := rows.Scan(&rec.ReasonCode, &rec.Note, &rec.Source, &rec.FromStage, &rec.LostAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *Repository) ListStageHistory(ctx context.Context, leadID uuid.UUID) ([]domain.StageHistory, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeAliased, []any{leadID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT h.id, h.lead_id, h.from_stage, h.to_stage, h.changed_by, h.note, h.created_at
		FROM lead_stage_history h
		WHERE h.lead_id=$1 AND EXISTS (SELECT 1 FROM leads l WHERE l.id = h.lead_id`+scope+`)
		ORDER BY h.created_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.StageHistory
	for rows.Next() {
		var h domain.StageHistory
		var from *string
		var to string
		if err := rows.Scan(&h.ID, &h.LeadID, &from, &to, &h.ChangedBy, &h.Note, &h.CreatedAt); err != nil {
			return nil, err
		}
		if from != nil {
			s := domain.Stage(*from)
			h.FromStage = &s
		}
		h.ToStage = domain.Stage(to)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *Repository) Analytics(ctx context.Context, branchID *uuid.UUID) (*domain.Analytics, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	a := &domain.Analytics{}
	where := []string{"1=1"}
	var args []any
	if branchID != nil {
		args = append(args, *branchID)
		where = append(where, fmt.Sprintf("l.branch_id=$%d", len(args)))
	}
	where, args, err := pgscope.Append(ctx, scopeAliased, where, args)
	if err != nil {
		return nil, err
	}
	clause := strings.Join(where, " AND ")

	if err := q.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE stage NOT IN ('won','lost')),
			COUNT(*) FILTER (WHERE stage = 'won'),
			COUNT(*) FILTER (WHERE stage = 'lost'),
			COUNT(*) FILTER (WHERE no_follow_up AND stage NOT IN ('won','lost'))
		FROM leads l WHERE `+clause, args...).Scan(
		&a.Total, &a.Open, &a.Won, &a.Lost, &a.NoFollowUp,
	); err != nil {
		return nil, err
	}
	closed := a.Won + a.Lost
	if closed > 0 {
		a.ConversionRate = float64(a.Won) / float64(closed)
	}

	stageRows, err := q.Query(ctx, `
		SELECT l.stage, COUNT(*) FROM leads l WHERE `+clause+` GROUP BY l.stage ORDER BY l.stage`, args...)
	if err != nil {
		return nil, err
	}
	defer stageRows.Close()
	for stageRows.Next() {
		var b domain.CountBucket
		if err := stageRows.Scan(&b.Key, &b.Count); err != nil {
			return nil, err
		}
		a.ByStage = append(a.ByStage, b)
	}

	srcRows, err := q.Query(ctx, `
		SELECT COALESCE(NULLIF(source,''),'(unknown)'), COUNT(*),
			COUNT(*) FILTER (WHERE stage='won')
		FROM leads l WHERE `+clause+` GROUP BY 1 ORDER BY 2 DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer srcRows.Close()
	for srcRows.Next() {
		var b domain.SourceBucket
		if err := srcRows.Scan(&b.Source, &b.Count, &b.Won); err != nil {
			return nil, err
		}
		a.BySource = append(a.BySource, b)
	}

	ownRows, err := q.Query(ctx, `
		SELECT l.owner_id, COALESCE(u.full_name,''), COUNT(*),
			COUNT(*) FILTER (WHERE l.stage='won'),
			COUNT(*) FILTER (WHERE l.stage='lost')
		FROM leads l
		LEFT JOIN users u ON u.id = l.owner_id
		WHERE `+clause+`
		GROUP BY l.owner_id, u.full_name
		ORDER BY COUNT(*) DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer ownRows.Close()
	for ownRows.Next() {
		var b domain.OwnerBucket
		if err := ownRows.Scan(&b.OwnerID, &b.OwnerName, &b.Count, &b.Won, &b.Lost); err != nil {
			return nil, err
		}
		a.ByOwner = append(a.ByOwner, b)
	}
	return a, nil
}
