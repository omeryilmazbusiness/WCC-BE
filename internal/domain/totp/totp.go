// Package totp implements RFC 6238 time-based one-time passwords
// (HMAC-SHA1, 30s step, 6 digits) with replay protection by time step.
package totp

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238 default; authenticator apps expect SHA1.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

const (
	Period     = 30 * time.Second
	Digits     = 6
	Skew       = 1
	SecretSize = 20
)

var (
	ErrInvalidCode   = errors.New("totp: invalid code")
	ErrReplay        = errors.New("totp: code already used")
	ErrInvalidSecret = errors.New("totp: invalid secret")
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret returns a new base32 (unpadded) secret of SecretSize bytes.
func GenerateSecret(random io.Reader) (string, error) {
	buf := make([]byte, SecretSize)
	if _, err := io.ReadFull(random, buf); err != nil {
		return "", err
	}
	return b32.EncodeToString(buf), nil
}

// DecodeSecret accepts base32 with or without padding, spaces or lowercase.
func DecodeSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	s = strings.TrimRight(s, "=")
	raw, err := b32.DecodeString(s)
	if err != nil || len(raw) == 0 {
		return nil, ErrInvalidSecret
	}
	return raw, nil
}

// Step returns the RFC 6238 time counter for t.
func Step(t time.Time) int64 {
	return t.Unix() / int64(Period/time.Second)
}

// Code returns the 6-digit code for a counter.
func Code(key []byte, counter int64) string {
	return code(key, counter, Digits)
}

func code(key []byte, counter int64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

// Verify checks code against now ±Skew steps. lastStep is the last accepted
// step for this secret; steps at or before it are rejected as replays.
// It returns the matched step, which the caller must persist as lastStep.
func Verify(secret, candidate string, now time.Time, lastStep int64) (int64, error) {
	key, err := DecodeSecret(secret)
	if err != nil {
		return 0, err
	}
	candidate = NormalizeCode(candidate)
	if len(candidate) != Digits {
		return 0, ErrInvalidCode
	}
	current := Step(now)
	matched := int64(-1)
	for d := -Skew; d <= Skew; d++ {
		s := current + int64(d)
		if subtle.ConstantTimeCompare([]byte(Code(key, s)), []byte(candidate)) == 1 {
			matched = s
		}
	}
	if matched < 0 {
		return 0, ErrInvalidCode
	}
	if matched <= lastStep {
		return 0, ErrReplay
	}
	return matched, nil
}

// NormalizeCode strips spaces and dashes users commonly type.
func NormalizeCode(c string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(c))
}

// LooksLikeCode reports whether c is a Digits-long numeric code.
func LooksLikeCode(c string) bool {
	c = NormalizeCode(c)
	if len(c) != Digits {
		return false
	}
	for _, r := range c {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// URL builds an otpauth:// provisioning URI for authenticator apps.
func URL(issuer, account, secret string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(Digits))
	q.Set("period", fmt.Sprint(int(Period/time.Second)))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
