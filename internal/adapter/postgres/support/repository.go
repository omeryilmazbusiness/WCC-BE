// Package support stores help requests.
package support

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appsupport "github.com/wodi-crm/wodi-crm-be/internal/app/support"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/support"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Repository reads requests across companies: requesters only reach their own
// rows (filtered by requester id) and the inbox is behind support.manage.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const columns = `r.id, r.number, r.company_id, r.branch_id, r.requester_id, r.title, r.description, r.status,
	r.page, r.locale, r.admin_note, r.resolved_at, r.resolved_by, r.created_at, r.updated_at`

func scanRequest(row pgx.Row, extra ...any) (*domain.Request, error) {
	var r domain.Request
	var status string
	dest := append([]any{&r.ID, &r.Number, &r.CompanyID, &r.BranchID, &r.RequesterID, &r.Title, &r.Description, &status,
		&r.Page, &r.Locale, &r.AdminNote, &r.ResolvedAt, &r.ResolvedBy, &r.CreatedAt, &r.UpdatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NewNotFound("support request")
		}
		return nil, err
	}
	r.Status = domain.Status(status)
	return &r, nil
}

// Create stores the request with its company taken from the branch, and fills in the number.
func (r *Repository) Create(ctx context.Context, req *domain.Request) error {
	return tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO support_requests (id, company_id, branch_id, requester_id, title, description, status, page, locale, created_at, updated_at)
		VALUES ($1, (SELECT company_id FROM branches WHERE id = $2), $2, $3, $4, $5, $6, $7, $8, $9, $9)
		RETURNING number, company_id`,
		req.ID, req.BranchID, req.RequesterID, req.Title, req.Description, string(req.Status), req.Page, req.Locale, req.CreatedAt,
	).Scan(&req.Number, &req.CompanyID)
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*domain.Request, error) {
	return scanRequest(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `SELECT `+columns+` FROM support_requests r WHERE r.id = $1`, id))
}

func (r *Repository) Update(ctx context.Context, req *domain.Request) error {
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		UPDATE support_requests SET status = $2, admin_note = $3, resolved_at = $4, resolved_by = $5, updated_at = $6
		WHERE id = $1`,
		req.ID, string(req.Status), req.AdminNote, req.ResolvedAt, req.ResolvedBy, req.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("support request")
	}
	return nil
}

func (r *Repository) ListByRequester(ctx context.Context, requesterID uuid.UUID, limit int) ([]domain.Request, error) {
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx,
		`SELECT `+columns+` FROM support_requests r WHERE r.requester_id = $1 ORDER BY r.created_at DESC LIMIT $2`,
		requesterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Request{}
	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *req)
	}
	return out, rows.Err()
}

// ListInbound pages the inbox, open work first, then newest.
func (r *Repository) ListInbound(ctx context.Context, f appsupport.Filter) ([]appsupport.Inbound, int, error) {
	where := []string{"TRUE"}
	args := []any{}
	if f.Status != "" {
		args = append(args, string(f.Status))
		where = append(where, "r.status = $1")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		n := len(args)
		where = append(where, "(r.title ILIKE $"+strconv.Itoa(n)+" OR u.full_name ILIKE $"+strconv.Itoa(n)+" OR u.email ILIKE $"+strconv.Itoa(n)+" OR c.name_en ILIKE $"+strconv.Itoa(n)+")")
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT `+columns+`, u.full_name, u.email, u.role, COALESCE(c.name_en, ''), b.name_en, COUNT(*) OVER ()
		FROM support_requests r
		JOIN users u ON u.id = r.requester_id
		JOIN branches b ON b.id = r.branch_id
		LEFT JOIN companies c ON c.id = r.company_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY CASE r.status WHEN 'open' THEN 0 WHEN 'in_progress' THEN 1 ELSE 2 END, r.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []appsupport.Inbound{}
	total := 0
	for rows.Next() {
		var in appsupport.Inbound
		req, err := scanRequest(rows, &in.RequesterName, &in.RequesterEmail, &in.RequesterRole, &in.CompanyName, &in.BranchName, &total)
		if err != nil {
			return nil, 0, err
		}
		in.Request = *req
		out = append(out, in)
	}
	return out, total, rows.Err()
}

func (r *Repository) CountByStatus(ctx context.Context) (map[domain.Status]int, error) {
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `SELECT status, COUNT(*) FROM support_requests GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[domain.Status]int{}
	for _, s := range domain.Statuses() {
		out[s] = 0
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[domain.Status(s)] = n
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
