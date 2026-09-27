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
	a, err := scanAccount(q.QueryRow(ctx, `
		SELECT `+accountCols+`
		FROM integration_accounts WHERE provider=$1 AND external_account_id=$2`, provider, externalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// AccountVerifyTokenMatches checks a Meta subscription verify_token against
// the provider's accounts by blind index, falling back to plaintext
// config_json for rows not yet backfilled.
func (r *Repository) AccountVerifyTokenMatches(ctx context.Context, provider domain.Channel, token, hash string) (bool, error) {
	if token == "" {
		return false, nil
	}
	q := tx.QuerierFrom(ctx, r.pool)
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM integration_accounts
			WHERE provider=$1
			  AND ((verify_token_hash = $3 AND $3 <> '') OR config_json->>'verify_token' = $2)
		)`, provider, token, hash).Scan(&ok)
	return ok, err
}
