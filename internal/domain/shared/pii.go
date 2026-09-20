package shared

import (
	"strings"
	"unicode"
)

// NormalizePhone strips spaces/dashes for duplicate matching.
func NormalizePhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if unicode.IsDigit(r) || r == '+' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizeEmail lowercases and trims.
func NormalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

// NormalizePassport uppercases and strips spaces.
func NormalizePassport(p string) string {
	p = strings.ToUpper(strings.TrimSpace(p))
	return strings.ReplaceAll(p, " ", "")
}

// NameSimilarity is a lightweight fuzzy score 0..100 (token overlap + prefix).
func NameSimilarity(a, b string) int {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	a = strings.ReplaceAll(a, "-", " ")
	b = strings.ReplaceAll(b, "-", " ")
	a = strings.Join(strings.Fields(a), " ")
	b = strings.Join(strings.Fields(b), " ")
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 100
	}
	at := strings.Fields(a)
	bt := strings.Fields(b)
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}
	set := map[string]struct{}{}
	for _, t := range bt {
		set[t] = struct{}{}
	}
	hit := 0
	for _, t := range at {
		if _, ok := set[t]; ok {
			hit++
		}
	}
	den := len(at)
	if len(bt) > den {
		den = len(bt)
	}
	score := (hit * 100) / den
	// prefix bonus
	min := a
	if len(b) < len(a) {
		min = b
	}
	if strings.HasPrefix(a, min[:minInt(3, len(min))]) || strings.HasPrefix(b, min[:minInt(3, len(min))]) {
		if score < 60 {
			score += 15
		}
	}
	if score > 100 {
		score = 100
	}
	return score
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MaskPassport redacts middle characters for list responses (PII minimize).
func MaskPassport(p string) string {
	p = strings.TrimSpace(p)
	if len(p) <= 4 {
		if p == "" {
			return ""
		}
		return "****"
	}
	return p[:2] + strings.Repeat("*", len(p)-4) + p[len(p)-2:]
}
