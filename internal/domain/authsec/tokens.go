package authsec

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RecoveryCodeCount is how many one-time recovery codes are issued at once.
const RecoveryCodeCount = 10

// Challenge purposes for mfa_challenges.purpose.
const (
	ChallengeLogin  = "login"
	ChallengeEnroll = "enroll"
)

// Challenge is a short-lived, attempt-limited second-factor ticket.
type Challenge struct {
	TokenHash string
	UserID    uuid.UUID
	Purpose   string
	Attempts  int
	ExpiresAt time.Time
	CreatedIP string
}

// HashToken is the storage form of opaque bearer tokens (challenges,
// refresh tokens, recovery codes). Inputs carry ≥80 bits of entropy.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// NewOpaqueToken returns a URL-safe random token of n bytes of entropy.
func NewOpaqueToken(random io.Reader, n int) (string, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(random, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

var recoveryAlphabet = base32.NewEncoding("abcdefghijkmnpqrstuvwxyz23456789").WithPadding(base32.NoPadding)

// NewRecoveryCodes returns RecoveryCodeCount codes formatted xxxx-xxxx-xxxx-xxxx
// (80 bits each) with their storage hashes.
func NewRecoveryCodes(random io.Reader) (codes, hashes []string, err error) {
	codes = make([]string, 0, RecoveryCodeCount)
	hashes = make([]string, 0, RecoveryCodeCount)
	for i := 0; i < RecoveryCodeCount; i++ {
		buf := make([]byte, 10)
		if _, err := io.ReadFull(random, buf); err != nil {
			return nil, nil, err
		}
		s := recoveryAlphabet.EncodeToString(buf)
		code := s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
		codes = append(codes, code)
		hashes = append(hashes, HashRecoveryCode(code))
	}
	return codes, hashes, nil
}

// NormalizeRecoveryCode lowercases and strips separators.
func NormalizeRecoveryCode(code string) string {
	return strings.NewReplacer("-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(code)))
}

func HashRecoveryCode(code string) string {
	return HashToken("recovery:" + NormalizeRecoveryCode(code))
}
