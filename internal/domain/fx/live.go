package fx

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// JobLiveSync refreshes the live rate board from every LiveSource.
const JobLiveSync shared.JobName = "fx.live_sync"

// SourceKind classifies what a live quote means.
type SourceKind string

const (
	// KindOfficial is the central bank rate.
	KindOfficial SourceKind = "official"
	// KindMarket is the local parallel (street) market rate.
	KindMarket SourceKind = "market"
	// KindReference is an international mid-market cross rate against USD.
	KindReference SourceKind = "reference"
)

func (k SourceKind) Valid() bool {
	return k == KindOfficial || k == KindMarket || k == KindReference
}

// Quote is one observed rate. For official and market kinds Buy/Sell/Mid are
// local currency per 1 unit of Currency; for reference, Mid is units of
// Currency per 1 USD and Buy/Sell are zero. All values use RateScale.
type Quote struct {
	Currency   string
	Buy        int64
	Sell       int64
	Mid        int64
	ObservedAt time.Time
}

// SourceSnapshot is the stored state of one (source, kind). Quotes and
// FetchedAt describe the last successful fetch; OK, Error and AttemptedAt
// describe the last attempt, so a failure keeps the previous quotes.
type SourceSnapshot struct {
	Source string
	Kind   SourceKind
	Quotes []Quote
	// FetchedAt is zero when the source never succeeded.
	FetchedAt   time.Time
	AttemptedAt time.Time
	// NextUpdateAt is when the provider publishes new data (zero = unknown).
	NextUpdateAt time.Time
	OK           bool
	Error        string
}

// LiveSourceInfo describes a live feed.
type LiveSourceInfo struct {
	ID          string
	Name        string
	URL         string
	Attribution string
	Kinds       []SourceKind
	// MinInterval is the shortest gap between fetches the provider tolerates.
	MinInterval time.Duration
}

// LiveSource is a live rate feed. Fetch returns one snapshot per kind it
// serves; any error fails the whole source for that attempt.
type LiveSource interface {
	Info() LiveSourceInfo
	Fetch(ctx context.Context) ([]SourceSnapshot, error)
}

// SnapshotRepository stores the latest snapshot per (source, kind).
type SnapshotRepository interface {
	List(ctx context.Context) ([]SourceSnapshot, error)
	// SaveSuccess replaces the quotes of (s.Source, s.Kind) and marks it OK.
	SaveSuccess(ctx context.Context, s SourceSnapshot) error
	// SaveFailure records a failed attempt, keeping previous quotes and FetchedAt.
	SaveFailure(ctx context.Context, source string, kind SourceKind, msg string, at time.Time) error
}

// maxSYPPerUSD bounds new-lira data: Syria redenominated on 2026-01-01
// (1 new SYP = 100 old, ISO code unchanged), so a larger value is old-lira
// data and must be rejected, never rescaled.
const maxSYPPerUSD = 2000 * RateScale

// ErrOldLira means a payload still quotes pre-redenomination SYP.
var ErrOldLira = errors.New("fx: SYP per USD above 2000 looks like old (pre-2026) lira data")

// Validate checks a freshly fetched snapshot before it is stored.
func (s SourceSnapshot) Validate(local string) error {
	if !s.Kind.Valid() {
		return fmt.Errorf("fx: unknown source kind %q", s.Kind)
	}
	if len(s.Quotes) == 0 {
		return fmt.Errorf("fx: %s %s returned no quotes", s.Source, s.Kind)
	}
	seen := make(map[string]bool, len(s.Quotes))
	for _, q := range s.Quotes {
		if c, err := NormalizeCurrency(q.Currency); err != nil || c != q.Currency {
			return fmt.Errorf("fx: invalid currency %q", q.Currency)
		}
		if seen[q.Currency] {
			return fmt.Errorf("fx: duplicate quote for %s", q.Currency)
		}
		seen[q.Currency] = true
		if q.Mid <= 0 || q.ObservedAt.IsZero() {
			return fmt.Errorf("fx: incomplete quote for %s", q.Currency)
		}
		if s.Kind != KindReference && (q.Buy <= 0 || q.Sell <= 0) {
			return fmt.Errorf("fx: quote for %s lacks buy/sell", q.Currency)
		}
		if local == "SYP" && s.localPerUSD(q) > maxSYPPerUSD {
			return ErrOldLira
		}
	}
	return nil
}

