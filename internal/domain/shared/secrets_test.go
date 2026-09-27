package shared

import (
	"encoding/json"
	"testing"
)

func TestIsSecretKey(t *testing.T) {
	for _, k := range []string{"api_key", "access_token", "refresh_token", "client_secret", "verify_token", "Password", "webhook_secret"} {
		if !IsSecretKey(k) {
			t.Errorf("%s must be secret", k)
		}
	}
	for _, k := range []string{"client_id", "phone_number_id", "page_id", "mailbox_email", "external_account_id", "tenant"} {
		if IsSecretKey(k) {
			t.Errorf("%s must stay public", k)
		}
	}
}

func TestSplitAndMergeSecrets(t *testing.T) {
	public, secrets, err := SplitSecrets(json.RawMessage(`{"tenant":"t1","api_key":"sk-1","retries":3,"password":""}`))
	if err != nil {
		t.Fatal(err)
	}
	var pub map[string]any
	_ = json.Unmarshal(public, &pub)
	if _, leaked := pub["api_key"]; leaked || pub["tenant"] != "t1" || pub["retries"] != float64(3) {
		t.Fatalf("public=%s", public)
	}
	if _, kept := pub["password"]; kept || secrets["api_key"] != "sk-1" || secrets["password"] != "" || len(secrets) != 2 {
		t.Fatalf("secrets=%v public=%s", secrets, public)
	}
	applied := ApplySecretChanges(map[string]string{"password": "old", "token": "t"}, secrets)
	if _, kept := applied["password"]; kept || applied["token"] != "t" || applied["api_key"] != "sk-1" {
		t.Fatalf("applied=%v", applied)
	}
	var merged map[string]any
	_ = json.Unmarshal(MergeSecrets(public, secrets), &merged)
	if merged["api_key"] != "sk-1" || merged["tenant"] != "t1" {
		t.Fatalf("merged=%v", merged)
	}
	if _, _, err := SplitSecrets(json.RawMessage(`{"api_key":42}`)); err == nil {
		t.Fatal("non-string secret must be rejected")
	}
	if _, _, err := SplitSecrets(json.RawMessage(`[1]`)); err == nil {
		t.Fatal("non-object config must be rejected")
	}
	if p, s, err := SplitSecrets(nil); err != nil || string(p) != "{}" || len(s) != 0 {
		t.Fatalf("empty config: %s %v %v", p, s, err)
	}
}

func TestSecretHintAndPassportMask(t *testing.T) {
	if SecretHint("sk-abcdef1234") != "••••1234" || SecretHint("abc") != "••••" {
		t.Fatal("secret hint")
	}
	if PassportLast4(" u 1234 5678 ") != "5678" || PassportLast4("A12") != "" {
		t.Fatal("last4")
	}
	if MaskedPassport("A12345678") != "••••5678" || MaskedPassport("A1") != "••••" || MaskedPassport("") != "" {
		t.Fatal("masked passport")
	}
	if MaskPassportLast4("") != "" {
		t.Fatal("no passport renders empty")
	}
}
