package inbox

import (
	"context"
	"errors"

	"github.com/wodi-crm/wodi-crm-be/internal/app/secretcfg"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
)

var errSecretsUnconfigured = errors.New("inbox: integration secret encryption is not configured")

// SetSecrets enables sealing of integration account credentials (T-259).
// Connect and account listings fail closed until it is called.
func (s *Service) SetSecrets(sealer crypto.SecretSealer, index crypto.BlindIndexer) {
	s.secrets = &accountSecrets{vault: secretcfg.NewVault(sealer), index: index}
}

type accountSecrets struct {
	vault secretcfg.Vault
	index crypto.BlindIndexer
}

func accountBinding(a *domain.IntegrationAccount) crypto.Binding {
	return crypto.Binding{Table: domain.AccountSecretsTable, RowID: a.ID, BranchID: a.BranchID}
}

func (a *accountSecrets) load(acc *domain.IntegrationAccount) (secretcfg.Config, error) {
	return a.vault.Load(acc.ConfigJSON, acc.SecretsEnc, accountBinding(acc))
}

// verifyTokenHash is empty for an empty token so it never matches.
func (a *accountSecrets) verifyTokenHash(token string) string {
	if token == "" {
		return ""
	}
	return a.index.BlindIndex(token)
}

// AccountSecretLookup is the unscoped webhook account lookup backed by storage.
type AccountSecretLookup interface {
	FindAccountByExternalID(ctx context.Context, provider domain.Channel, externalID string) (*domain.IntegrationAccount, error)
	AccountVerifyTokenMatches(ctx context.Context, provider domain.Channel, token, hash string) (bool, error)
}

// WebhookAccounts serves webhook.AccountLookup with account credentials
// opened in memory, so signature checks see the sealed webhook secrets.
type WebhookAccounts struct {
	lookup  AccountSecretLookup
	secrets *accountSecrets
}

func NewWebhookAccounts(lookup AccountSecretLookup, sealer crypto.SecretSealer, index crypto.BlindIndexer) *WebhookAccounts {
	return &WebhookAccounts{lookup: lookup, secrets: &accountSecrets{vault: secretcfg.NewVault(sealer), index: index}}
}

func (w *WebhookAccounts) FindAccountByExternalID(ctx context.Context, provider domain.Channel, externalID string) (*domain.IntegrationAccount, error) {
	acc, err := w.lookup.FindAccountByExternalID(ctx, provider, externalID)
	if err != nil || acc == nil {
		return acc, err
	}
	cfg, err := w.secrets.load(acc)
	if err != nil {
		return nil, err
	}
	acc.ConfigJSON, acc.SecretsEnc = cfg.Merged(), ""
	return acc, nil
}

func (w *WebhookAccounts) AccountVerifyTokenExists(ctx context.Context, provider domain.Channel, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	return w.lookup.AccountVerifyTokenMatches(ctx, provider, token, w.secrets.verifyTokenHash(token))
}
