package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgpii"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/search"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
	pii  *pgpii.Passports
}

func NewRepository(pool *pgxpool.Pool, pii *pgpii.Passports) *Repository {
	return &Repository{pool: pool, pii: pii}
}

// Search spans customers (branch-shared), leads and bookings (branch + owner)
// and booking participants (via their booking), each filtered by the
// caller's scope; branchID nil searches every branch the scope allows.
// Passport hits match the full number exactly (blind index) and carry only
// the last four characters as subtitle.
func (r *Repository) Search(ctx context.Context, branchID *uuid.UUID, query domain.Query) ([]domain.Hit, error) {
	q, limit := query.Text, query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	pat := "%" + strings.ToLower(strings.TrimSpace(q)) + "%"
	args := []any{branchID, q, pat, r.pii.Hash(q), shared.NormalizePassport(q), kindFilter(query.Kinds)}
	var sc [3]string
	for i, cols := range []pgscope.Columns{
		{Branch: "c.branch_id"},
		{Branch: "l.branch_id", Owner: "l.owner_id"},
		{Branch: "b.branch_id", Owner: "b.owner_id"},
	} {
		var err error
		if sc[i], args, err = pgscope.Clause(ctx, cols, args); err != nil {
			return nil, err
		}
	}
	csc, lsc, bsc := sc[0], sc[1], sc[2]
	args = append(args, limit)
	querier := tx.QuerierFrom(ctx, r.pool)
	rows, err := querier.Query(ctx, fmt.Sprintf(`
		(
			SELECT 'customer'::text AS kind, c.id,
				c.full_name AS title,
				COALESCE(NULLIF(c.phone,''), NULLIF(c.email,''), '') AS subtitle,
				'customers'::text AS href_hint,
				CASE
					WHEN lower(c.full_name) = lower($2) THEN 100
					WHEN lower(c.phone) LIKE $3 THEN 90
					WHEN lower(c.email) LIKE $3 THEN 85
					ELSE 70
				END AS score
			FROM customers c
			WHERE ($1::uuid IS NULL OR c.branch_id=$1)`+csc+`
			  AND ($6::text[] IS NULL OR 'customer' = ANY($6::text[]))
			  AND (lower(c.full_name) LIKE $3 OR lower(c.phone) LIKE $3 OR lower(c.email) LIKE $3)
		)
		UNION ALL
		(
			SELECT 'lead', l.id, l.full_name,
				COALESCE(NULLIF(l.phone,''), l.source, ''),
				'leads',
				CASE WHEN lower(l.full_name) = lower($2) THEN 95 ELSE 65 END
			FROM leads l
			WHERE ($1::uuid IS NULL OR l.branch_id=$1) AND l.deleted_at IS NULL`+lsc+`
			  AND ($6::text[] IS NULL OR 'lead' = ANY($6::text[]))
			  AND (lower(l.full_name) LIKE $3 OR lower(l.phone) LIKE $3)
		)
		UNION ALL
		(
			SELECT 'booking', b.id,
				'Booking '||left(b.id::text,8),
				COALESCE(c.full_name,''),
				'bookings',
				CASE WHEN b.id::text ILIKE $3 THEN 100 ELSE 60 END
			FROM bookings b
			JOIN customers c ON c.id = b.customer_id
			WHERE ($1::uuid IS NULL OR b.branch_id=$1)`+bsc+`
			  AND ($6::text[] IS NULL OR 'booking' = ANY($6::text[]))
			  AND (
				b.id::text ILIKE $3
				OR lower(c.full_name) LIKE $3
				OR lower(c.phone) LIKE $3
			  )
		)
		UNION ALL
		(
			SELECT 'passport', p.id,
				p.full_name,
				CASE
					WHEN p.passport_last4 <> '' THEN p.passport_last4
					WHEN length(replace(p.passport_no,' ','')) > 4 THEN right(upper(replace(p.passport_no,' ','')), 4)
					ELSE ''
				END,
				'bookings',
				80
			FROM booking_participants p
			JOIN bookings b ON b.id = p.booking_id
			WHERE ($1::uuid IS NULL OR b.branch_id=$1)`+bsc+`
			  AND ($6::text[] IS NULL OR 'passport' = ANY($6::text[]))
			  AND $4 <> ''
			  AND (p.passport_hash = $4
			    OR (p.passport_no <> '' AND upper(replace(p.passport_no,' ','')) = $5))
		)
		ORDER BY score DESC, title
		LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Hit
	for rows.Next() {
		var h domain.Hit
		var kind string
		if err := rows.Scan(&kind, &h.ID, &h.Title, &h.Subtitle, &h.HrefHint, &h.Score); err != nil {
			return nil, err
		}
		h.Kind = domain.Kind(kind)
		out = append(out, h)
	}
	return out, rows.Err()
}

// kindFilter is NULL (every kind) when no kinds were requested.
func kindFilter(kinds []domain.Kind) []string {
	if len(kinds) == 0 {
		return nil
	}
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = string(k)
	}
	return out
}
