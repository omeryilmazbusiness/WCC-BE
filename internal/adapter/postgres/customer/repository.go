package customer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const customerCols = `id, branch_id, full_name, full_name_ar, phone, email, nationality,
	COALESCE(passport_no,''), date_of_birth, COALESCE(preferences,'{}'::jsonb),
	COALESCE(special_requirements,''), notes, merged_into_id, COALESCE(is_active,true),
	created_by, created_at, updated_at`

func (r *Repository) Create(ctx context.Context, c *domain.Customer) error {
	if c.Preferences == nil {
		c.Preferences = json.RawMessage(`{}`)
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO customers (
			id, branch_id, full_name, full_name_ar, phone, email, nationality,
			passport_no, date_of_birth, preferences, special_requirements, notes,
			merged_into_id, is_active, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		c.ID, c.BranchID, c.FullName, c.FullNameAR, c.Phone, c.Email, c.Nationality,
		c.PassportNo, c.DateOfBirth, c.Preferences, c.SpecialRequirements, c.Notes,
		c.MergedIntoID, c.IsActive, c.CreatedBy, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, c *domain.Customer) error {
	if c.Preferences == nil {
		c.Preferences = json.RawMessage(`{}`)
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE customers SET
			full_name=$2, full_name_ar=$3, phone=$4, email=$5, nationality=$6,
			passport_no=$7, date_of_birth=$8, preferences=$9, special_requirements=$10,
			notes=$11, is_active=$12, updated_at=$13
		WHERE id=$1`,
		c.ID, c.FullName, c.FullNameAR, c.Phone, c.Email, c.Nationality,
		c.PassportNo, c.DateOfBirth, c.Preferences, c.SpecialRequirements,
		c.Notes, c.IsActive, c.UpdatedAt,
	)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Customer, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+customerCols+` FROM customers WHERE id=$1`, id)
	return scan(row)
}

func (r *Repository) FindByPhone(ctx context.Context, phone string, branchID uuid.UUID) (*domain.Customer, error) {
	return r.findOne(ctx, `
		SELECT `+customerCols+` FROM customers
		WHERE branch_id=$2 AND is_active=true AND merged_into_id IS NULL
		  AND regexp_replace(phone, '[^0-9+]', '', 'g') = $1
		LIMIT 1`, phone, branchID)
}

func (r *Repository) FindByEmail(ctx context.Context, email string, branchID uuid.UUID) (*domain.Customer, error) {
	return r.findOne(ctx, `
		SELECT `+customerCols+` FROM customers
		WHERE branch_id=$2 AND is_active=true AND merged_into_id IS NULL
		  AND lower(email) = lower($1) AND email <> ''
		LIMIT 1`, email, branchID)
}

func (r *Repository) FindByPassport(ctx context.Context, passport string, branchID uuid.UUID) (*domain.Customer, error) {
	return r.findOne(ctx, `
		SELECT `+customerCols+` FROM customers
		WHERE branch_id=$2 AND is_active=true AND merged_into_id IS NULL
		  AND upper(replace(passport_no,' ','')) = upper(replace($1,' ','')) AND passport_no <> ''
		LIMIT 1`, passport, branchID)
}

func (r *Repository) findOne(ctx context.Context, sql string, args ...any) (*domain.Customer, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	c, err := scan(q.QueryRow(ctx, sql, args...))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *Repository) FindNameCandidates(ctx context.Context, name string, branchID uuid.UUID, limit int) ([]domain.Customer, error) {
	if limit <= 0 {
		limit = 20
	}
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT `+customerCols+` FROM customers
		WHERE branch_id=$1 AND is_active=true AND merged_into_id IS NULL
		  AND (full_name ILIKE $2 OR full_name_ar ILIKE $2)
		ORDER BY updated_at DESC LIMIT $3`,
		branchID, "%"+name+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

func (r *Repository) Search(ctx context.Context, f domain.SearchFilter) ([]domain.Customer, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	pattern := "%" + f.Query + "%"
	activeClause := `AND merged_into_id IS NULL AND is_active=true`
	if f.IncludeMerged {
		activeClause = ""
	}
	var total int
	countSQL := `SELECT COUNT(*) FROM customers WHERE ($1 = '' OR full_name ILIKE $2 OR phone ILIKE $2 OR full_name_ar ILIKE $2 OR passport_no ILIKE $2 OR email ILIKE $2)
		AND ($3::uuid IS NULL OR branch_id = $3) ` + activeClause
	if err := q.QueryRow(ctx, countSQL, f.Query, pattern, f.BranchID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `
		SELECT `+customerCols+`
		FROM customers
		WHERE ($1 = '' OR full_name ILIKE $2 OR phone ILIKE $2 OR full_name_ar ILIKE $2 OR passport_no ILIKE $2 OR email ILIKE $2)
		  AND ($3::uuid IS NULL OR branch_id = $3) `+activeClause+`
		ORDER BY updated_at DESC
		LIMIT $4 OFFSET $5`, f.Query, pattern, f.BranchID, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanAll(rows)
	return items, total, err
}

func (r *Repository) MarkMerged(ctx context.Context, sourceID, targetID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE customers SET merged_into_id=$2, is_active=false, updated_at=NOW() WHERE id=$1`,
		sourceID, targetID)
	return err
}

func (r *Repository) ReassignLeads(ctx context.Context, from, to uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `UPDATE leads SET customer_id=$2, updated_at=NOW() WHERE customer_id=$1`, from, to)
	return err
}

func (r *Repository) ReassignBookings(ctx context.Context, from, to uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `UPDATE bookings SET customer_id=$2, updated_at=NOW() WHERE customer_id=$1`, from, to)
	return err
}

func (r *Repository) ListCompanions(ctx context.Context, customerID uuid.UUID) ([]domain.CompanionLink, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT cc.id, cc.customer_id, cc.companion_id, cc.relation, cc.notes, cc.created_at,
			c.id, c.branch_id, c.full_name, c.full_name_ar, c.phone, c.email, c.nationality,
			COALESCE(c.passport_no,''), c.date_of_birth, COALESCE(c.preferences,'{}'::jsonb),
			COALESCE(c.special_requirements,''), c.notes, c.merged_into_id, COALESCE(c.is_active,true),
			c.created_by, c.created_at, c.updated_at
		FROM customer_companions cc
		JOIN customers c ON c.id = cc.companion_id
		WHERE cc.customer_id=$1
		ORDER BY cc.created_at DESC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CompanionLink
	for rows.Next() {
		var link domain.CompanionLink
		var comp domain.Customer
		if err := rows.Scan(
			&link.ID, &link.CustomerID, &link.CompanionID, &link.Relation, &link.Notes, &link.CreatedAt,
			&comp.ID, &comp.BranchID, &comp.FullName, &comp.FullNameAR, &comp.Phone, &comp.Email, &comp.Nationality,
			&comp.PassportNo, &comp.DateOfBirth, &comp.Preferences, &comp.SpecialRequirements, &comp.Notes,
			&comp.MergedIntoID, &comp.IsActive, &comp.CreatedBy, &comp.CreatedAt, &comp.UpdatedAt,
		); err != nil {
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
	_, err := q.Exec(ctx, `
		INSERT INTO customer_companions (id, customer_id, companion_id, relation, notes, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (customer_id, companion_id) DO UPDATE SET relation=EXCLUDED.relation, notes=EXCLUDED.notes`,
		link.ID, link.CustomerID, link.CompanionID, link.Relation, link.Notes, link.CreatedAt,
	)
	return err
}

func (r *Repository) UnlinkCompanion(ctx context.Context, customerID, companionID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		DELETE FROM customer_companions WHERE customer_id=$1 AND companion_id=$2`, customerID, companionID)
	return err
}

func (r *Repository) ListTimeline(ctx context.Context, customerID uuid.UUID, limit int) ([]domain.TimelineItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		(
			SELECT 'lead'::text, id, coalesce(full_name,'Lead'), stage, updated_at, jsonb_build_object('source', source)
			FROM leads WHERE customer_id=$1
		)
		UNION ALL
		(
			SELECT 'booking', id, 'Booking '||left(id::text,8), status, updated_at,
				jsonb_build_object('balance', balance_amt, 'currency', currency, 'pax', pax_count)
			FROM bookings WHERE customer_id=$1
		)
		UNION ALL
		(
			SELECT 'payment', p.id, 'Payment '||p.amount::text, p.method, p.created_at,
				jsonb_build_object('currency', p.currency, 'booking_id', p.booking_id)
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			WHERE b.customer_id=$1
		)
		UNION ALL
		(
			SELECT 'document', d.id, d.file_name, d.kind, d.created_at,
				jsonb_build_object('related_type', d.related_type)
			FROM documents d WHERE d.related_type='customer' AND d.related_id=$1
		)
		UNION ALL
		(
			SELECT 'task', t.id, t.title, t.status, coalesce(t.updated_at, t.created_at),
				jsonb_build_object('kind', t.kind, 'related_type', t.related_type, 'related_id', t.related_id)
			FROM tasks t
			WHERE (t.related_type='customer' AND t.related_id=$1)
			   OR (t.related_type='lead' AND t.related_id IN (SELECT id FROM leads WHERE customer_id=$1))
			   OR (t.related_type='booking' AND t.related_id IN (SELECT id FROM bookings WHERE customer_id=$1))
		)
		ORDER BY 5 DESC
		LIMIT $2`, customerID, limit)
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

func scan(row scannable) (*domain.Customer, error) {
	var c domain.Customer
	err := row.Scan(
		&c.ID, &c.BranchID, &c.FullName, &c.FullNameAR, &c.Phone, &c.Email, &c.Nationality,
		&c.PassportNo, &c.DateOfBirth, &c.Preferences, &c.SpecialRequirements, &c.Notes,
		&c.MergedIntoID, &c.IsActive, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

func scanAll(rows pgx.Rows) ([]domain.Customer, error) {
	var out []domain.Customer
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
