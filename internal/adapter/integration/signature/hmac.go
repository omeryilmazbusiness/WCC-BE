// Package signature verifies inbound webhook signatures per provider.
package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

var (
	ErrMissing  = errors.New("webhook signature missing")
	ErrInvalid  = errors.New("webhook signature invalid")
	ErrNoSecret = errors.New("webhook secret not configured")
)

// Meta header: X-Hub-Signature-256: sha256=<hex hmac of raw body>.
const (
	MetaHeader = "x-hub-signature-256"
	WodiHeader = "x-wodi-signature"
)

// HMACSHA256 verifies "<prefix><hex(HMAC-SHA256(secret, body))>" in a header.
// Header names are matched lowercase to fit the ingest header map.
type HMACSHA256 struct {
	Header string
	Prefix string
}

// Meta covers WhatsApp Cloud API, Instagram and Messenger webhooks.
func Meta() HMACSHA256 { return HMACSHA256{Header: MetaHeader, Prefix: "sha256="} }

// Shared is the generic shared-secret scheme for email/gmail relays.
func Shared() HMACSHA256 { return HMACSHA256{Header: WodiHeader, Prefix: "sha256="} }

func (v HMACSHA256) Verify(headers map[string]string, body []byte, secret string) error {
	if secret == "" {
		return ErrNoSecret
	}
	got := strings.TrimSpace(headers[strings.ToLower(v.Header)])
	if got == "" {
		return ErrMissing
	}
	if v.Prefix != "" {
		if len(got) < len(v.Prefix) || !strings.EqualFold(got[:len(v.Prefix)], v.Prefix) {
			return ErrInvalid
		}
		got = got[len(v.Prefix):]
	}
	sig, err := hex.DecodeString(got)
	if err != nil || len(sig) != sha256.Size {
		return ErrInvalid
	}
	if !hmac.Equal(sig, Sign(body, secret)) {
		return ErrInvalid
	}
	return nil
}

// Sign returns the raw HMAC-SHA256 of body.
func Sign(body []byte, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return mac.Sum(nil)
}

// SignHeader renders a header value in the "sha256=<hex>" form.
func SignHeader(body []byte, secret string) string {
	return "sha256=" + hex.EncodeToString(Sign(body, secret))
}
