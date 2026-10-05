package finance

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Letter parties.
const (
	PartySupplier = "supplier"
	PartyAgency   = "agency"
)

// Letter statuses. Expiry is derived from ExpiresAt, not stored.
const (
	LetterSent      = "sent"
	LetterConfirmed = "confirmed"
	LetterDisputed  = "disputed"
	LetterExpired   = "expired"
)

// LetterTTL is how long a counterparty may answer.
const LetterTTL = 14 * 24 * time.Hour

// Letter is a balance confirmation (cari mutabakat) sent to a supplier or
// an agency. Balance > 0 means the party owes the agency; < 0 means the
// agency owes the party.
type Letter struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	PartyType    string
	PartyID      uuid.UUID
	PartyName    string
	PeriodEnd    time.Time
	Balance      int64
	Currency     string
	Email        string
	TokenHash    string
	Status       string
	ResponseNote string
	RespondedBy  string
	RespondedAt  *time.Time
	ExpiresAt    time.Time
	CreatedBy    *uuid.UUID
	CreatedAt    time.Time
}

// NewLetterToken returns a URL-safe secret and its storage hash. Only the
// hash is persisted; the secret travels in the confirmation link.
func NewLetterToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashLetterToken(token), nil
}

// HashLetterToken is the lookup key of a confirmation token.
func HashLetterToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// StatusOn is the effective status, with unanswered letters past their
// deadline shown as expired.
func (l Letter) StatusOn(now time.Time) string {
	if l.Status == LetterSent && now.After(l.ExpiresAt) {
		return LetterExpired
	}
	return l.Status
}

// Respond records the counterparty's answer.
func (l *Letter) Respond(confirm bool, by, note string, now time.Time) error {
	by, note = strings.TrimSpace(by), strings.TrimSpace(note)
	switch l.StatusOn(now) {
	case LetterExpired:
		return shared.NewInvalidState("this confirmation request has expired")
	case LetterConfirmed, LetterDisputed:
		return shared.NewInvalidState("this confirmation request was already answered")
	}
	if by == "" || len(by) > 120 {
		return fieldErr("name", "1-120 characters")
	}
	if len(note) > 1000 {
		return fieldErr("note", "too long")
	}
	if !confirm && note == "" {
		return fieldErr("note", "explain the difference you see")
	}
	l.Status = LetterDisputed
	if confirm {
		l.Status = LetterConfirmed
	}
	l.RespondedBy, l.ResponseNote = by, note
	t := now
	l.RespondedAt = &t
	return nil
}
