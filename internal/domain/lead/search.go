package lead

import (
	"strings"
	"time"
	"unicode"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// RetentionMonths is how far back every lead stays searchable. Live leads
// are never purged automatically; this is the window the archive guarantees.
const RetentionMonths = 12

// MaxQueryLength bounds a search string (runes) so a pasted document cannot
// turn into an expensive pattern.
const MaxQueryLength = 100

// MaxQueryTokens caps the words matched together; extra words are ignored.
const MaxQueryTokens = 5

// SearchTerms is a normalised free-text query: every token must appear in the
// lead's name, email, company or source (lowercase), or a token made of
// digits may match the phone ignoring formatting.
type SearchTerms struct {
	Tokens []string
	// Raw is the trimmed query, used to match owner names as a whole.
	Raw string
}

// Empty reports whether there is nothing to search for.
func (s SearchTerms) Empty() bool { return len(s.Tokens) == 0 }

// ParseSearch splits a query into lowercase tokens. Phone-like input such as
// "+90 555 123" stays one digits-only token so it matches however the number
// was stored.
func ParseSearch(raw string) (SearchTerms, error) {
	q := strings.TrimSpace(raw)
	if q == "" {
		return SearchTerms{}, nil
	}
	if len([]rune(q)) > MaxQueryLength {
		return SearchTerms{}, shared.NewValidation("search is too long")
	}
	if d := Digits(q); d != "" && isPhoneLike(q) {
		return SearchTerms{Tokens: []string{d}, Raw: q}, nil
	}
	var tokens []string
	seen := map[string]bool{}
	for _, f := range strings.Fields(strings.ToLower(q)) {
		f = strings.Trim(f, ",;")
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		tokens = append(tokens, f)
		if len(tokens) == MaxQueryTokens {
			break
		}
	}
	return SearchTerms{Tokens: tokens, Raw: q}, nil
}

// IsPhoneToken reports whether a token is all digits and may match a phone.
func IsPhoneToken(tok string) bool {
	return tok != "" && Digits(tok) == tok
}

// Digits keeps only ASCII digits.
func Digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isPhoneLike is true when s is only digits and phone punctuation.
func isPhoneLike(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', unicode.IsSpace(r), r == '+', r == '-', r == '(', r == ')', r == '.':
		default:
			return false
		}
	}
	return true
}

// EscapeLike escapes LIKE wildcards so user input matches literally (escape char \).
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// RetentionStart is the oldest created_at the archive guarantees at now.
func RetentionStart(now time.Time) time.Time {
	return now.AddDate(0, -RetentionMonths, 0)
}

func ValidSegment(s Segment) bool { return s == SegmentB2C || s == SegmentB2B }

// ValidateFilter rejects contradictory or unknown filter values.
func ValidateFilter(f ListFilter) error {
	if f.CreatedFrom != nil && f.CreatedTo != nil && !f.CreatedTo.After(*f.CreatedFrom) {
		return shared.NewValidation("created_to must be after created_from")
	}
	if f.LostReason != "" && !ValidLostReasonCode(f.LostReason) {
		return shared.NewValidation("invalid lost_reason")
	}
	if f.Segment != "" && !ValidSegment(f.Segment) {
		return shared.NewValidation("invalid segment")
	}
	return nil
}
