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
	hold_expires_at, status_changed_at, status_reason, ready_forced, created_at, updated_at,
	ref_no, pnr, service_type, supplier_source, channel, summary, company_name, reissue_count`

// Read-time context. Correlated subselects and a lateral projection with
// x_-prefixed names keep every unqualified bookings column (used by pgscope)
// unambiguous.
const (
	overdueExpr = `EXISTS (SELECT 1 FROM payment_schedules ps WHERE ps.booking_id = bookings.id
		AND ps.status IN ('open','overdue') AND ps.due_at < NOW())`
	visaPendingExpr = `EXISTS (SELECT 1 FROM visa_cases vc WHERE vc.booking_id = bookings.id
		AND vc.status IN ('draft','submitted','processing'))`
	soldStatuses = `('confirmed','partially_paid','ready','travelled','completed')`

	infoCols = `,
	COALESCE((SELECT c.full_name FROM customers c WHERE c.id = bookings.customer_id), ''),
	COALESCE((SELECT c.full_name_ar FROM customers c WHERE c.id = bookings.customer_id), ''),
	COALESCE((SELECT u.full_name FROM users u WHERE u.id = bookings.owner_id), ''),
	x.x_package_id, COALESCE(x.x_pkg_code, ''), COALESCE(x.x_pkg_name, ''), COALESCE(x.x_pkg_name_ar, ''),
	COALESCE(x.x_pkg_kind, ''), COALESCE(x.x_dep_code, ''), x.x_depart, x.x_return,
	COALESCE(x.x_makkah, ''), COALESCE(x.x_madinah, ''), COALESCE(x.x_routing, ''),
	(SELECT COUNT(*) FROM booking_participants bp WHERE bp.booking_id = bookings.id),
	COALESCE((SELECT -SUM(pm.amount) FROM payments pm WHERE pm.booking_id = bookings.id
		AND pm.event_type = 'refund' AND pm.status = 'approved'), 0),
	` + overdueExpr + `,
	(SELECT COUNT(*) FROM visa_cases vc WHERE vc.booking_id = bookings.id AND vc.status IN ('draft','submitted','processing')),
	(SELECT COUNT(*) FROM booking_change_requests cr WHERE cr.booking_id = bookings.id AND cr.status = 'requested')`

	bookingFrom = ` FROM bookings LEFT JOIN LATERAL (
		SELECT d.package_id AS x_package_id, d.code AS x_dep_code, d.depart_date AS x_depart, d.return_date AS x_return,
		       p.code AS x_pkg_code, p.name_en AS x_pkg_name, p.name_ar AS x_pkg_name_ar, p.kind AS x_pkg_kind,
		       p.spec->'makkah'->>'name' AS x_makkah, p.spec->'madinah'->>'name' AS x_madinah,
		       p.spec->'flights'->>'routing' AS x_routing
		FROM departures d JOIN packages p ON p.id = d.package_id
		WHERE d.id = bookings.departure_id) x ON TRUE`
)

func scanBooking(row pgx.Row) (*domain.Booking, error) {
	var b domain.Booking
	var status string
	err := row.Scan(baseDest(&b, &status)...)
	if err != nil {
		return nil, err
	}
	b.Status = domain.Status(status)
	return &b, nil
}

func baseDest(b *domain.Booking, status *string) []any {
	return []any{
		&b.ID, &b.BranchID, &b.CustomerID, &b.DepartureID, &b.LeadID, status, &b.PaxCount,
		&b.TotalAmount, &b.DiscountAmt, &b.TaxAmt, &b.FeeAmt, &b.CostAmt, &b.CollectedAmt, &b.BalanceAmt,
		&b.Currency, &b.Notes, &b.OwnerID,
		&b.HoldExpiresAt, &b.StatusChangedAt, &b.StatusReason, &b.ReadyForced, &b.CreatedAt, &b.UpdatedAt,
		&b.RefNo, &b.PNR, &b.ServiceType, &b.SupplierSource, &b.Channel, &b.Summary, &b.CompanyName, &b.ReissueCount,
	}
}

// scanBookingInfo scans bookingCols followed by infoCols.
func scanBookingInfo(row pgx.Row) (*domain.Booking, error) {
	var b domain.Booking
	var status string
	i := &b.Info
	dest := append(baseDest(&b, &status),
		&i.CustomerName, &i.CustomerNameAr, &i.OwnerName,
		&i.PackageID, &i.PackageCode, &i.PackageName, &i.PackageNameAr,
		&i.PackageKind, &i.DepartureCode, &i.DepartDate, &i.ReturnDate,
		&i.MakkahHotel, &i.MadinahHotel, &i.FlightRouting,
		&i.ParticipantsCount, &i.RefundedAmt, &i.OverdueSchedule, &i.VisaPending, &i.OpenChanges,
	)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	b.Status = domain.Status(status)
	return &b, nil
}

const selectBookingInfo = `SELECT ` + bookingCols + infoCols + bookingFrom

func (r *Repository) Create(ctx context.Context, b *domain.Booking) error {
	if err := pgscope.EnsureBranch(ctx, b.BranchID); err != nil {
		return err
	}
	changedAt := b.StatusChangedAt
	if changedAt.IsZero() {
		changedAt = b.CreatedAt
	}
	p := b.Profile()
	if p.ServiceType == "" {
		p.ServiceType = domain.ServicePackage
	}
	if p.Channel == "" {
		p.Channel = domain.ChannelAgent
	}
	b.ServiceType, b.Channel = p.ServiceType, p.Channel
	q := tx.QuerierFrom(ctx, r.pool)
	return q.QueryRow(ctx, `
		INSERT INTO bookings (
			id, branch_id, customer_id, departure_id, lead_id, status, pax_count,
			total_amount, discount_amt, tax_amt, fee_amt, cost_amt, collected_amt, balance_amt, currency, notes, owner_id,
			hold_expires_at, status_changed_at, status_reason, ready_forced, created_at, updated_at,
			pnr, service_type, supplier_source, channel, summary, company_name
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29)
		RETURNING ref_no`,
		b.ID, b.BranchID, b.CustomerID, b.DepartureID, b.LeadID, b.Status, b.PaxCount,
		b.TotalAmount, b.DiscountAmt, b.TaxAmt, b.FeeAmt, b.CostAmt, b.CollectedAmt, b.BalanceAmt, b.Currency, b.Notes,
		b.OwnerID, b.HoldExpiresAt, changedAt, b.StatusReason, b.ReadyForced, b.CreatedAt, b.UpdatedAt,
		p.PNR, p.ServiceType, p.SupplierSource, p.Channel, p.Summary, p.CompanyName,
	).Scan(&b.RefNo)
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
	b, err := scanBookingInfo(q.QueryRow(ctx, selectBookingInfo+` WHERE id=$1`+scope, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return b, err
}

// filterWhere renders the list filter (without segment) as scoped predicates.
func (r *Repository) filterWhere(ctx context.Context, f domain.ListFilter) ([]string, []any, error) {
	where := []string{"1=1"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	eq := func(col string, v any) { where = append(where, col+"="+arg(v)) }
	if f.BranchID != nil {
		eq("branch_id", *f.BranchID)
	}
	if f.CustomerID != nil {
		eq("customer_id", *f.CustomerID)
	}
	if f.DepartureID != nil {
		eq("departure_id", *f.DepartureID)
	}
	if f.PackageID != nil {
		where = append(where, "x.x_package_id="+arg(*f.PackageID))
	}
	if f.OwnerID != nil {
		eq("owner_id", *f.OwnerID)
	}
	if f.LeadID != nil {
		eq("lead_id", *f.LeadID)
	}
	if f.Status != "" {
		eq("status", string(f.Status))
	}
	if f.ServiceType != "" {
		eq("service_type", f.ServiceType)
	}
	if f.Channel != "" {
		eq("channel", f.Channel)
	}
	if f.From != nil || f.To != nil {
		col := "created_at"
		switch f.DateField {
		case domain.DateDepart:
			col = "x.x_depart"
		case domain.DateReturn:
			col = "x.x_return"
		}
		if f.From != nil {
			where = append(where, col+" >= "+arg(*f.From))
		}
		if f.To != nil {
			where = append(where, col+" < "+arg(*f.To))
		}
	}
	if qs := strings.TrimSpace(f.Query); qs != "" {
		like := arg("%" + qs + "%")
		parts := []string{
			"upper(pnr) LIKE upper(" + like + ")",
			"CAST(id AS TEXT) ILIKE " + like,
			"notes ILIKE " + like,
			"company_name ILIKE " + like,
			"summary ILIKE " + like,
			`EXISTS (SELECT 1 FROM customers c WHERE c.id = bookings.customer_id
				AND (c.full_name ILIKE ` + like + ` OR c.full_name_ar ILIKE ` + like + ` OR c.email ILIKE ` + like + `))`,
			"EXISTS (SELECT 1 FROM booking_participants bp WHERE bp.booking_id = bookings.id AND bp.full_name ILIKE " + like + ")",
		}
		if digits := phoneDigits(qs); len(digits) >= 5 {
			parts = append(parts, `EXISTS (SELECT 1 FROM customers c WHERE c.id = bookings.customer_id
				AND regexp_replace(c.phone, '\D', '', 'g') LIKE `+arg("%"+digits+"%")+`)`)
		}
		if n, ok := domain.ParseRefCode(qs); ok {
			parts = append(parts, "ref_no="+arg(n))
		}
		if looksLikeDocumentNo(qs) {
			h := arg(r.pii.Hash(qs))
			parts = append(parts,
				"EXISTS (SELECT 1 FROM booking_participants bp WHERE bp.booking_id = bookings.id AND bp.passport_hash = "+h+")",
				"EXISTS (SELECT 1 FROM customers c WHERE c.id = bookings.customer_id AND c.passport_hash = "+h+")")
		}
		where = append(where, "("+strings.Join(parts, " OR ")+")")
	}
	return pgscope.Append(ctx, scopeBookings, where, args)
}

// segmentWhere is the predicate of a quick segment; dayEndArg is the
// placeholder bound to the end of the caller's day.
func segmentWhere(s domain.Segment, dayEndArg string) string {
	switch s {
	case domain.SegmentOptionToday:
		return "status = 'option_hold' AND hold_expires_at <= " + dayEndArg
	case domain.SegmentPaymentDue:
		return "balance_amt > 0 AND status IN ('option_hold','confirmed','partially_paid','ready','travelled','completed')"
	case domain.SegmentOverdue:
		return "balance_amt > 0 AND status <> 'cancelled' AND " + overdueExpr
	case domain.SegmentVisaPending:
		return "status <> 'cancelled' AND " + visaPendingExpr
	case domain.SegmentIssued:
		return "status IN " + soldStatuses
	case domain.SegmentCancelled:
		return "status = 'cancelled'"
	default:
		return ""
	}
}

func phoneDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if r != ' ' && r != '+' && r != '-' && r != '(' && r != ')' {
			return ""
		}
	}
	return b.String()
}

// looksLikeDocumentNo gates the exact passport lookup to 6–20 character
// tokens that contain a digit, so names never hit the blind index.
func looksLikeDocumentNo(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	if len(s) < 6 || len(s) > 20 {
		return false
	}
	digit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		default:
			return false
		}
	}
	return digit
}

func dayEndOf(f domain.ListFilter) time.Time {
	if !f.DayEnd.IsZero() {
		return f.DayEnd
	}
	now := f.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Booking, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where, args, err := r.filterWhere(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	if seg := segmentWhere(f.Segment, fmt.Sprintf("$%d", len(args)+1)); seg != "" {
		if f.Segment == domain.SegmentOptionToday {
			args = append(args, dayEndOf(f))
		}
		where = append(where, seg)
	}
	clause := strings.Join(where, " AND ")
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*)`+bookingFrom+` WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "updated_at DESC, id"
	switch f.Sort {
	case domain.SortTTL:
		order = "hold_expires_at ASC NULLS LAST, updated_at DESC, id"
	case domain.SortDepart:
		order = "x.x_depart ASC NULLS LAST, updated_at DESC, id"
	}
	i := len(args) + 1
	args = append(args, limit, offset)
	rows, err := q.Query(ctx, fmt.Sprintf(`%s WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		selectBookingInfo, clause, order, i, i+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Booking
	for rows.Next() {
		b, err := scanBookingInfo(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *b)
	}
	return out, total, rows.Err()
}