// localPerUSD is the SYP-per-USD value q carries, or 0 when it carries none.
func (s SourceSnapshot) localPerUSD(q Quote) int64 {
	switch {
	case s.Kind == KindReference && q.Currency == "SYP":
		return q.Mid
	case s.Kind != KindReference && q.Currency == "USD":
		return max(q.Buy, q.Sell, q.Mid)
	}
	return 0
}

var decimalText = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]{1,2})?$`)

// ParseDecimal converts provider decimal text (JSON number syntax, small
// exponents allowed) into a positive scaled value rounded half away from zero
// to RateDecimals. It never goes through floating point.
func ParseDecimal(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if len(s) > 64 || !decimalText.MatchString(s) {
		return 0, fmt.Errorf("fx: %q is not a positive decimal", s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() <= 0 {
		return 0, fmt.Errorf("fx: %q is not a positive decimal", s)
	}
	v, err := divRound(new(big.Int).Mul(r.Num(), bigScale), r.Denom())
	if err != nil {
		return 0, err
	}
	if v <= 0 {
		return 0, fmt.Errorf("fx: %q is below %d decimals", s, RateDecimals)
	}
	return v, nil
}

// BoardConfig drives ComposeBoard.
type BoardConfig struct {
	Local string
	// Pinned currencies come first, in this order.
	Pinned []string
	// Currencies follow the pinned ones, in this order.
	Currencies []string
	// MarketMaxAge is how old a direct market quote may be before the board
	// prefers a value derived from the market USD rate.
	MarketMaxAge time.Duration
	// StaleAfter marks the board stale when the newest official/market fetch is older.
	StaleAfter time.Duration
}

// BoardSide is one resolved rate (local currency per 1 unit).
type BoardSide struct {
	Buy        int64
	Sell       int64
	Mid        int64
	ObservedAt time.Time
	// Derived means computed from the USD rate and a reference cross.
	Derived bool
	// Stale means the underlying market observation is older than MarketMaxAge.
	Stale bool
}

type BoardQuote struct {
	Currency string
	Pinned   bool
	Official *BoardSide
	Market   *BoardSide
	// USDCross is reference units of Currency per 1 USD; 0 when unknown.
	USDCross int64
}

type Board struct {
	Local string
	// UpdatedAt is the newest successful official/market fetch; zero if none.
	UpdatedAt time.Time
	Stale     bool
	Quotes    []BoardQuote
}

// ComposeBoard resolves official and market rates for every configured
// currency. Snapshots earlier in the slice win when several serve a kind.
func ComposeBoard(snaps []SourceSnapshot, now time.Time, cfg BoardConfig) Board {
	byKind := map[SourceKind]map[string]Quote{}
	board := Board{Local: cfg.Local}
	for _, s := range snaps {
		if s.FetchedAt.IsZero() || len(s.Quotes) == 0 {
			continue
		}
		if s.Kind != KindReference && s.FetchedAt.After(board.UpdatedAt) {
			board.UpdatedAt = s.FetchedAt
		}
		if _, taken := byKind[s.Kind]; taken {
			continue
		}
		m := make(map[string]Quote, len(s.Quotes))
		for _, q := range s.Quotes {
			m[q.Currency] = q
		}
		byKind[s.Kind] = m
	}
	board.Stale = board.UpdatedAt.IsZero() || now.Sub(board.UpdatedAt) > cfg.StaleAfter

	c := composer{
		official: byKind[KindOfficial], market: byKind[KindMarket], ref: byKind[KindReference],
		now: now, maxAge: cfg.MarketMaxAge,
	}
	for _, cur := range boardOrder(cfg) {
		q := BoardQuote{Currency: cur.code, Pinned: cur.pinned, Official: c.resolveOfficial(cur.code), Market: c.resolveMarket(cur.code)}
		if x, ok := c.cross(cur.code); ok {
			q.USDCross = x.Mid
		}
		board.Quotes = append(board.Quotes, q)
	}
	return board
}

type boardCurrency struct {
	code   string
	pinned bool
}

func boardOrder(cfg BoardConfig) []boardCurrency {
	seen := map[string]bool{cfg.Local: true}
	var out []boardCurrency
	add := func(list []string, pinned bool) {
		for _, c := range list {
			if !seen[c] {
				seen[c] = true
				out = append(out, boardCurrency{code: c, pinned: pinned})
			}
		}
	}
	add(cfg.Pinned, true)
	add(cfg.Currencies, false)
	return out
}

type composer struct {
	official, market, ref map[string]Quote
	now                   time.Time
	maxAge                time.Duration
}

// cross is the reference quote of units of c per USD (exactly 1 for USD).
func (c composer) cross(code string) (Quote, bool) {
	if code == "USD" {
		observed := time.Time{}
		if q, ok := c.ref["USD"]; ok {
			observed = q.ObservedAt
		}
		return Quote{Currency: "USD", Mid: RateScale, ObservedAt: observed}, true
	}
	q, ok := c.ref[code]
	return q, ok
}

func (c composer) resolveOfficial(code string) *BoardSide {
	if q, ok := c.official[code]; ok {
		return direct(q, false)
	}
	usd, ok := c.official["USD"]
	if !ok || code == "USD" {
		return nil
	}
	x, ok := c.cross(code)
	if !ok {
		return nil
	}
	return derive(usd, x)
}

// resolveMarket prefers a fresh direct quote, then one derived from the
// market USD rate, then a stale direct quote.
func (c composer) resolveMarket(code string) *BoardSide {
	q, hasDirect := c.market[code]
	if hasDirect && c.fresh(q) {
		return direct(q, false)
	}
	if usd, ok := c.market["USD"]; ok && code != "USD" {
		if x, ok := c.cross(code); ok {
			if side := derive(usd, x); side != nil {
				side.Stale = !c.fresh(usd)
				return side
			}
		}
	}
	if hasDirect {
		return direct(q, true)
	}
	return nil
}

func (c composer) fresh(q Quote) bool { return c.now.Sub(q.ObservedAt) <= c.maxAge }

func direct(q Quote, stale bool) *BoardSide {
	return &BoardSide{Buy: q.Buy, Sell: q.Sell, Mid: q.Mid, ObservedAt: q.ObservedAt, Stale: stale}
}

// derive turns local-per-USD into local-per-unit using units-per-USD.
func derive(usd, x Quote) *BoardSide {
	buy, errB := divScaled(usd.Buy, x.Mid)
	sell, errS := divScaled(usd.Sell, x.Mid)
	mid, errM := divScaled(usd.Mid, x.Mid)
	if errB != nil || errS != nil || errM != nil || buy <= 0 || sell <= 0 || mid <= 0 {
		return nil
	}
	observed := usd.ObservedAt
	if !x.ObservedAt.IsZero() && x.ObservedAt.Before(observed) {
		observed = x.ObservedAt
	}
	return &BoardSide{Buy: buy, Sell: sell, Mid: mid, ObservedAt: observed, Derived: true}
}

// divScaled returns a/b for two scaled values, at scale, half away from zero.
func divScaled(a, b int64) (int64, error) {
	if b <= 0 {
		return 0, ErrOverflow
	}
	return divRound(new(big.Int).Mul(big.NewInt(a), bigScale), big.NewInt(b))
}
