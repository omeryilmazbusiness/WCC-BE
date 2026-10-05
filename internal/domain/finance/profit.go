package finance

import (
	"slices"
	"strings"

	"github.com/google/uuid"
)

// PnL is the profit picture of one booking, package or departure in one
// currency. Gross is the selling price, Net the supplier cost (net rate);
// taxes and service fees collected on behalf of others are not revenue.
type PnL struct {
	Gross  int64
	Net    int64
	Tax    int64
	Fee    int64
	Costed bool
}

// Revenue is the selling price kept by the agency (gross minus pass-through tax).
func (p PnL) Revenue() int64 { return p.Gross - p.Tax }

// Margin is revenue minus cost. It is 0 until a cost is recorded, so an
// uncosted sale never shows as pure profit.
func (p PnL) Margin() int64 {
	if !p.Costed {
		return 0
	}
	return p.Gross - p.Tax - p.Fee - p.Net
}

// MarginBPS is margin over revenue in basis points.
func (p PnL) MarginBPS() int { return RatioBPS(p.Margin(), p.Revenue()) }

// Add accumulates another P&L (same currency).
func (p PnL) Add(o PnL) PnL {
	return PnL{Gross: p.Gross + o.Gross, Net: p.Net + o.Net, Tax: p.Tax + o.Tax, Fee: p.Fee + o.Fee, Costed: p.Costed || o.Costed}
}

// Budget is the planned revenue and cost of a departure (group, Hajj,
// Umrah tour) set by finance before sales.
type Budget struct {
	DepartureID uuid.UUID
	Currency    string
	Revenue     int64
	Cost        int64
	Note        string
}

// Normalize validates a budget.
func (b *Budget) Normalize() error {
	f := fields{}
	cur, ok := NormalizeCurrency(b.Currency)
	b.Currency = cur
	b.Note = strings.TrimSpace(b.Note)
	if !ok {
		f.add("currency", "ISO 4217 code")
	}
	if b.Revenue < 0 || b.Revenue > MaxMoney {
		f.add("revenue", "out of range")
	}
	if b.Cost < 0 || b.Cost > MaxMoney {
		f.add("cost", "out of range")
	}
	if len(b.Note) > 500 {
		f.add("note", "too long")
	}
	return f.err("invalid budget")
}

// Margin is the planned margin.
func (b Budget) Margin() int64 { return b.Revenue - b.Cost }

// Variance compares actuals with the plan: positive revenue/margin variance
// is good, positive cost variance is an overrun.
type Variance struct {
	Revenue int64
	Cost    int64
	Margin  int64
	// CostOverrun is true when actual cost passed the planned cost.
	CostOverrun bool
}

// VarianceOf compares an actual P&L with a budget.
func VarianceOf(b Budget, actual PnL) Variance {
	v := Variance{
		Revenue: actual.Revenue() - b.Revenue,
		Cost:    actual.Net - b.Cost,
		Margin:  actual.Margin() - b.Margin(),
	}
	v.CostOverrun = b.Cost > 0 && actual.Net > b.Cost
	return v
}

// DefaultCommissionBPS pays sales staff 10 % of the margin they close.
const DefaultCommissionBPS = 1_000

// MaxCommissionRateBPS caps the commission rate at 50 % of margin.
const MaxCommissionRateBPS = 5_000

// Commission is what a sales representative earns on a margin: a share of
// positive margin only — a loss-making sale earns nothing and is not
// clawed back from other sales.
func Commission(margin int64, bps int) int64 {
	if margin <= 0 || bps <= 0 {
		return 0
	}
	return ApplyBPS(margin, bps)
}

// RepEarnings is one representative's period in one currency.
type RepEarnings struct {
	UserID     uuid.UUID
	Name       string
	Currency   string
	Bookings   int
	Uncosted   int
	PnL        PnL
	Commission int64
}

// Settle computes the commission on each booking margin and sums it.
func (r *RepEarnings) Settle(margins []int64, bps int) {
	r.Commission = 0
	for _, m := range margins {
		r.Commission += Commission(m, bps)
	}
}

// Exposure share of one currency in the cash position.
type CurrencyShare struct {
	Currency  string
	Amount    int64
	Converted int64
	ShareBPS  int
	// Unconverted is true when no FX rate was available; the amount is then
	// left out of the shares.
	Unconverted bool
}

// Shares turns converted per-currency amounts into a distribution that
// sums to 10000 bps (largest remainder), largest first. Negative balances
// are excluded from the distribution.
func Shares(in []CurrencyShare) []CurrencyShare {
	out := slices.Clone(in)
	var total int64
	for _, s := range out {
		if !s.Unconverted && s.Converted > 0 {
			total += s.Converted
		}
	}
	if total == 0 {
		return out
	}
	type rem struct {
		i int
		r int64
	}
	var rems []rem
	assigned := 0
	for i, s := range out {
		if s.Unconverted || s.Converted <= 0 {
			out[i].ShareBPS = 0
			continue
		}
		num := s.Converted * BPS
		out[i].ShareBPS = int(num / total)
		assigned += out[i].ShareBPS
		rems = append(rems, rem{i, num % total})
	}
	slices.SortStableFunc(rems, func(a, b rem) int {
		switch {
		case a.r > b.r:
			return -1
		case a.r < b.r:
			return 1
		}
		return 0
	})
	for k := 0; assigned < BPS && k < len(rems); k++ {
		out[rems[k].i].ShareBPS++
		assigned++
	}
	slices.SortStableFunc(out, func(a, b CurrencyShare) int {
		switch {
		case a.Converted > b.Converted:
			return -1
		case a.Converted < b.Converted:
			return 1
		}
		return strings.Compare(a.Currency, b.Currency)
	})
	return out
}
