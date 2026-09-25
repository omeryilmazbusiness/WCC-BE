package hardening

import (
	"strings"
	"unicode"
)

const (
	MaxIdempotencyKeyLen = 128
	MinIdempotencyKeyLen = 8
)

// NormalizeIdempotencyKey trims and validates webhook/import/payment keys (T-209).
// Returns empty string when invalid — callers must reject empty keys.
func NormalizeIdempotencyKey(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) < MinIdempotencyKeyLen || len(s) > MaxIdempotencyKeyLen {
		return ""
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return s
}

// SameEffect reports whether two successful responses for the same key must be treated
// as identical (idempotent replay). Used by tests and gateway adapters.
func SameEffect(firstID, secondID string) bool {
	return firstID != "" && firstID == secondID
}
