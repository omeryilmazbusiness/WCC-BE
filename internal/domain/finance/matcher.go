package finance

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Booking references customers write in a transfer description: the
// human booking number ("BK-001042", "WCC-1042", "REZ 1042", "#1042") and the airline
// or supplier PNR (6 alphanumerics with at least one letter and digit).
var (
	refNoPattern = regexp.MustCompile(`(?i)(?:\b(?:WCC|REZ|RES|BOOKING|BKG|BK|REF)[\s\-_:#]*|#)(\d{1,10})\b`)
	pnrPattern   = regexp.MustCompile(`\b[A-Z0-9]{6}\b`)
	hasLetter    = regexp.MustCompile(`[A-Z]`)
	hasDigit     = regexp.MustCompile(`[0-9]`)
)

// ExtractRefs finds booking numbers and PNR-like tokens in free text.
func ExtractRefs(text string) (refNos []int64, pnrs []string) {
	for _, m := range refNoPattern.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n > 0 && !slices.Contains(refNos, n) {
			refNos = append(refNos, n)
		}
	}
	for _, tok := range pnrPattern.FindAllString(strings.ToUpper(text), -1) {
		if hasLetter.MatchString(tok) && hasDigit.MatchString(tok) && !slices.Contains(pnrs, tok) {
			pnrs = append(pnrs, tok)
		}
	}
	return refNos, pnrs
}

// BookingDue is an open booking a bank credit may settle.
type BookingDue struct {
	BookingID    uuid.UUID
	RefNo        int64
	PNR          string
	Currency     string
	Balance      int64
	CustomerName string
}

// MatchCredit picks the booking an incoming transfer pays: the only
// candidate in the same currency whose open balance covers the amount.
// Ambiguity (zero or several candidates) is left for a human.
func MatchCredit(amount int64, currency string, candidates []BookingDue) (BookingDue, bool) {
	var hit []BookingDue
	seen := map[uuid.UUID]bool{}
	for _, c := range candidates {
		if seen[c.BookingID] || c.Currency != currency || c.Balance < amount || c.Balance <= 0 {
			continue
		}
		seen[c.BookingID] = true
		hit = append(hit, c)
	}
	if len(hit) != 1 {
		return BookingDue{}, false
	}
	return hit[0], true
}
