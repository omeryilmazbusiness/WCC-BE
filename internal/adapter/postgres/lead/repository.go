package lead

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	l.travel_date, l.travel_window, l.pax_count, l.budget_amount, l.budget_currency, l.package_id, l.package_interest,
	l.email, l.segment, l.company_name, l.tax_number, l.tax_office, l.priority, l.intent, l.next_follow_up_at,
	l.services, l.origin, l.destination, l.return_date, l.flex_days, l.adults, l.child_ages, l.infants,
	l.cabin_class, l.board_type, l.preferences,
	COALESCE((SELECT pk.code FROM packages pk WHERE pk.id = l.package_id), ''),
	COALESCE((SELECT pk.name_en FROM packages pk WHERE pk.id = l.package_id), ''),
	COALESCE((SELECT pk.name_ar FROM packages pk WHERE pk.id = l.package_id), '')`

func scanLead(row pgx.Row) (*domain.Lead, error) {
	var l domain.Lead
	var stage, segment, priority, intent string
	p, t := &l.Profile, &l.Interest
	err := row.Scan(
		&l.ID, &l.BranchID, &l.CustomerID, &l.FullName, &l.Phone, &l.Source, &stage, &l.OwnerID,
		&l.OwnerName, &l.LostReasonCode, &l.LostReason, &l.Notes, &l.NoFollowUp,
		&l.ConvertedBookingID, &l.CreatedAt, &l.UpdatedAt,
		&t.TravelDate, &t.TravelWindow, &t.PaxCount, &t.BudgetAmount,
		&t.BudgetCurrency, &t.PackageID, &t.PackageInterest,
		&p.Email, &segment, &p.CompanyName, &p.TaxNumber, &p.TaxOffice, &priority, &intent, &p.NextFollowUpAt,
		&t.Services, &t.Origin, &t.Destination, &t.ReturnDate, &t.FlexDays, &t.Adults, &t.ChildAges, &t.Infants,
		&t.CabinClass, &t.BoardType, &t.Preferences,
		&t.PackageCode, &t.PackageName, &t.PackageNameAr,
	)
	if err != nil {
		return nil, err
	}
	l.Stage = domain.Stage(stage)
	p.Segment, p.Priority, p.Intent = domain.Segment(segment), domain.Priority(priority), domain.Intent(intent)
	return &l, nil
}

// detailArgs are the profile and trip columns shared by Create and Update, in
// the order of detailCols.
func detailArgs(l *domain.Lead) []any {
	p, t := l.Profile, l.Interest
	return []any{
		t.TravelDate, t.TravelWindow, t.PaxCount, t.BudgetAmount, t.BudgetCurrency, t.PackageID, t.PackageInterest,
		p.Email, string(p.Segment), p.CompanyName, p.TaxNumber, p.TaxOffice, string(p.Priority), string(p.Intent), p.NextFollowUpAt,
		emptyIfNil(t.Services), t.Origin, t.Destination, t.ReturnDate, t.FlexDays, t.Adults, emptyIfNil(t.ChildAges), t.Infants,
		t.CabinClass, t.BoardType, emptyIfNil(t.Preferences),
	}
}

var detailCols = []string{
	"travel_date", "travel_window", "pax_count", "budget_amount", "budget_currency", "package_id", "package_interest",
	"email", "segment", "company_name", "tax_number", "tax_office", "priority", "intent", "next_follow_up_at",
	"services", "origin", "destination", "return_date", "flex_days", "adults", "child_ages", "infants",
	"cabin_class", "board_type", "preferences",
}

func emptyIfNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// placeholders renders "$from, $from+1, ..." for n values.
func placeholders(from, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("$%d", from+i)
	}
	return strings.Join(parts, ",")
}

func (r *Repository) Create(ctx context.Context, l *domain.Lead) error {
	if err := pgscope.EnsureBranch(ctx, l.BranchID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	args := append([]any{
		l.ID, l.BranchID, l.CustomerID, l.FullName, l.Phone, l.Source, l.Stage, l.OwnerID,
		l.LostReasonCode, l.LostReason, l.Notes, l.NoFollowUp, l.ConvertedBookingID, l.CreatedAt, l.UpdatedAt,
	}, detailArgs(l)...)
	_, err := q.Exec(ctx, `
		INSERT INTO leads (
			id, branch_id, customer_id, full_name, phone, source, stage, owner_id,
			lost_reason_code, lost_reason, notes, no_follow_up, converted_booking_id, created_at, updated_at,
			`+strings.Join(detailCols, ", ")+`
		) VALUES (`+placeholders(1, len(args))+`)`, args...)
	return err
}

func (r *Repository) Update(ctx context.Context, l *domain.Lead) error {
	q := tx.QuerierFrom(ctx, r.pool)
	args := []any{
		l.ID, l.CustomerID, l.FullName, l.Phone, l.Source, l.Stage, l.OwnerID,
		l.LostReasonCode, l.LostReason, l.Notes, l.NoFollowUp, l.ConvertedBookingID, l.UpdatedAt,
	}
	sets := make([]string, len(detailCols))
	for i, col := range detailCols {
		sets[i] = fmt.Sprintf("%s=$%d", col, len(args)+i+1)
	}
	args = append(args, detailArgs(l)...)
	scope, args, err := pgscope.Clause(ctx, scopeBare, args)
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE leads SET customer_id=$2, full_name=$3, phone=$4, source=$5, stage=$6, owner_id=$7,
			lost_reason_code=$8, lost_reason=$9, notes=$10, no_follow_up=$11, converted_booking_id=$12, updated_at=$13,
			`+strings.Join(sets, ", ")+`
		WHERE id=$1 AND deleted_at IS NULL`+scope, args...)
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
		WHERE l.id=$1 AND l.deleted_at IS NULL`+scope, args...)
	l, err := scanLead(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return l, err
}

// filter builds the WHERE terms shared by List and Board; deleted leads never match.
func filter(ctx context.Context, f domain.ListFilter) ([]string, []any, error) {
	where := []string{"l.deleted_at IS NULL"}
	args := []any{}
	add := func(term string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(term, len(args)))
	}
	if f.BranchID != nil {
		add("l.branch_id=$%d", *f.BranchID)
	}
	if f.OwnerID != nil {
		add("l.owner_id=$%d", *f.OwnerID)
	}
	if f.CustomerID != nil {
		add("l.customer_id=$%d", *f.CustomerID)
	}
	if f.PackageID != nil {
		add("l.package_id=$%d", *f.PackageID)
	}
	if f.Stage != "" {
		add("l.stage=$%d", string(f.Stage))
	}
	if src := strings.TrimSpace(f.Source); src != "" {
		add("lower(l.source)=lower($%d)", src)
	}
	if f.Priority != "" {
		add("l.priority=$%d", string(f.Priority))
	}
	if f.NoFollowUp != nil {
		add("l.no_follow_up=$%d", *f.NoFollowUp)
	}
	if f.CreatedFrom != nil {
		add("l.created_at >= $%d", *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		add("l.created_at < $%d", *f.CreatedTo)
	}
	if qstr := strings.TrimSpace(f.Query); qstr != "" {
		args = append(args, "%"+qstr+"%")
		i := len(args)
		where = append(where, fmt.Sprintf(
			`(l.full_name ILIKE $%d OR l.phone ILIKE $%d OR l.email ILIKE $%d OR l.company_name ILIKE $%d OR l.source ILIKE $%d OR COALESCE(u.full_name,'') ILIKE $%d)`,
			i, i, i, i, i, i,
		))
	}
	return pgscope.Append(ctx, scopeAliased, where, args)
}

// orderBy maps a whitelisted sort to SQL; the id tiebreak keeps pages stable.
func orderBy(s domain.Sort) string {
	switch s {
	case domain.SortCreated:
		return "l.created_at DESC, l.id DESC"
	case domain.SortOldest:
		return "l.created_at ASC, l.id ASC"
	case domain.SortName:
		return "lower(l.full_name) ASC, l.id ASC"
	case domain.SortBudget:
		return "l.budget_amount DESC NULLS LAST, l.updated_at DESC, l.id DESC"
	case domain.SortTravel:
		return "l.travel_date ASC NULLS LAST, l.updated_at DESC, l.id DESC"
	default:
		return "l.updated_at DESC, l.id DESC"
	}
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Lead, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where, args, err := filter(ctx, f)
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
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, leadCols, clause, orderBy(f.Sort), i, i+1)
	args = append(args, limit, offset)
	out, err := r.query(ctx, listSQL, args...)
	return out, total, err
}

func (r *Repository) query(ctx context.Context, sql string, args ...any) ([]domain.Lead, error) {
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Lead
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (r *Repository) Board(ctx context.Context, f domain.ListFilter, perStage int) (*domain.Board, error) {
	f.Stage = ""
	where, args, err := filter(ctx, f)
	if err != nil {
		return nil, err
	}
	clause := strings.Join(where, " AND ")
	q := tx.QuerierFrom(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT l.stage, l.budget_currency, COUNT(*),
			COALESCE(SUM(l.budget_amount) FILTER (WHERE l.budget_amount > 0), 0)::bigint,
			COUNT(*) FILTER (WHERE l.budget_amount > 0),
			COUNT(*) FILTER (WHERE l.no_follow_up)
		FROM leads l LEFT JOIN users u ON u.id = l.owner_id
		WHERE `+clause+`
		GROUP BY l.stage, l.budget_currency`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[domain.Stage]*domain.BoardColumn{}
	board := &domain.Board{}
	for _, st := range domain.AllStages() {
		board.Columns = append(board.Columns, domain.BoardColumn{Stage: st})
	}
	for i := range board.Columns {
		cols[board.Columns[i].Stage] = &board.Columns[i]
	}
	for rows.Next() {
		var stage, currency string
		var n, budgeted, noFollow int
		var amount int64
		if err := rows.Scan(&stage, &currency, &n, &amount, &budgeted, &noFollow); err != nil {
			return nil, err
		}
		col, ok := cols[domain.Stage(stage)]
		if !ok {
			continue
		}
		col.Total += n
		col.NoFollowUp += noFollow
		if currency != "" && budgeted > 0 {
			col.Budgets = append(col.Budgets, domain.BudgetSum{Currency: currency, Amount: amount, Count: budgeted})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range board.Columns {
		b := board.Columns[i].Budgets
		sort.Slice(b, func(x, y int) bool { return b[x].Amount > b[y].Amount })
	}
	if perStage <= 0 {
		return board, nil
	}

	order := orderBy(f.Sort)
	n := len(args) + 1
	items, err := r.query(ctx, fmt.Sprintf(`
		SELECT %[1]s FROM leads l
		LEFT JOIN users u ON u.id = l.owner_id
		WHERE l.id IN (
			SELECT id FROM (
				SELECT l.id, ROW_NUMBER() OVER (PARTITION BY l.stage ORDER BY %[2]s) AS rn
				FROM leads l LEFT JOIN users u ON u.id = l.owner_id
				WHERE %[3]s
			) ranked WHERE rn <= $%[4]d
		)
		ORDER BY %[2]s`, leadCols, order, clause, n), append(args, perStage)...)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if col, ok := cols[it.Stage]; ok {
			col.Items = append(col.Items, it)
		}
	}
	return board, nil
}

