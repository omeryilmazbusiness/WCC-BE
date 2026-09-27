// Package fx holds the currency conversion contract. Amounts are integer
// minor units; every supported currency uses two decimal places.
package fx

import (
	"context"
	"errors"
	"time"
)

// RateScale is the fixed-point scale of Rate.Scaled (1 unit = 1e8).
const RateScale int64 = 100_000_000

// ErrRateNotFound means no rate is effective for the pair on that date.
var ErrRateNotFound = errors.New("fx: no effective rate")

// Rate converts one unit of Base into Scaled/RateScale units of Quote.
type Rate struct {
	Base          string
	Quote         string
	Scaled        int64
	EffectiveDate time.Time
	Source        string
}

// Conversion is a converted amount together with the rate that produced it,
// so callers can snapshot both.
type Conversion struct {
	Amount int64
	Rate   Rate
}

// Converter converts using the latest rate effective on the given date.
// Same-currency conversion returns the amount unchanged with Scaled=RateScale.
type Converter interface {
	Convert(ctx context.Context, amount int64, from, to string, on time.Time) (Conversion, error)
}
