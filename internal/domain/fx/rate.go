package fx

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// RateDecimals is the number of fractional digits a rate may carry.
const RateDecimals = 8

// ErrOverflow means a converted amount does not fit into int64 minor units.
var ErrOverflow = errors.New("fx: amount overflow")

var bigScale = big.NewInt(RateScale)

// ParseRate turns a positive decimal string ("3.75") into its exact scaled
// value. Signs, exponents and more than RateDecimals fractional digits are
// rejected so no precision is ever lost.
func ParseRate(s string) (int64, error) {
	s = strings.TrimSpace(s)
	intPart, frac, hasDot := strings.Cut(s, ".")
	if intPart == "" || !digitsOnly(intPart) || (hasDot && (frac == "" || !digitsOnly(frac))) {
		return 0, shared.NewValidation("rate must be a positive decimal like 3.75")
	}
	if len(frac) > RateDecimals {
		return 0, shared.NewValidation(fmt.Sprintf("rate supports at most %d decimal places", RateDecimals))
	}
	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil || whole > (math.MaxInt64-RateScale)/RateScale {
		return 0, shared.NewValidation("rate is too large")
	}
	fracScaled := int64(0)
	if frac != "" {
		fracScaled, _ = strconv.ParseInt(frac+strings.Repeat("0", RateDecimals-len(frac)), 10, 64)
	}
	scaled := whole*RateScale + fracScaled
	if scaled <= 0 {
		return 0, shared.NewValidation("rate must be greater than zero")
	}
	return scaled, nil
}

// FormatRate renders a positive scaled rate with all RateDecimals digits ("3.75000000").
func FormatRate(scaled int64) string {
	return fmt.Sprintf("%d.%08d", scaled/RateScale, scaled%RateScale)
}

// Apply converts amount (minor units) at scaled, rounding half away from zero.
// Every supported currency has two decimals, so minor units map 1:1.
func Apply(amount, scaled int64) (int64, error) {
	if scaled <= 0 {
		return 0, shared.NewValidation("rate must be greater than zero")
	}
	n := new(big.Int).Mul(big.NewInt(amount), big.NewInt(scaled))
	return divRound(n, bigScale)
}

// Invert returns 1/scaled at the same scale, rounded half away from zero.
func Invert(scaled int64) (int64, error) {
	if scaled <= 0 {
		return 0, shared.NewValidation("rate must be greater than zero")
	}
	n := new(big.Int).Mul(bigScale, bigScale)
	inv, err := divRound(n, big.NewInt(scaled))
	if err != nil {
		return 0, err
	}
	if inv == 0 {
		return 0, shared.NewValidation("rate is too large to invert")
	}
	return inv, nil
}

// Identity is the rate used when both currencies are equal.
func Identity(currency string, on time.Time) Rate {
	return Rate{Base: currency, Quote: currency, Scaled: RateScale, EffectiveDate: DateOf(on), Source: "identity"}
}

// Pick chooses between the latest direct (from→to) and inverse (to→from)
// rates: the more recent effective date wins, a tie prefers direct. An
// inverse rate is inverted first so the returned rate always reads from→to.
func Pick(direct, inverse *Rate) (Rate, error) {
	switch {
	case direct == nil && inverse == nil:
		return Rate{}, ErrRateNotFound
	case inverse == nil || (direct != nil && !inverse.EffectiveDate.After(direct.EffectiveDate)):
		return *direct, nil
	}
	scaled, err := Invert(inverse.Scaled)
	if err != nil {
		return Rate{}, err
	}
	return Rate{
		Base: inverse.Quote, Quote: inverse.Base, Scaled: scaled,
		EffectiveDate: inverse.EffectiveDate, Source: inverse.Source + " (inverse)",
	}, nil
}

// NormalizeCurrency upper-cases and validates an ISO-4217 code.
func NormalizeCurrency(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != 3 {
		return "", shared.NewValidation("currency must be ISO-4217 (3 letters)")
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return "", shared.NewValidation("currency must be ISO-4217 (3 letters)")
		}
	}
	return s, nil
}

// DateOf truncates t to its calendar date (in t's location) at UTC midnight,
// matching how DATE columns round-trip.
func DateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ParseDate parses YYYY-MM-DD.
func ParseDate(s string) (time.Time, error) {
	d, err := time.Parse(time.DateOnly, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, shared.NewValidation("date must be YYYY-MM-DD")
	}
	return d, nil
}

func divRound(n, d *big.Int) (int64, error) {
	q, r := new(big.Int).QuoRem(n, d, new(big.Int))
	if new(big.Int).Mul(new(big.Int).Abs(r), big.NewInt(2)).Cmp(new(big.Int).Abs(d)) >= 0 {
		if (n.Sign() < 0) != (d.Sign() < 0) {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	if !q.IsInt64() {
		return 0, ErrOverflow
	}
	return q.Int64(), nil
}

func digitsOnly(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
