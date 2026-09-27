package booking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgpii"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var (
	scopeBookings = pgscope.Columns{Branch: "branch_id", Owner: "owner_id"}
	scopeParent   = pgscope.Columns{Branch: "b.branch_id", Owner: "b.owner_id"}
)

// parentVisible renders an EXISTS predicate restricting child rows whose
// booking_id column is childCol to bookings visible in the caller's scope.
func parentVisible(ctx context.Context, childCol string, args []any) (string, []any, error) {
	scope, args, err := pgscope.Clause(ctx, scopeParent, args)
	if err != nil {
		return "", args, err
	}
	return " AND EXISTS (SELECT 1 FROM bookings b WHERE b.id = " + childCol + scope + ")", args, nil
}

// requireVisible fails with NotFound unless the booking is in the caller's scope.
func (r *Repository) requireVisible(ctx context.Context, bookingID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeParent, []any{bookingID})
	if err != nil {
		return err
	}
	var ok bool
	err = q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bookings b WHERE b.id=$1`+scope+`)`, args...).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return shared.NewNotFound("booking")
	}
	return nil
}

type Repository struct {
	pool *pgxpool.Pool
	pii  *pgpii.Passports
}

func NewRepository(pool *pgxpool.Pool, pii *pgpii.Passports) *Repository {
	return &Repository{pool: pool, pii: pii}
}

const bookingCols = `id, branch_id, customer_id, departure_id, lead_id, status, pax_count,
	total_amount, discount_amt, tax_amt, fee_amt, cost_amt, collected_amt, balance_amt, currency, notes, owner_id,
	hold_expires_at, status_changed_at, status_reason, ready_forced, created_at, updated_at`

func scanBooking(row pgx.Row) (*domain.Booking, error) {
	var b domain.Booking
	var status string
	err := row.Scan(
		&b.ID, &b.BranchID, &b.CustomerID, &b.DepartureID, &b.LeadID, &status, &b.PaxCount,
		&b.TotalAmount, &b.DiscountAmt, &b.TaxAmt, &b.FeeAmt, &b.CostAmt, &b.CollectedAmt, &b.BalanceAmt,
		&b.Currency, &b.Notes, &b.OwnerID,
		&b.HoldExpiresAt, &b.StatusChangedAt, &b.StatusReason, &b.ReadyForced, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	b.Status = domain.Status(status)
	return &b, nil
}

func (r *Repository) Create(ctx context.Context, b *domain.Booking) error {
	if err := pgscope.EnsureBranch(ctx, b.BranchID); err != nil {
		return err
	}
	changedAt := b.StatusChangedAt
	if changedAt.IsZero() {
		changedAt = b.CreatedAt
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO bookings (
			id, branch_id, customer_id, departure_id, lead_id, status, pax_count,
			total_amount, discount_amt, tax_amt, fee_amt, cost_amt, collected_amt, balance_amt, currency, notes, owner_id,
			hold_expires_at, status_changed_at, status_reason, ready_forced, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
		b.ID, b.BranchID, b.CustomerID, b.DepartureID, b.LeadID, b.Status, b.PaxCount,
		b.TotalAmount, b.DiscountAmt, b.TaxAmt, b.FeeAmt, b.CostAmt, b.CollectedAmt, b.BalanceAmt, b.Currency, b.Notes,
		b.OwnerID, b.HoldExpiresAt, changedAt, b.StatusReason, b.ReadyForced, b.CreatedAt, b.UpdatedAt,
	)
	return err
}

// Update writes the editable and money fields; lifecycle columns are only
// written by SaveStatus.
func (r *Repository) Update(ctx context.Context, b *domain.Booking) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeBookings, []any{
		b.ID, b.PaxCount, b.TotalAmount, b.DiscountAmt, b.TaxAmt, b.FeeAmt, b.CostAmt,
		b.CollectedAmt, b.BalanceAmt, b.Currency, b.Notes, b.OwnerID, b.UpdatedAt,
	})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE bookings SET pax_count=$2, total_amount=$3, discount_amt=$4, tax_amt=$5, fee_amt=$6, cost_amt=$7,
			collected_amt=$8, balance_amt=$9, currency=$10, notes=$11, owner_id=$12, updated_at=$13
		WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("booking")
	}
	return nil
}

func (r *Repository) FindForUpdate(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeBookings, []any{id})
	if err != nil {
		return nil, err
	}
	b, err := scanBooking(q.QueryRow(ctx, `SELECT `+bookingCols+` FROM bookings WHERE id=$1`+scope+` FOR UPDATE`, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("booking")
	}
	return b, err
}

func (r *Repository) SaveStatus(ctx context.Context, b *domain.Booking) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeBookings, []any{
		b.ID, b.Status, b.HoldExpiresAt, b.StatusChangedAt, b.StatusReason, b.ReadyForced, b.UpdatedAt,
	})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE bookings SET status=$2, hold_expires_at=$3, status_changed_at=$4, status_reason=$5,
			ready_forced=$6, updated_at=$7
		WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("booking")
	}
	return nil
}

// LockDeparture is unscoped like the seat count it protects.
func (r *Repository) LockDeparture(ctx context.Context, departureID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT id FROM departures WHERE id=$1 FOR UPDATE`, departureID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewNotFound("departure")
	}
	return err
}

