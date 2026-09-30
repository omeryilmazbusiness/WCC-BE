package dashboard

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// DueSoonWindow is how far ahead instalments count as "due soon".
const DueSoonWindow = 30 * 24 * time.Hour

// Amount is a money sum in minor units of Currency. Readers return rows
// already converted through stored FX snapshots in the reporting currency;
// rows without a snapshot keep their original currency.
type Amount struct {
	Currency string
	Minor    int64
	Count    int
}

// MethodAmounts are collected payments for one payment method.
type MethodAmounts struct {
	Method  string
	Amounts []Amount
}

// DayAmounts is net money received on one calendar day (UTC).
type DayAmounts struct {
	Day     time.Time
	Amounts []Amount
}

// RevenueFacts is the raw finance ledger view for one branch and period.
type RevenueFacts struct {
	Booked          []Amount // bookings created in the period (confirmed and later)
	BookedCollected []Amount // money collected on those bookings
	Margin          []Amount // total minus tax, fees and cost, for bookings with a cost entered (cost_amt > 0)
	CostedBooked    []Amount // total of those costed bookings (the margin base)
	Collected       []Amount // verified charges and adjustments received in the period
	Refunds         []Amount // approved refunds received in the period (positive)
	Pending         []Amount // charges still waiting for verification
	Outstanding     []Amount // open balances on active bookings
	Overdue         []Amount // instalments past their due date
	DueSoon         []Amount // instalments due within DueSoonWindow
	Methods         []MethodAmounts
	Daily           []DayAmounts
}

// RevenueQuery bounds one facts read.
type RevenueQuery struct {
	BranchID uuid.UUID
	Currency string
	From, To time.Time
	Now      time.Time
}

// RevenueReader reads the finance ledger (ISP: only what the revenue card needs).
type RevenueReader interface {
	RevenueFacts(ctx context.Context, q RevenueQuery) (*RevenueFacts, error)
}

// ReportingCurrencyReader returns the branch's finance reporting currency.
type ReportingCurrencyReader interface {
	GetFinanceSettings(ctx context.Context, branchID uuid.UUID) (string, error)
}

// MoneyStat is a converted sum in the reporting currency.
type MoneyStat struct {
	Amount int64 `json:"amount"`
	Count  int   `json:"count"`
}

type MethodStat struct {
	Method string `json:"method"`
	Amount int64  `json:"amount"`
	Count  int    `json:"count"`
}

type SeriesPoint struct {
	Date   string `json:"date"`
	Amount int64  `json:"amount"`
}

// Revenue is the finance-backed revenue card, all amounts in Currency minor units.
type Revenue struct {
	Currency            string        `json:"currency"`
	PeriodFrom          time.Time     `json:"period_from"`
	PeriodTo            time.Time     `json:"period_to"`
	Booked              MoneyStat     `json:"booked"`
	Collected           MoneyStat     `json:"collected"`
	Refunds             MoneyStat     `json:"refunds"`
	NetCollected        int64         `json:"net_collected"`
	Margin              *int64        `json:"margin"` // nil until some booking has a cost
	MarginPct           *float64      `json:"margin_pct"`
	CostedBookings      int           `json:"costed_bookings"`
	CollectionPct       *float64      `json:"collection_pct"`
	Outstanding         MoneyStat     `json:"outstanding"`
	Overdue             MoneyStat     `json:"overdue"`
	DueSoon             MoneyStat     `json:"due_soon"`
	PendingVerification MoneyStat     `json:"pending_verification"`
	Methods             []MethodStat  `json:"methods"`
	Series              []SeriesPoint `json:"series"`
	Unconverted         []string      `json:"unconverted"`
}

// SetRevenueSources wires the finance ledger reader, reporting currency and FX converter.
func (s *Service) SetRevenueSources(r RevenueReader, cur ReportingCurrencyReader, fx fxdomain.Converter) {
	s.revenue, s.reportingCur, s.fx = r, cur, fx
}