// Stats counts the operations segments over the same filter as List.
func (r *Repository) Stats(ctx context.Context, f domain.ListFilter) (domain.Stats, error) {
	f.Segment = domain.SegmentAll
	where, args, err := r.filterWhere(ctx, f)
	if err != nil {
		return domain.Stats{}, err
	}
	dayEnd := fmt.Sprintf("$%d", len(args)+1)
	urgent := fmt.Sprintf("$%d", len(args)+2)
	now := f.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	args = append(args, dayEndOf(f), now.Add(domain.UrgentHoldWindow))
	seg := func(s domain.Segment) string { return segmentWhere(s, dayEnd) }
	var st domain.Stats
	err = tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `SELECT
		COUNT(*) FILTER (WHERE status NOT IN ('cancelled','completed')),
		COUNT(*) FILTER (WHERE `+seg(domain.SegmentOptionToday)+`),
		COUNT(*) FILTER (WHERE status = 'option_hold' AND hold_expires_at <= `+urgent+`),
		COUNT(*) FILTER (WHERE `+seg(domain.SegmentPaymentDue)+`),
		COUNT(*) FILTER (WHERE `+seg(domain.SegmentOverdue)+`),
		COUNT(*) FILTER (WHERE `+seg(domain.SegmentVisaPending)+`),
		COUNT(*) FILTER (WHERE `+seg(domain.SegmentIssued)+`),
		COUNT(*) FILTER (WHERE `+seg(domain.SegmentCancelled)+`),
		COUNT(*)`+bookingFrom+` WHERE `+strings.Join(where, " AND "), args...).Scan(
		&st.Active, &st.OptionToday, &st.OptionUrgent, &st.PaymentDue, &st.Overdue,
		&st.VisaPending, &st.Issued, &st.Cancelled, &st.Total)
	return st, err
}

