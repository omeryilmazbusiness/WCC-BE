package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// LiveRepository stores fx_live_snapshots (domain fx.SnapshotRepository).
type LiveRepository struct {
	pool *pgxpool.Pool
}

func NewLiveRepository(pool *pgxpool.Pool) *LiveRepository {
	return &LiveRepository{pool: pool}
}

type livePayload struct {
	Quotes []liveQuote `json:"quotes"`
}

type liveQuote struct {
	Currency   string `json:"currency"`
	Buy        string `json:"buy,omitempty"`
	Sell       string `json:"sell,omitempty"`
	Mid        string `json:"mid"`
	ObservedAt string `json:"observed_at"`
}

func encodeQuotes(qs []domain.Quote) ([]byte, error) {
	p := livePayload{Quotes: make([]liveQuote, 0, len(qs))}
	for _, q := range qs {
		lq := liveQuote{Currency: q.Currency, Mid: domain.FormatRate(q.Mid), ObservedAt: q.ObservedAt.UTC().Format(time.RFC3339Nano)}
		if q.Buy > 0 {
			lq.Buy = domain.FormatRate(q.Buy)
		}
		if q.Sell > 0 {
			lq.Sell = domain.FormatRate(q.Sell)
		}
		p.Quotes = append(p.Quotes, lq)
	}
	return json.Marshal(p)
}

func decodeQuotes(raw []byte) ([]domain.Quote, error) {
	var p livePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	out := make([]domain.Quote, 0, len(p.Quotes))
	for _, lq := range p.Quotes {
		q := domain.Quote{Currency: lq.Currency}
		var err error
		if q.Mid, err = domain.ParseRate(lq.Mid); err != nil {
			return nil, fmt.Errorf("quote %s mid: %w", lq.Currency, err)
		}
		if lq.Buy != "" {
			if q.Buy, err = domain.ParseRate(lq.Buy); err != nil {
				return nil, fmt.Errorf("quote %s buy: %w", lq.Currency, err)
			}
		}
		if lq.Sell != "" {
			if q.Sell, err = domain.ParseRate(lq.Sell); err != nil {
				return nil, fmt.Errorf("quote %s sell: %w", lq.Currency, err)
			}
		}
		if q.ObservedAt, err = time.Parse(time.RFC3339Nano, lq.ObservedAt); err != nil {
			return nil, fmt.Errorf("quote %s observed_at: %w", lq.Currency, err)
		}
		out = append(out, q)
	}
	return out, nil
}

func (r *LiveRepository) List(ctx context.Context) ([]domain.SourceSnapshot, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT source, kind, payload, fetched_at, next_update_at, ok, error, updated_at
		FROM fx_live_snapshots ORDER BY source, kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SourceSnapshot
	for rows.Next() {
		var (
			s          domain.SourceSnapshot
			kind       string
			payload    []byte
			fetched    *time.Time
			nextUpdate *time.Time
		)
		if err := rows.Scan(&s.Source, &kind, &payload, &fetched, &nextUpdate, &s.OK, &s.Error, &s.AttemptedAt); err != nil {
			return nil, err
		}
		s.Kind = domain.SourceKind(kind)
		if fetched != nil {
			s.FetchedAt = *fetched
		}
		if nextUpdate != nil {
			s.NextUpdateAt = *nextUpdate
		}
		if s.Quotes, err = decodeQuotes(payload); err != nil {
			return nil, fmt.Errorf("fx_live_snapshots %s/%s: %w", s.Source, kind, err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *LiveRepository) SaveSuccess(ctx context.Context, s domain.SourceSnapshot) error {
	payload, err := encodeQuotes(s.Quotes)
	if err != nil {
		return err
	}
	var next *time.Time
	if !s.NextUpdateAt.IsZero() {
		next = &s.NextUpdateAt
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err = q.Exec(ctx, `
		INSERT INTO fx_live_snapshots (source, kind, payload, fetched_at, next_update_at, ok, error, updated_at)
		VALUES ($1, $2, $3, $4, $5, TRUE, '', $4)
		ON CONFLICT (source, kind) DO UPDATE SET
			payload = EXCLUDED.payload, fetched_at = EXCLUDED.fetched_at, next_update_at = EXCLUDED.next_update_at,
			ok = TRUE, error = '', updated_at = EXCLUDED.updated_at`,
		s.Source, string(s.Kind), payload, s.FetchedAt, next)
	return err
}

func (r *LiveRepository) SaveFailure(ctx context.Context, source string, kind domain.SourceKind, msg string, at time.Time) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO fx_live_snapshots (source, kind, ok, error, updated_at)
		VALUES ($1, $2, FALSE, $3, $4)
		ON CONFLICT (source, kind) DO UPDATE SET ok = FALSE, error = EXCLUDED.error, updated_at = EXCLUDED.updated_at`,
		source, string(kind), msg, at)
	return err
}