func (r *Repository) listIDs(ctx context.Context, where []string, args []any, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	args = append(args, after)
	where = append(where, fmt.Sprintf("b.id > $%d", len(args)))
	where, args, err := pgscope.Append(ctx, scopeParent, where, args)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	args = append(args, limit)
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, fmt.Sprintf(`SELECT b.id FROM bookings b WHERE %s ORDER BY b.id LIMIT $%d`,
		strings.Join(where, " AND "), len(args)), args...)
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

func (r *Repository) ListExpiredHolds(ctx context.Context, now time.Time, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	return r.listIDs(ctx, []string{"b.status = 'option_hold'", "b.hold_expires_at <= $1"}, []any{now}, after, limit)
}

func (r *Repository) ListDueForTravel(ctx context.Context, today time.Time, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	return r.listIDs(ctx, []string{
		"b.status IN ('confirmed','partially_paid','ready')",
		"EXISTS (SELECT 1 FROM departures d WHERE d.id = b.departure_id AND d.depart_date <= $1::date)",
	}, []any{today.Format("2006-01-02")}, after, limit)
}

// ListDerivedCandidates skips confirmed-family bookings whose status already
// matches their money and cannot become ready (balance still open).
func (r *Repository) ListDerivedCandidates(ctx context.Context, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	return r.listIDs(ctx, []string{
		"b.status IN ('confirmed','partially_paid','ready')",
		`NOT (b.balance_amt > 0 AND (
			(b.status = 'confirmed' AND b.collected_amt <= 0) OR
			(b.status = 'partially_paid' AND b.collected_amt > 0)))`,
		"NOT (b.status = 'ready' AND b.ready_forced)",
	}, nil, after, limit)
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeBookings, []any{id})
	if err != nil {
		return nil, err
	}
	b, err := scanBooking(q.QueryRow(ctx, `SELECT `+bookingCols+` FROM bookings WHERE id=$1`+scope, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return b, err
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Booking, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where := []string{"1=1"}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf("%s=$%d", col, len(args)))
	}
	if f.BranchID != nil {
		add("branch_id", *f.BranchID)
	}
	if f.CustomerID != nil {
		add("customer_id", *f.CustomerID)
	}
	if f.DepartureID != nil {
		add("departure_id", *f.DepartureID)
	}
	if f.OwnerID != nil {
		add("owner_id", *f.OwnerID)
	}
	if f.LeadID != nil {
		add("lead_id", *f.LeadID)
	}
	if f.Status != "" {
		add("status", string(f.Status))
	}
	if qs := strings.TrimSpace(f.Query); qs != "" {
		args = append(args, "%"+qs+"%")
		where = append(where, fmt.Sprintf(`(notes ILIKE $%d OR CAST(id AS TEXT) ILIKE $%d)`, len(args), len(args)))
	}
	where, args, err := pgscope.Append(ctx, scopeBookings, where, args)
	if err != nil {
		return nil, 0, err
	}
	i := len(args) + 1
	clause := strings.Join(where, " AND ")
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM bookings WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := fmt.Sprintf(`SELECT %s FROM bookings WHERE %s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`,
		bookingCols, clause, i, i+1)
	args = append(args, limit, offset)
	rows, err := q.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Booking
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *b)
	}
	return out, total, rows.Err()
}

