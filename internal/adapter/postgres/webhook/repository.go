package webhook

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgbatch"
	appwebhook "github.com/wodi-crm/wodi-crm-be/internal/app/webhook"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Repository journals inbound webhooks in webhook_events.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Insert(ctx context.Context, e *appwebhook.Event) (bool, error) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	payload := e.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	q := tx.QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `
		INSERT INTO webhook_events (id, provider, external_event_id, external_account_id, integration_account_id,
			branch_id, signature_valid, status, error, source_ip, payload, received_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)
		ON CONFLICT (provider, external_event_id) WHERE status <> 'rejected' DO NOTHING`,
		e.ID, e.Provider, e.ExternalEventID, e.ExternalAccountID, e.IntegrationAccountID,
		e.BranchID, e.SignatureValid, e.Status, e.Error, e.SourceIP, string(payload), e.ReceivedAt,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) FindByExternalID(ctx context.Context, provider domain.Channel, externalEventID string) (*appwebhook.Event, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var e appwebhook.Event
	err := q.QueryRow(ctx, `
		SELECT id, provider, external_event_id, external_account_id, integration_account_id, branch_id,
		       signature_valid, status, error, source_ip, received_at, processed_at, attempts
		FROM webhook_events
		WHERE provider=$1 AND external_event_id=$2 AND status <> 'rejected'`, provider, externalEventID,
	).Scan(&e.ID, &e.Provider, &e.ExternalEventID, &e.ExternalAccountID, &e.IntegrationAccountID, &e.BranchID,
		&e.SignatureValid, &e.Status, &e.Error, &e.SourceIP, &e.ReceivedAt, &e.ProcessedAt, &e.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *Repository) MarkStatus(ctx context.Context, id uuid.UUID, status, errMsg string, at time.Time) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE webhook_events SET status=$2, error=$3, processed_at=$4 WHERE id=$1`, id, status, errMsg, at)
	return err
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, nextRetry *time.Time, at time.Time) error {
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		UPDATE webhook_events SET status='failed', error=$2, processed_at=$3,
			attempts = attempts + 1, next_retry_at = $4
		WHERE id=$1`, id, errMsg, at, nextRetry)
	return err
}

func (r *Repository) ListRetryable(ctx context.Context, now time.Time, limit int) ([]appwebhook.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT id, provider, external_event_id, external_account_id, integration_account_id, branch_id,
		       signature_valid, status, error, source_ip, payload, received_at, processed_at, attempts
		FROM webhook_events
		WHERE status='failed' AND next_retry_at IS NOT NULL AND next_retry_at <= $1
		ORDER BY next_retry_at ASC LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appwebhook.Event
	for rows.Next() {
		var e appwebhook.Event
		if err := rows.Scan(&e.ID, &e.Provider, &e.ExternalEventID, &e.ExternalAccountID, &e.IntegrationAccountID,
			&e.BranchID, &e.SignatureValid, &e.Status, &e.Error, &e.SourceIP, &e.Payload, &e.ReceivedAt,
			&e.ProcessedAt, &e.Attempts); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeWebhookEvents deletes journal rows received before cutoff.
func (r *Repository) PurgeWebhookEvents(ctx context.Context, cutoff time.Time) (int64, error) {
	return pgbatch.Delete(ctx, tx.QuerierFrom(ctx, r.pool), `
		DELETE FROM webhook_events WHERE id IN (
			SELECT id FROM webhook_events WHERE received_at < $1 LIMIT `+strconv.Itoa(pgbatch.Size)+`)`, cutoff)
}

var _ appwebhook.EventStore = (*Repository)(nil)
