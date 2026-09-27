package crypto

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// Binding is the row a sealed secret bag belongs to. It is the AAD, so a
// ciphertext copied to another row, table or branch fails to open.
type Binding struct {
	Table    string
	RowID    uuid.UUID
	BranchID uuid.UUID
}

func (b Binding) aad() string {
	return b.Table + ":" + b.RowID.String() + ":" + b.BranchID.String()
}

// SecretSealer seals a set of named secrets into one opaque string.
type SecretSealer interface {
	// Seal returns "" for an empty bag.
	Seal(secrets map[string]string, b Binding) (string, error)
	// Open returns an empty bag for "".
	Open(sealed string, b Binding) (map[string]string, error)
}

// SecretBox is a SecretSealer over a Cipher.
type SecretBox struct {
	cipher Cipher
}

func NewSecretBox(c Cipher) *SecretBox {
	return &SecretBox{cipher: c}
}

var errUnboundSecret = errors.New("crypto: secret binding requires table and row id")

func (s *SecretBox) Seal(secrets map[string]string, b Binding) (string, error) {
	if b.Table == "" || b.RowID == uuid.Nil {
		return "", errUnboundSecret
	}
	if len(secrets) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(secrets)
	if err != nil {
		return "", err
	}
	return s.cipher.Encrypt(string(raw), b.aad())
}

func (s *SecretBox) Open(sealed string, b Binding) (map[string]string, error) {
	if sealed == "" {
		return map[string]string{}, nil
	}
	if b.Table == "" || b.RowID == uuid.Nil {
		return nil, errUnboundSecret
	}
	plain, err := s.cipher.Decrypt(sealed, b.aad())
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, ErrMalformed
	}
	return out, nil
}