func (r *Repository) AddParticipant(ctx context.Context, p *domain.Participant) error {
	if err := r.requireVisible(ctx, p.BookingID); err != nil {
		return err
	}
	pp, err := r.pii.Seal(pgpii.BookingParticipants, p.ID, p.PassportNo)
	if err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err = q.Exec(ctx, `
		INSERT INTO booking_participants (id, booking_id, full_name, passport_no, passport_enc, passport_hash, passport_last4,
			nationality, date_of_birth, created_at)
		VALUES ($1,$2,$3,'',$4,$5,$6,$7,$8,$9)`,
		p.ID, p.BookingID, p.FullName, pp.Enc, pp.Hash, pp.Last4, p.Nationality, p.DateOfBirth, p.CreatedAt,
	)
	return err
}

func (r *Repository) UpdateParticipant(ctx context.Context, p *domain.Participant) error {
	pp, err := r.pii.Seal(pgpii.BookingParticipants, p.ID, p.PassportNo)
	if err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := parentVisible(ctx, "booking_participants.booking_id", []any{
		p.ID, p.BookingID, p.FullName, pp.Enc, pp.Hash, pp.Last4, p.Nationality, p.DateOfBirth,
	})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE booking_participants SET full_name=$3, passport_no='', passport_enc=$4, passport_hash=$5,
			passport_last4=$6, nationality=$7, date_of_birth=$8
		WHERE id=$1 AND booking_id=$2`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("participant")
	}
	return nil
}

func (r *Repository) DeleteParticipant(ctx context.Context, bookingID, participantID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := parentVisible(ctx, "booking_participants.booking_id", []any{participantID, bookingID})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `DELETE FROM booking_participants WHERE id=$1 AND booking_id=$2`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("participant")
	}
	return nil
}

func (r *Repository) ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]domain.Participant, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := parentVisible(ctx, "p.booking_id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT p.id, p.booking_id, p.full_name, COALESCE(p.passport_no,''), p.passport_enc,
			p.nationality, p.date_of_birth, p.created_at
		FROM booking_participants p WHERE p.booking_id=$1`+scope+` ORDER BY p.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Participant
	for rows.Next() {
		var p domain.Participant
		var legacy, enc string
		if err := rows.Scan(&p.ID, &p.BookingID, &p.FullName, &legacy, &enc, &p.Nationality, &p.DateOfBirth, &p.CreatedAt); err != nil {
			return nil, err
		}
		if p.PassportNo, err = r.pii.Open(pgpii.BookingParticipants, p.ID, enc, legacy); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountConfirmedPaxByDeparture sums pax in seat-consuming statuses. It is
// deliberately unscoped: departure capacity is shared across branches and
// owners, so the seat guard must see every seat. It exposes only an aggregate.
func (r *Repository) CountConfirmedPaxByDeparture(ctx context.Context, departureID uuid.UUID) (int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var n int
	err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(pax_count),0) FROM bookings
		WHERE departure_id=$1
		  AND status IN ('option_hold','confirmed','partially_paid','ready','travelled','completed')`,
		departureID).Scan(&n)
	return n, err
}