// leadDeletedOutcome marks tasks cancelled by a lead deletion so a restore
// reopens exactly those.
const leadDeletedOutcome = "lead_deleted"

func (r *Repository) SoftDelete(ctx context.Context, id, by uuid.UUID, at time.Time) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeBare, []any{id, at, by})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE leads SET deleted_at=$2, deleted_by=$3
		WHERE id=$1 AND deleted_at IS NULL`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("lead")
	}
	_, err = q.Exec(ctx, `
		UPDATE tasks SET status='cancelled', outcome=$3, updated_at=$2
		WHERE related_type='lead' AND related_id=$1 AND status IN ('open','in_progress')`,
		id, at, leadDeletedOutcome)
	return err
}

func (r *Repository) Restore(ctx context.Context, id uuid.UUID, at time.Time) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeBare, []any{id})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE leads SET deleted_at=NULL, deleted_by=NULL
		WHERE id=$1 AND deleted_at IS NOT NULL`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("lead")
	}
	_, err = q.Exec(ctx, `
		UPDATE tasks SET status='open', outcome='', updated_at=$2
		WHERE related_type='lead' AND related_id=$1 AND status='cancelled' AND outcome=$3`,
		id, at, leadDeletedOutcome)
	return err
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
			WHERE l.branch_id = $1 AND l.stage = 'lost' AND l.deleted_at IS NULL AND h.created_at >= $2 AND h.created_at < $3`+scope+`
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
		WHERE h.lead_id=$1 AND EXISTS (SELECT 1 FROM leads l WHERE l.id = h.lead_id AND l.deleted_at IS NULL`+scope+`)
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
	where := []string{"l.deleted_at IS NULL"}
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
