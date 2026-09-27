package customer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgpii"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Customers are shared within a branch (dedupe relies on it), so they are
// scoped by branch only.
var (
	scopeCustomers = pgscope.Columns{Branch: "branch_id"}
	scopeAliased   = pgscope.Columns{Branch: "c.branch_id"}
)

// customerVisible renders an EXISTS predicate restricting rows whose customer
// reference is col to customers in the caller's branch scope.
func customerVisible(ctx context.Context, col string, args []any) (string, []any, error) {
	scope, args, err := pgscope.Clause(ctx, scopeAliased, args)
	if err != nil {
		return "", args, err
	}
	return " AND EXISTS (SELECT 1 FROM customers c WHERE c.id = " + col + scope + ")", args, nil
}

type Repository struct {
	pool *pgxpool.Pool
	pii  *pgpii.Passports
}

func NewRepository(pool *pgxpool.Pool, pii *pgpii.Passports) *Repository {
	return &Repository{pool: pool, pii: pii}
}

// Passport numbers are stored encrypted; passport_no is only read as a
// fallback for rows the encrypt backfill has not reached and always written
// blank.
const customerCols = `id, branch_id, full_name, full_name_ar, phone, email, nationality,
	COALESCE(passport_no,''), passport_enc, date_of_birth, COALESCE(preferences,'{}'::jsonb),
	COALESCE(special_requirements,''), notes, merged_into_id, COALESCE(is_active,true),
	created_by, created_at, updated_at, anonymized_at`

func (r *Repository) Create(ctx context.Context, c *domain.Customer) error {
	if err := pgscope.EnsureBranch(ctx, c.BranchID); err != nil {
		return err
	}
	if c.Preferences == nil {
		c.Preferences = json.RawMessage(`{}`)
	}
	pp, err := r.pii.Seal(pgpii.Customers, c.ID, c.PassportNo)
	if err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err = q.Exec(ctx, `
		INSERT INTO customers (
			id, branch_id, full_name, full_name_ar, phone, email, nationality,
			passport_no, passport_enc, passport_hash, passport_last4,
			date_of_birth, preferences, special_requirements, notes,
			merged_into_id, is_active, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,'',$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		c.ID, c.BranchID, c.FullName, c.FullNameAR, c.Phone, c.Email, c.Nationality,
		pp.Enc, pp.Hash, pp.Last4,
		c.DateOfBirth, c.Preferences, c.SpecialRequirements, c.Notes,
		c.MergedIntoID, c.IsActive, c.CreatedBy, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, c *domain.Customer) error {
	if c.Preferences == nil {
		c.Preferences = json.RawMessage(`{}`)
	}
	pp, err := r.pii.Seal(pgpii.Customers, c.ID, c.PassportNo)
	if err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeCustomers, []any{
		c.ID, c.FullName, c.FullNameAR, c.Phone, c.Email, c.Nationality,
		pp.Enc, pp.Hash, pp.Last4, c.DateOfBirth, c.Preferences, c.SpecialRequirements,
		c.Notes, c.IsActive, c.UpdatedAt,
	})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE customers SET
			full_name=$2, full_name_ar=$3, phone=$4, email=$5, nationality=$6,
			passport_no='', passport_enc=$7, passport_hash=$8, passport_last4=$9,
			date_of_birth=$10, preferences=$11, special_requirements=$12,
			notes=$13, is_active=$14, updated_at=$15
		WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("customer")
	}
	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Customer, error) {
	return r.findOne(ctx, `id=$1`, id)
}

func (r *Repository) FindByPhone(ctx context.Context, phone string, branchID uuid.UUID) (*domain.Customer, error) {
	return r.findOne(ctx, `branch_id=$2 AND is_active=true AND merged_into_id IS NULL
		  AND regexp_replace(phone, '[^0-9+]', '', 'g') = $1`, phone, branchID)
}

func (r *Repository) FindByEmail(ctx context.Context, email string, branchID uuid.UUID) (*domain.Customer, error) {
	return r.findOne(ctx, `branch_id=$2 AND is_active=true AND merged_into_id IS NULL
		  AND lower(email) = lower($1) AND email <> ''`, email, branchID)
}

// FindByPassport matches the blind index exactly; the plaintext comparison
// covers rows not yet backfilled.
func (r *Repository) FindByPassport(ctx context.Context, passport string, branchID uuid.UUID) (*domain.Customer, error) {
	hash := r.pii.Hash(passport)
	if hash == "" {
		return nil, nil
	}
	return r.findOne(ctx, `branch_id=$2 AND is_active=true AND merged_into_id IS NULL
		  AND (passport_hash = $1 OR (passport_no <> '' AND upper(replace(passport_no,' ','')) = $3))`,
		hash, branchID, shared.NormalizePassport(passport))
}

// findOne returns the first customer matching where (bound to args) within
// the caller's scope; (nil, nil) when none is visible.
func (r *Repository) findOne(ctx context.Context, where string, args ...any) (*domain.Customer, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeCustomers, args)
	if err != nil {
		return nil, err
	}
	return r.scan(q.QueryRow(ctx, `SELECT `+customerCols+` FROM customers WHERE `+where+scope+` LIMIT 1`, args...))
}

func (r *Repository) FindNameCandidates(ctx context.Context, name string, branchID uuid.UUID, limit int) ([]domain.Customer, error) {
	if limit <= 0 {
		limit = 20
	}
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeCustomers, []any{branchID, "%" + name + "%"})
	if err != nil {
		return nil, err
	}
	args = append(args, limit)
	rows, err := q.Query(ctx, fmt.Sprintf(`
		SELECT `+customerCols+` FROM customers
		WHERE branch_id=$1 AND is_active=true AND merged_into_id IS NULL
		  AND (full_name ILIKE $2 OR full_name_ar ILIKE $2)`+scope+`
		ORDER BY updated_at DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanAll(rows)
}

func (r *Repository) Search(ctx context.Context, f domain.SearchFilter) ([]domain.Customer, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	pattern := "%" + f.Query + "%"
	activeClause := `AND merged_into_id IS NULL AND is_active=true`
	if f.IncludeMerged {
		activeClause = ""
	}
	passport := shared.NormalizePassport(f.Query)
	scope, args, err := pgscope.Clause(ctx, scopeCustomers, []any{f.Query, pattern, f.BranchID, r.pii.Hash(f.Query), passport})
	if err != nil {
		return nil, 0, err
	}
	// Passports match exactly (blind index), never by substring.
	where := `($1 = '' OR full_name ILIKE $2 OR phone ILIKE $2 OR full_name_ar ILIKE $2 OR email ILIKE $2
			OR ($4 <> '' AND passport_hash = $4)
			OR ($5 <> '' AND passport_no <> '' AND upper(replace(passport_no,' ','')) = $5))
		AND ($3::uuid IS NULL OR branch_id = $3) ` + activeClause + scope
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM customers WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	n := len(args)
	args = append(args, f.Limit, f.Offset)
	rows, err := q.Query(ctx, fmt.Sprintf(`
		SELECT `+customerCols+`
		FROM customers
		WHERE `+where+`
		ORDER BY updated_at DESC
		LIMIT $%d OFFSET $%d`, n+1, n+2), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := r.scanAll(rows)
	return items, total, err
}

func (r *Repository) MarkMerged(ctx context.Context, sourceID, targetID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeCustomers, []any{sourceID, targetID})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE customers SET merged_into_id=$2, is_active=false, updated_at=NOW() WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("customer")
	}
	return nil
}

