package inbox

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// FindAccountByExternalID resolves a webhook's provider-side account id
// (integration_accounts.external_account_id) across all branches.
func (r *Repository) FindAccountByExternalID(ctx context.Context, provider domain.Channel, externalID string) (*domain.IntegrationAccount, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var a domain.IntegrationAccount
	var cfg []byte
	err := q.QueryRow(ctx, `
		SELECT id, branch_id, provider, display_name, status, COALESCE(config_json,'{}'::jsonb), last_ok_at, last_error, updated_at
		FROM integration_accounts WHERE provider=$1 AND external_account_id=$2`, provider, externalID,
	).Scan(&a.ID, &a.BranchID, &a.Provider, &a.DisplayName, &a.Status, &cfg, &a.LastOKAt, &a.LastError, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.ConfigJSON = cfg
	return &a, nil
}

// AccountVerifyTokenExists checks a Meta subscription verify_token against
// connected accounts of the provider.
func (r *Repository) AccountVerifyTokenExists(ctx context.Context, provider domain.Channel, token string) (bool, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM integration_accounts
			WHERE provider=$1 AND config_json->>'verify_token' = $2 AND $2 <> ''
		)`, provider, token).Scan(&ok)
	return ok, err
}
