package search

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/search"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Search(ctx context.Context, branchID uuid.UUID, q string, limit int) ([]domain.Hit, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	pat := "%" + strings.ToLower(strings.TrimSpace(q)) + "%"
	querier := tx.QuerierFrom(ctx, r.pool)
	rows, err := querier.Query(ctx, `
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
			WHERE c.branch_id=$1
			  AND (lower(c.full_name) LIKE $3 OR lower(c.phone) LIKE $3 OR lower(c.email) LIKE $3)
		)
		UNION ALL
		(
			SELECT 'lead', l.id, l.full_name,
				COALESCE(NULLIF(l.phone,''), l.source, ''),
				'leads',
				CASE WHEN lower(l.full_name) = lower($2) THEN 95 ELSE 65 END
			FROM leads l
			WHERE l.branch_id=$1
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
			WHERE b.branch_id=$1
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
				COALESCE(NULLIF(p.passport_no,''), ''),
				'bookings',
				80
			FROM booking_participants p
			JOIN bookings b ON b.id = p.booking_id
			WHERE b.branch_id=$1
			  AND p.passport_no <> ''
			  AND lower(p.passport_no) LIKE $3
		)
		ORDER BY score DESC, title
		LIMIT $4`, branchID, q, pat, limit)
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