// ReassignLeads and ReassignBookings are scoped by branch only: a merge must
// repoint every reference to the absorbed customer, including records owned
// by colleagues, or they would dangle on an inactive customer.
func (r *Repository) ReassignLeads(ctx context.Context, from, to uuid.UUID) error {
	return r.reassign(ctx, "leads", from, to)
}

func (r *Repository) ReassignBookings(ctx context.Context, from, to uuid.UUID) error {
	return r.reassign(ctx, "bookings", from, to)
}

func (r *Repository) reassign(ctx context.Context, table string, from, to uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeCustomers, []any{from, to})
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE `+table+` SET customer_id=$2, updated_at=NOW() WHERE customer_id=$1`+scope, args...)
	return err
}

func (r *Repository) ListCompanions(ctx context.Context, customerID uuid.UUID) ([]domain.CompanionLink, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeAliased, []any{customerID})
	if err != nil {
		return nil, err
	}
	parentScope, args, err := customerVisible(ctx, "cc.customer_id", args)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT cc.id, cc.customer_id, cc.companion_id, cc.relation, cc.notes, cc.created_at,
			c.id, c.branch_id, c.full_name, c.full_name_ar, c.phone, c.email, c.nationality,
			COALESCE(c.passport_no,''), c.passport_enc, c.date_of_birth, COALESCE(c.preferences,'{}'::jsonb),
			COALESCE(c.special_requirements,''), c.notes, c.merged_into_id, COALESCE(c.is_active,true),
			c.created_by, c.created_at, c.updated_at, c.anonymized_at
		FROM customer_companions cc
		JOIN customers c ON c.id = cc.companion_id
		WHERE cc.customer_id=$1`+scope+parentScope+`
		ORDER BY cc.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CompanionLink
	for rows.Next() {
		var link domain.CompanionLink
		var comp domain.Customer
		var legacy, enc string
		if err := rows.Scan(
			&link.ID, &link.CustomerID, &link.CompanionID, &link.Relation, &link.Notes, &link.CreatedAt,
			&comp.ID, &comp.BranchID, &comp.FullName, &comp.FullNameAR, &comp.Phone, &comp.Email, &comp.Nationality,
			&legacy, &enc, &comp.DateOfBirth, &comp.Preferences, &comp.SpecialRequirements, &comp.Notes,
			&comp.MergedIntoID, &comp.IsActive, &comp.CreatedBy, &comp.CreatedAt, &comp.UpdatedAt, &comp.AnonymizedAt,
		); err != nil {
			return nil, err
		}
		if comp.PassportNo, err = r.pii.Open(pgpii.Customers, comp.ID, enc, legacy); err != nil {
			return nil, err
		}
		cp := comp
		link.Companion = &cp
		out = append(out, link)
	}
	return out, rows.Err()
}

func (r *Repository) LinkCompanion(ctx context.Context, link *domain.CompanionLink) error {
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	if link.CreatedAt.IsZero() {
		link.CreatedAt = time.Now().UTC()
	}
	q := tx.QuerierFrom(ctx, r.pool)
	args := []any{link.ID, link.CustomerID, link.CompanionID, link.Relation, link.Notes, link.CreatedAt}
	ownerScope, args, err := customerVisible(ctx, "$2::uuid", args)
	if err != nil {
		return err
	}
	companionScope, args, err := customerVisible(ctx, "$3::uuid", args)
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		INSERT INTO customer_companions (id, customer_id, companion_id, relation, notes, created_at)
		SELECT $1::uuid, $2::uuid, $3::uuid, $4::text, $5::text, $6::timestamptz WHERE TRUE`+ownerScope+companionScope+`
		ON CONFLICT (customer_id, companion_id) DO UPDATE SET relation=EXCLUDED.relation, notes=EXCLUDED.notes`,
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("customer")
	}
	return nil
}