func (r *Repository) AddParticipant(ctx context.Context, p *domain.Participant) error {
	if err := r.requireVisible(ctx, p.BookingID); err != nil {
		return err
	}
	pp, err := r.pii.Seal(pgpii.BookingParticipants, p.ID, p.PassportNo)
	if err != nil {
		return err
	}
	nidEnc, nidLast4, err := r.pii.SealNationalID(pgpii.BookingParticipants, p.ID, p.NationalID)
	if err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err = q.Exec(ctx, `
		INSERT INTO booking_participants (id, booking_id, full_name, passport_no, passport_enc, passport_hash, passport_last4,
			nationality, date_of_birth, gender, national_id_enc, national_id_last4, health_ok, created_at)
		VALUES ($1,$2,$3,'',$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		p.ID, p.BookingID, p.FullName, pp.Enc, pp.Hash, pp.Last4, p.Nationality, p.DateOfBirth,
		p.Gender, nidEnc, nidLast4, p.HealthOK, p.CreatedAt,
	)
	if err == nil {
		p.NationalIDLast4, p.NationalID = nidLast4, ""
	}
	return err
}

// UpdateParticipant rewrites the passenger. A blank NationalID keeps the
// stored number.
func (r *Repository) UpdateParticipant(ctx context.Context, p *domain.Participant) error {
	pp, err := r.pii.Seal(pgpii.BookingParticipants, p.ID, p.PassportNo)
	if err != nil {
		return err
	}
	nidEnc, nidLast4, err := r.pii.SealNationalID(pgpii.BookingParticipants, p.ID, p.NationalID)
	if err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := parentVisible(ctx, "booking_participants.booking_id", []any{
		p.ID, p.BookingID, p.FullName, pp.Enc, pp.Hash, pp.Last4, p.Nationality, p.DateOfBirth,
		p.Gender, p.HealthOK, nidEnc, nidLast4,
	})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE booking_participants SET full_name=$3, passport_no='', passport_enc=$4, passport_hash=$5,
			passport_last4=$6, nationality=$7, date_of_birth=$8, gender=$9, health_ok=$10,
			national_id_enc = CASE WHEN $11 = '' THEN national_id_enc ELSE $11 END,
			national_id_last4 = CASE WHEN $11 = '' THEN national_id_last4 ELSE $12 END
		WHERE id=$1 AND booking_id=$2`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("participant")
	}
	p.NationalID = ""
	if nidEnc != "" {
		p.NationalIDLast4 = nidLast4
		return nil
	}
	return q.QueryRow(ctx, `SELECT national_id_last4 FROM booking_participants WHERE id=$1`, p.ID).Scan(&p.NationalIDLast4)
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
			p.nationality, p.date_of_birth, p.gender, p.national_id_last4, p.health_ok, p.created_at
		FROM booking_participants p WHERE p.booking_id=$1`+scope+` ORDER BY p.created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Participant
	for rows.Next() {
		var p domain.Participant
		var legacy, enc string
		if err := rows.Scan(&p.ID, &p.BookingID, &p.FullName, &legacy, &enc, &p.Nationality, &p.DateOfBirth,
			&p.Gender, &p.NationalIDLast4, &p.HealthOK, &p.CreatedAt); err != nil {
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
	rows, err := q.Query(ctx, selectBookingInfo+` WHERE departure_id=$1`+scope+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Booking
	for rows.Next() {
		b, err := scanBookingInfo(rows)
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