func (r *Repository) ListByDeparture(ctx context.Context, departureID uuid.UUID) ([]domain.Booking, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeBookings, []any{departureID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+bookingCols+` FROM bookings WHERE departure_id=$1`+scope+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Booking
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (r *Repository) ReplaceLineItems(ctx context.Context, bookingID uuid.UUID, items []domain.LineItem) error {
	if err := r.requireVisible(ctx, bookingID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `DELETE FROM booking_line_items WHERE booking_id=$1`, bookingID); err != nil {
		return err
	}
	now := time.Now().UTC()
	for i, it := range items {
		id := it.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO booking_line_items (
				id, booking_id, kind, category, label, quantity, unit_price, unit_cost, sort_order, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			id, bookingID, it.Kind, it.Category, it.Label, it.Quantity, it.UnitPrice, it.UnitCost, i, now, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListLineItems(ctx context.Context, bookingID uuid.UUID) ([]domain.LineItem, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := parentVisible(ctx, "li.booking_id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT li.id, li.booking_id, li.kind, li.category, li.label, li.quantity, li.unit_price, li.unit_cost, li.sort_order,
			li.created_at, li.updated_at
		FROM booking_line_items li WHERE li.booking_id=$1`+scope+` ORDER BY li.sort_order`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LineItem
	for rows.Next() {
		var l domain.LineItem
		if err := rows.Scan(&l.ID, &l.BookingID, &l.Kind, &l.Category, &l.Label, &l.Quantity, &l.UnitPrice, &l.UnitCost,
			&l.SortOrder, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *Repository) SeedChecklist(ctx context.Context, items []domain.ChecklistItem) error {
	checked := map[uuid.UUID]struct{}{}
	for _, it := range items {
		if _, ok := checked[it.BookingID]; ok {
			continue
		}
		if err := r.requireVisible(ctx, it.BookingID); err != nil {
			return err
		}
		checked[it.BookingID] = struct{}{}
	}
	q := tx.QuerierFrom(ctx, r.pool)
	for _, it := range items {
		if _, err := q.Exec(ctx, `
			INSERT INTO booking_checklist_items (
				id, booking_id, code, label, required, completed, completed_at, sort_order, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (booking_id, code) DO NOTHING`,
			it.ID, it.BookingID, it.Code, it.Label, it.Required, it.Completed, it.CompletedAt, it.SortOrder, it.CreatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListChecklist(ctx context.Context, bookingID uuid.UUID) ([]domain.ChecklistItem, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := parentVisible(ctx, "c.booking_id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT c.id, c.booking_id, c.code, c.label, c.required, c.completed, c.completed_at, c.sort_order, c.created_at
		FROM booking_checklist_items c WHERE c.booking_id=$1`+scope+` ORDER BY c.sort_order`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ChecklistItem
	for rows.Next() {
		var c domain.ChecklistItem
		if err := rows.Scan(&c.ID, &c.BookingID, &c.Code, &c.Label, &c.Required, &c.Completed, &c.CompletedAt,
			&c.SortOrder, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateChecklistItem(ctx context.Context, item *domain.ChecklistItem) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := parentVisible(ctx, "booking_checklist_items.booking_id", []any{
		item.ID, item.BookingID, item.Completed, item.CompletedAt, item.Label,
	})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE booking_checklist_items SET completed=$3, completed_at=$4, label=$5
		WHERE id=$1 AND booking_id=$2`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("checklist item")
	}
	return nil
}

func (r *Repository) UpsertReadinessOverride(ctx context.Context, o *domain.ReadinessOverride) error {
	if err := r.requireVisible(ctx, o.BookingID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO booking_readiness_overrides (id, booking_id, reason, actor_id, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (booking_id) DO UPDATE SET
			reason = EXCLUDED.reason,
			actor_id = EXCLUDED.actor_id,
			created_at = EXCLUDED.created_at,
			id = EXCLUDED.id`,
		o.ID, o.BookingID, o.Reason, o.ActorID, o.CreatedAt,
	)
	return err
}

func (r *Repository) FindReadinessOverride(ctx context.Context, bookingID uuid.UUID) (*domain.ReadinessOverride, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := parentVisible(ctx, "o.booking_id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `
		SELECT o.id, o.booking_id, o.reason, o.actor_id, o.created_at
		FROM booking_readiness_overrides o WHERE o.booking_id=$1`+scope, args...)
	var o domain.ReadinessOverride
	err = row.Scan(&o.ID, &o.BookingID, &o.Reason, &o.ActorID, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}
