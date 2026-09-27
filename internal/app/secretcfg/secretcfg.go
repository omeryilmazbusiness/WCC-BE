// Package secretcfg keeps credentials of JSON-configured rows (integration
// accounts, AI settings, external integrations, file sync) out of
// config_json: secrets live in a sealed bag (secrets_enc) bound to the row,
// the rest of the config stays queryable (T-259).
package secretcfg

import (
	"encoding/json"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
)

// Config is a row's configuration split for storage and display.
type Config struct {
	Public  json.RawMessage
	Secrets map[string]string
}

// Merged is the full credential set for provider adapters; never persist or
// return it.
func (c Config) Merged() json.RawMessage {
	return shared.MergeSecrets(c.Public, c.Secrets)
}

// Hints are "••••1234" display hints per secret key.
func (c Config) Hints() map[string]string {
	return shared.SecretHints(c.Secrets)
}

// Vault loads and seals row configs.
type Vault struct {
	sealer crypto.SecretSealer
}

func NewVault(s crypto.SecretSealer) Vault {
	return Vault{sealer: s}
}

// Load opens a stored row. Secret keys still in config_json (rows written
// before the encrypt backfill) are read as a fallback; sealed values win.
func (v Vault) Load(configJSON json.RawMessage, sealed string, b crypto.Binding) (Config, error) {
	public, legacy, err := shared.SplitSecrets(configJSON)
	if err != nil {
		return Config{}, err
	}
	opened, err := v.sealer.Open(sealed, b)
	if err != nil {
		return Config{}, err
	}
	secrets := shared.ApplySecretChanges(nil, legacy)
	for k, val := range opened {
		secrets[k] = val
	}
	return Config{Public: public, Secrets: secrets}, nil
}

// Apply replaces the public config with incoming's and merges its secrets:
// non-empty values replace, empty values clear, omitted keys are kept, so a
// client echoing the redacted config back does not wipe credentials.
func (v Vault) Apply(current Config, incoming json.RawMessage) (Config, error) {
	public, secrets, err := shared.SplitSecrets(incoming)
	if err != nil {
		return Config{}, err
	}
	return Config{Public: public, Secrets: shared.ApplySecretChanges(current.Secrets, secrets)}, nil
}

// Seal returns the secrets_enc value for c.
func (v Vault) Seal(c Config, b crypto.Binding) (string, error) {
	return v.sealer.Seal(c.Secrets, b)
}
