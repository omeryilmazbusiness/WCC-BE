package fx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Repository stores company-wide FX rates; rows carry no branch scope.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const rateCols = `id, base, quote, rate_scaled, effective_date, source, created_by, created_at`

func scanRate(scan func(dest ...any) error) (*domain.StoredRate, error) {
	var r domain.StoredRate
	if err := scan(&r.ID, &r.Base, &r.Quote, &r.Scaled, &r.EffectiveDate, &r.Source, &r.CreatedBy, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func (r *Repository) insert(ctx context.Context, rate *domain.StoredRate) (bool, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	err := q.QueryRow(ctx, `
		INSERT INTO fx_rates (id, base, quote, rate_scaled, effective_date, source, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (base, quote, effective_date) DO NOTHING
		RETURNING id`,
		rate.ID, rate.Base, rate.Quote, rate.Scaled, rate.EffectiveDate, rate.Source, rate.CreatedBy, rate.CreatedAt,
	).Scan(&rate.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) Insert(ctx context.Context, rate *domain.StoredRate) error {
	ok, err := r.insert(ctx, rate)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrRateExists
	}
	return nil
}

func (r *Repository) InsertIfAbsent(ctx context.Context, rate *domain.StoredRate) (bool, error) {
	return r.insert(ctx, rate)
}

func (r *Repository) UpdateRate(ctx context.Context, id uuid.UUID, scaled int64, source string) error {
	q := tx.QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `UPDATE fx_rates SET rate_scaled=$2, source=$3 WHERE id=$1`, id, scaled, source)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("fx rate")
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `DELETE FROM fx_rates WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("fx rate")
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*domain.StoredRate, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rate, err := scanRate(q.QueryRow(ctx, `SELECT `+rateCols+` FROM fx_rates WHERE id=$1`, id).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("fx rate")
	}
	return rate, err
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.StoredRate, int64, error) {
	var where []string
	var args []any
	add := func(clause string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.Base != "" {
		add("base=$%d", f.Base)
	}
	if f.Quote != "" {
		add("quote=$%d", f.Quote)
	}
	if f.From != nil {
		add("effective_date >= $%d", *f.From)
	}
	if f.To != nil {
		add("effective_date <= $%d", *f.To)
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}
	q := tx.QuerierFrom(ctx, r.pool)
	var total int64
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM fx_rates`+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := q.Query(ctx, fmt.Sprintf(`SELECT `+rateCols+` FROM fx_rates`+cond+`
		ORDER BY effective_date DESC, base, quote LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.StoredRate{}
	for rows.Next() {
		rate, err := scanRate(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *rate)
	}
	return out, total, rows.Err()
}

func (r *Repository) Latest(ctx context.Context, base, quote string, on time.Time) (*domain.Rate, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var rate domain.Rate
	err := q.QueryRow(ctx, `
		SELECT base, quote, rate_scaled, effective_date, source FROM fx_rates
		WHERE base=$1 AND quote=$2 AND effective_date <= $3
		ORDER BY effective_date DESC LIMIT 1`, base, quote, on,
	).Scan(&rate.Base, &rate.Quote, &rate.Scaled, &rate.EffectiveDate, &rate.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rate, nil
}
