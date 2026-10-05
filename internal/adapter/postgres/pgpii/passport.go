// Package pgpii encrypts passport numbers for the postgres repositories:
// ciphertext in passport_enc, a blind index in passport_hash for exact
// lookups and the clear last four characters in passport_last4 (T-260).
package pgpii

import (
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
)

// Tables holding encrypted passports; the name is part of the AAD so a
// ciphertext cannot be moved between tables or rows.
const (
	Customers           = "customers"
	BookingParticipants = "booking_participants"
)

// Sealed is the stored form of one passport number.
type Sealed struct {
	Enc   string
	Hash  string
	Last4 string
}

type Passports struct {
	cipher crypto.FieldCipher
}

func NewPassports(c crypto.FieldCipher) *Passports {
	return &Passports{cipher: c}
}

func aad(table string, id uuid.UUID) string {
	return table + ".passport:" + id.String()
}

// Seal normalizes and encrypts plain for row id of table; blank input
// yields the zero Sealed.
func (p *Passports) Seal(table string, id uuid.UUID, plain string) (Sealed, error) {
	n := shared.NormalizePassport(plain)
	if n == "" {
		return Sealed{}, nil
	}
	enc, err := p.cipher.Encrypt(n, aad(table, id))
	if err != nil {
		return Sealed{}, err
	}
	return Sealed{Enc: enc, Hash: p.cipher.BlindIndex(n), Last4: shared.PassportLast4(n)}, nil
}

// Open returns the row's passport. The application always blanks the legacy
// plaintext column, so a non-empty one is either not yet backfilled or was
// written by an older release after enc, and wins.
func (p *Passports) Open(table string, id uuid.UUID, enc, legacy string) (string, error) {
	if legacy != "" || enc == "" {
		return legacy, nil
	}
	return p.cipher.Decrypt(enc, aad(table, id))
}

// SealNationalID encrypts a national identity number (e.g. TCKN). It is
// write-only: reads expose the clear last four characters, never the
// plaintext, so there is no Open counterpart.
func (p *Passports) SealNationalID(table string, id uuid.UUID, plain string) (enc, last4 string, err error) {
	n := shared.NormalizePassport(plain)
	if n == "" {
		return "", "", nil
	}
	enc, err = p.cipher.Encrypt(n, table+".national_id:"+id.String())
	if err != nil {
		return "", "", err
	}
	if len(n) > 4 {
		n = n[len(n)-4:]
	}
	return enc, n, nil
}

// Hash is the blind index used for exact passport lookups; "" never matches.
func (p *Passports) Hash(plain string) string {
	n := shared.NormalizePassport(plain)
	if n == "" {
		return ""
	}
	return p.cipher.BlindIndex(n)
}
