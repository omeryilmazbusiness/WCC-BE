package shared

import (
	"bytes"
	"encoding/json"
	"strings"
)

// secretKeyMarkers name config keys that hold credentials. Identifiers such
// as client_id, page_id or phone_number_id stay public because webhook
// routing (integration_accounts.external_account_id) is derived from them.
var secretKeyMarkers = []string{"secret", "token", "password", "passwd", "api_key", "apikey", "private_key", "credential"}

// IsSecretKey reports whether a config key holds a credential.
func IsSecretKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, m := range secretKeyMarkers {
		if strings.Contains(k, m) {
			return true
		}
	}
	return false
}

// PartitionSecrets splits a flat string config into public keys and secrets.
func PartitionSecrets(cfg map[string]string) (public, secrets map[string]string) {
	public, secrets = map[string]string{}, map[string]string{}
	for k, v := range cfg {
		if IsSecretKey(k) {
			if v != "" {
				secrets[k] = v
			}
			continue
		}
		public[k] = v
	}
	return public, secrets
}

// SplitSecrets separates credential keys from a JSON object config. Secret
// values must be strings; an empty value is kept so callers can treat it as
// "clear this secret".
func SplitSecrets(cfg json.RawMessage) (json.RawMessage, map[string]string, error) {
	obj, err := configObject(cfg)
	if err != nil {
		return nil, nil, err
	}
	secrets := map[string]string{}
	for k, raw := range obj {
		if !IsSecretKey(k) {
			continue
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, nil, NewValidation("config_json." + k + " must be a string")
		}
		secrets[k] = strings.TrimSpace(v)
		delete(obj, k)
	}
	public, err := json.Marshal(obj)
	if err != nil {
		return nil, nil, err
	}
	return public, secrets, nil
}

// ApplySecretChanges returns current updated with incoming: non-empty values
// replace, empty values remove, absent keys are kept.
func ApplySecretChanges(current, incoming map[string]string) map[string]string {
	out := make(map[string]string, len(current)+len(incoming))
	for k, v := range current {
		out[k] = v
	}
	for k, v := range incoming {
		if v == "" {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

// MergeSecrets overlays secrets onto a public config for adapters that need
// the full credential set. The result must never be persisted or returned.
func MergeSecrets(public json.RawMessage, secrets map[string]string) json.RawMessage {
	obj, err := configObject(public)
	if err != nil {
		obj = map[string]json.RawMessage{}
	}
	for k, v := range secrets {
		if v == "" {
			continue
		}
		raw, _ := json.Marshal(v)
		obj[k] = raw
	}
	out, _ := json.Marshal(obj)
	return out
}

// SecretHints maps each secret key to a display hint.
func SecretHints(secrets map[string]string) map[string]string {
	out := make(map[string]string, len(secrets))
	for k, v := range secrets {
		out[k] = SecretHint(v)
	}
	return out
}

// SecretHint reveals at most the last four characters of a credential.
func SecretHint(v string) string {
	v = strings.TrimSpace(v)
	if len(v) <= 4 {
		return "••••"
	}
	return "••••" + v[len(v)-4:]
}

func configObject(cfg json.RawMessage) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	trimmed := bytes.TrimSpace(cfg)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return obj, nil
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, NewValidation("config_json must be a JSON object")
	}
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}
	return obj, nil
}