func (r *Repository) UnlinkCompanion(ctx context.Context, customerID, companionID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := customerVisible(ctx, "customer_companions.customer_id", []any{customerID, companionID})
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		DELETE FROM customer_companions WHERE customer_id=$1 AND companion_id=$2`+scope, args...)
	return err
}

// ListTimeline aggregates activity for a customer; each source is filtered
// by its own visibility rules so the timeline never reveals leads, bookings
// or tasks outside the caller's scope.
func (r *Repository) ListTimeline(ctx context.Context, customerID uuid.UUID, limit int) ([]domain.TimelineItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := tx.QuerierFrom(ctx, r.pool)
	args := []any{customerID}
	var clauses [5]string
	for i, cols := range []pgscope.Columns{
		{Branch: "l.branch_id", Owner: "l.owner_id"},
		{Branch: "b.branch_id", Owner: "b.owner_id"},
		{Branch: "b.branch_id", Owner: "b.owner_id"},
		{Branch: "d.branch_id"},
		{Branch: "t.branch_id", Owner: "t.assignee_id"},
	} {
		var err error
		if clauses[i], args, err = pgscope.Clause(ctx, cols, args); err != nil {
			return nil, err
		}
	}
	args = append(args, limit)
	rows, err := q.Query(ctx, fmt.Sprintf(`
		(
			SELECT 'lead'::text, l.id, coalesce(l.full_name,'Lead'), l.stage, l.updated_at, jsonb_build_object('source', l.source)
			FROM leads l WHERE l.customer_id=$1`+clauses[0]+`
		)
		UNION ALL
		(
			SELECT 'booking', b.id, 'Booking '||left(b.id::text,8), b.status, b.updated_at,
				jsonb_build_object('balance', b.balance_amt, 'currency', b.currency, 'pax', b.pax_count)
			FROM bookings b WHERE b.customer_id=$1`+clauses[1]+`
		)
		UNION ALL
		(
			SELECT 'payment', p.id, 'Payment '||p.amount::text, p.method, p.created_at,
				jsonb_build_object('currency', p.currency, 'booking_id', p.booking_id)
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			WHERE b.customer_id=$1`+clauses[2]+`
		)
		UNION ALL
		(
			SELECT 'document', d.id, d.file_name, d.kind, d.created_at,
				jsonb_build_object('related_type', d.related_type)
			FROM documents d WHERE d.related_type='customer' AND d.related_id=$1`+clauses[3]+`
		)
		UNION ALL
		(
			SELECT 'task', t.id, t.title, t.status, coalesce(t.updated_at, t.created_at),
				jsonb_build_object('kind', t.kind, 'related_type', t.related_type, 'related_id', t.related_id)
			FROM tasks t
			WHERE ((t.related_type='customer' AND t.related_id=$1)
			   OR (t.related_type='lead' AND t.related_id IN (SELECT id FROM leads WHERE customer_id=$1))
			   OR (t.related_type='booking' AND t.related_id IN (SELECT id FROM bookings WHERE customer_id=$1)))`+clauses[4]+`
		)
		ORDER BY 5 DESC
		LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TimelineItem
	for rows.Next() {
		var item domain.TimelineItem
		if err := rows.Scan(&item.Kind, &item.ID, &item.Title, &item.Status, &item.OccurredAt, &item.Meta); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func (r *Repository) scan(row scannable) (*domain.Customer, error) {
	var c domain.Customer
	var legacy, enc string
	err := row.Scan(
		&c.ID, &c.BranchID, &c.FullName, &c.FullNameAR, &c.Phone, &c.Email, &c.Nationality,
		&legacy, &enc, &c.DateOfBirth, &c.Preferences, &c.SpecialRequirements, &c.Notes,
		&c.MergedIntoID, &c.IsActive, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.AnonymizedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if c.PassportNo, err = r.pii.Open(pgpii.Customers, c.ID, enc, legacy); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) scanAll(rows pgx.Rows) ([]domain.Customer, error) {
	var out []domain.Customer
	for rows.Next() {
		c, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
