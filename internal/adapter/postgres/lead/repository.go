package lead

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const leadCols = `l.id, l.branch_id, l.customer_id, l.full_name, l.phone, l.source, l.stage, l.owner_id,
	COALESCE(u.full_name, ''), l.lost_reason_code, l.lost_reason, l.notes, l.no_follow_up,
	l.converted_booking_id, l.created_at, l.updated_at`

func scanLead(row pgx.Row) (*domain.Lead, error) {
	var l domain.Lead
	var stage string
	err := row.Scan(
		&l.ID, &l.BranchID, &l.CustomerID, &l.FullName, &l.Phone, &l.Source, &stage, &l.OwnerID,
		&l.OwnerName, &l.LostReasonCode, &l.LostReason, &l.Notes, &l.NoFollowUp,
		&l.ConvertedBookingID, &l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	l.Stage = domain.Stage(stage)
	return &l, nil
}

func (r *Repository) Create(ctx context.Context, l *domain.Lead) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO leads (
			id, branch_id, customer_id, full_name, phone, source, stage, owner_id,
			lost_reason_code, lost_reason, notes, no_follow_up, converted_booking_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		l.ID, l.BranchID, l.CustomerID, l.FullName, l.Phone, l.Source, l.Stage, l.OwnerID,
		l.LostReasonCode, l.LostReason, l.Notes, l.NoFollowUp, l.ConvertedBookingID, l.CreatedAt, l.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, l *domain.Lead) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE leads SET customer_id=$2, full_name=$3, phone=$4, source=$5, stage=$6, owner_id=$7,
			lost_reason_code=$8, lost_reason=$9, notes=$10, no_follow_up=$11, converted_booking_id=$12, updated_at=$13
		WHERE id=$1`,
		l.ID, l.CustomerID, l.FullName, l.Phone, l.Source, l.Stage, l.OwnerID,
		l.LostReasonCode, l.LostReason, l.Notes, l.NoFollowUp, l.ConvertedBookingID, l.UpdatedAt,
	)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Lead, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT `+leadCols+`
		FROM leads l
		LEFT JOIN users u ON u.id = l.owner_id
		WHERE l.id=$1`, id)
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
	i := 1
	if f.BranchID != nil {
		where = append(where, fmt.Sprintf("l.branch_id=$%d", i))
		args = append(args, *f.BranchID)
		i++
	}
	if f.OwnerID != nil {
		where = append(where, fmt.Sprintf("l.owner_id=$%d", i))
		args = append(args, *f.OwnerID)
		i++
	}
	if f.Stage != "" {
		where = append(where, fmt.Sprintf("l.stage=$%d", i))
		args = append(args, string(f.Stage))
		i++
	}
	if f.NoFollowUp != nil {
		where = append(where, fmt.Sprintf("l.no_follow_up=$%d", i))
		args = append(args, *f.NoFollowUp)
		i++
	}
	if qstr := strings.TrimSpace(f.Query); qstr != "" {
		where = append(where, fmt.Sprintf(
			`(l.full_name ILIKE $%d OR l.phone ILIKE $%d OR l.source ILIKE $%d OR COALESCE(u.full_name,'') ILIKE $%d)`,
			i, i, i, i,
		))
		args = append(args, "%"+qstr+"%")
		i++
	}
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

func (r *Repository) ListStageHistory(ctx context.Context, leadID uuid.UUID) ([]domain.StageHistory, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, lead_id, from_stage, to_stage, changed_by, note, created_at
		FROM lead_stage_history WHERE lead_id=$1 ORDER BY created_at ASC`, leadID)
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

func (r *Repository) Analytics(ctx context.Context, branchID uuid.UUID) (*domain.Analytics, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	a := &domain.Analytics{}

	if err := q.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE stage NOT IN ('won','lost')),
			COUNT(*) FILTER (WHERE stage = 'won'),
			COUNT(*) FILTER (WHERE stage = 'lost'),
			COUNT(*) FILTER (WHERE no_follow_up AND stage NOT IN ('won','lost'))
		FROM leads WHERE branch_id=$1`, branchID).Scan(
		&a.Total, &a.Open, &a.Won, &a.Lost, &a.NoFollowUp,
	); err != nil {
		return nil, err
	}
	closed := a.Won + a.Lost
	if closed > 0 {
		a.ConversionRate = float64(a.Won) / float64(closed)
	}

	stageRows, err := q.Query(ctx, `
		SELECT stage, COUNT(*) FROM leads WHERE branch_id=$1 GROUP BY stage ORDER BY stage`, branchID)
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
		FROM leads WHERE branch_id=$1 GROUP BY 1 ORDER BY 2 DESC`, branchID)
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
		WHERE l.branch_id=$1
		GROUP BY l.owner_id, u.full_name
		ORDER BY COUNT(*) DESC`, branchID)
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