// Revenue summarises the branch's finance ledger for the period in its reporting currency.
func (s *Service) Revenue(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*Revenue, error) {
	if s.revenue == nil || s.reportingCur == nil || s.fx == nil {
		return nil, shared.NewValidation("revenue sources not wired")
	}
	now := s.now()
	from, to, err := NormalizePeriod(from, to, now)
	if err != nil {
		return nil, err
	}
	if branchID, err = resolveBranch(ctx, branchID); err != nil {
		return nil, err
	}
	if branchID == nil {
		return nil, shared.NewValidation("branch_id is required: each branch reports in its own currency")
	}
	currency, err := s.reportingCur.GetFinanceSettings(ctx, *branchID)
	if err != nil {
		return nil, err
	}
	facts, err := s.revenue.RevenueFacts(ctx, RevenueQuery{BranchID: *branchID, Currency: currency, From: from, To: to, Now: now})
	if err != nil {
		return nil, err
	}
	c := &converter{ctx: ctx, fx: s.fx, to: currency, missing: map[string]bool{}}
	out := &Revenue{Currency: currency, PeriodFrom: from, PeriodTo: to}
	stat := func(rows []Amount, on time.Time) MoneyStat {
		if err != nil {
			return MoneyStat{}
		}
		var st MoneyStat
		st, err = c.sum(rows, on)
		return st
	}
	out.Booked = stat(facts.Booked, to)
	bookedCollected := stat(facts.BookedCollected, to)
	margin := stat(facts.Margin, to)
	costed := stat(facts.CostedBooked, to)
	out.Collected = stat(facts.Collected, to)
	out.Refunds = stat(facts.Refunds, to)
	out.PendingVerification = stat(facts.Pending, now)
	out.Outstanding = stat(facts.Outstanding, now)
	out.Overdue = stat(facts.Overdue, now)
	out.DueSoon = stat(facts.DueSoon, now)
	if err != nil {
		return nil, err
	}
	out.NetCollected = out.Collected.Amount - out.Refunds.Amount
	if costed.Count > 0 {
		out.Margin = &margin.Amount
		out.MarginPct = percent(margin.Amount, costed.Amount)
		out.CostedBookings = costed.Count
	}
	out.CollectionPct = percent(bookedCollected.Amount, out.Booked.Amount)

	if out.Methods, err = c.methods(facts.Methods, to); err != nil {
		return nil, err
	}
	if out.Series, err = c.series(facts.Daily, from, to); err != nil {
		return nil, err
	}
	out.Unconverted = c.missingCurrencies()
	return out, nil
}

// converter folds per-currency rows into the reporting currency, remembering
// currencies that have no effective rate instead of failing the whole card.
type converter struct {
	ctx     context.Context
	fx      fxdomain.Converter
	to      string
	missing map[string]bool
}

func (c *converter) sum(rows []Amount, on time.Time) (MoneyStat, error) {
	var st MoneyStat
	for _, r := range rows {
		st.Count += r.Count
		if r.Minor == 0 {
			continue
		}
		if r.Currency == c.to {
			st.Amount += r.Minor
			continue
		}
		conv, err := c.fx.Convert(c.ctx, r.Minor, r.Currency, c.to, on)
		if err != nil {
			if errors.Is(err, fxdomain.ErrRateNotFound) {
				c.missing[r.Currency] = true
				continue
			}
			return MoneyStat{}, err
		}
		st.Amount += conv.Amount
	}
	return st, nil
}

func (c *converter) methods(in []MethodAmounts, on time.Time) ([]MethodStat, error) {
	out := make([]MethodStat, 0, len(in))
	for _, m := range in {
		st, err := c.sum(m.Amounts, on)
		if err != nil {
			return nil, err
		}
		if st.Amount == 0 && st.Count == 0 {
			continue
		}
		out = append(out, MethodStat{Method: m.Method, Amount: st.Amount, Count: st.Count})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Amount > out[j].Amount })
	return out, nil
}

// series returns one point per UTC day in [from, to], zero-filled.
func (c *converter) series(in []DayAmounts, from, to time.Time) ([]SeriesPoint, error) {
	byDay := make(map[string]int64, len(in))
	for _, d := range in {
		st, err := c.sum(d.Amounts, d.Day)
		if err != nil {
			return nil, err
		}
		byDay[d.Day.UTC().Format(time.DateOnly)] += st.Amount
	}
	var out []SeriesPoint
	for day := truncateDay(from); !day.After(truncateDay(to)); day = day.AddDate(0, 0, 1) {
		key := day.Format(time.DateOnly)
		out = append(out, SeriesPoint{Date: key, Amount: byDay[key]})
	}
	return out, nil
}

func (c *converter) missingCurrencies() []string {
	out := make([]string, 0, len(c.missing))
	for cur := range c.missing {
		out = append(out, cur)
	}
	sort.Strings(out)
	return out
}

func percent(part, whole int64) *float64 {
	if whole <= 0 {
		return nil
	}
	v := float64(part*1000/whole) / 10
	return &v
}

func truncateDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
