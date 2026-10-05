package finance

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
)

// TrendMonths is how many months the revenue trend covers.
const TrendMonths = 6

// Converted is a per-currency figure plus its reporting-currency total.
type Converted struct {
	Items []Money `json:"items"`
	Total int64   `json:"total"`
	// Partial is true when some currency had no FX rate and is missing
	// from Total.
	Partial bool `json:"partial"`
}

// MonthFigure is one month of the revenue trend in the reporting currency.
type MonthFigure struct {
	Month    time.Time
	Revenue  int64
	Margin   int64
	Bookings int
	Partial  bool
}

// Overview is the executive financial dashboard.
type Overview struct {
	ReportingCurrency string
	Cash              Converted
	Receivables       Converted
	Payables          Converted
	Deposits          Converted
	NetPosition       int64
	Month             MonthFigure
	MonthMarginBPS    int
	Trend             []MonthFigure
	Exposure          []domain.CurrencyShare
	Alerts            AlertCounts
	AsOf              time.Time
}

// OverviewService builds the dashboard.
type OverviewService struct {
	Base
	reader   OverviewReader
	settings SettingsStore
	fx       Converter
}

func NewOverviewService(b Base, reader OverviewReader, settings SettingsStore, fx Converter) *OverviewService {
	return &OverviewService{Base: b, reader: reader, settings: settings, fx: fx}
}

func (s *OverviewService) Overview(ctx context.Context, branchID *uuid.UUID) (*Overview, error) {
	set, err := s.settings.FinanceSettings(ctx, branchID)
	if err != nil {
		return nil, err
	}
	today := s.today()
	cur := set.ReportingCurrency
	o := &Overview{ReportingCurrency: cur, AsOf: s.now()}

	cash, err := s.reader.CashByCurrency(ctx, branchID)
	if err != nil {
		return nil, err
	}
	o.Cash = s.convertAll(ctx, cash, cur, today)
	if o.Receivables, err = s.load(ctx, s.reader.ReceivablesByCurrency, branchID, cur, today); err != nil {
		return nil, err
	}
	if o.Payables, err = s.load(ctx, s.reader.PayablesByCurrency, branchID, cur, today); err != nil {
		return nil, err
	}
	if o.Deposits, err = s.load(ctx, s.reader.DepositsByCurrency, branchID, cur, today); err != nil {
		return nil, err
	}
	o.NetPosition = o.Receivables.Total - o.Payables.Total

	monthStart, _ := domain.MonthBounds(today)
	from := monthStart.AddDate(0, -(TrendMonths - 1), 0)
	rows, err := s.reader.MonthlyPnL(ctx, branchID, from, monthStart.AddDate(0, 1, 0))
	if err != nil {
		return nil, err
	}
	o.Trend = s.trend(ctx, rows, from, cur, today)
	o.Month = o.Trend[len(o.Trend)-1]
	o.MonthMarginBPS = domain.RatioBPS(o.Month.Margin, o.Month.Revenue)

	o.Exposure = s.exposure(ctx, cash, cur, today)
	if o.Alerts, err = s.reader.AlertCounts(ctx, branchID, today); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *OverviewService) load(ctx context.Context, fn func(context.Context, *uuid.UUID) ([]Money, error), branchID *uuid.UUID, cur string, on time.Time) (Converted, error) {
	items, err := fn(ctx, branchID)
	if err != nil {
		return Converted{}, err
	}
	return s.convertAll(ctx, items, cur, on), nil
}

// convert returns amount in the reporting currency; ok is false without a rate.
func (s *OverviewService) convert(ctx context.Context, amount int64, from, to string, on time.Time) (int64, bool) {
	if from == to || amount == 0 {
		return amount, true
	}
	if s.fx == nil {
		return 0, false
	}
	c, err := s.fx.Convert(ctx, amount, from, to, on)
	if err != nil {
		return 0, false
	}
	return c.Amount, true
}

func (s *OverviewService) convertAll(ctx context.Context, items []Money, cur string, on time.Time) Converted {
	out := Converted{Items: items}
	if out.Items == nil {
		out.Items = []Money{}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].Currency < out.Items[j].Currency })
	for _, m := range out.Items {
		v, ok := s.convert(ctx, m.Amount, m.Currency, cur, on)
		if !ok {
			out.Partial = true
			continue
		}
		out.Total += v
	}
	return out
}

func (s *OverviewService) trend(ctx context.Context, rows []PeriodPnL, from time.Time, cur string, on time.Time) []MonthFigure {
	out := make([]MonthFigure, TrendMonths)
	index := map[time.Time]int{}
	for i := range out {
		m := from.AddDate(0, i, 0)
		out[i].Month = m
		index[m] = i
	}
	for _, r := range rows {
		i, ok := index[time.Date(r.Month.Year(), r.Month.Month(), 1, 0, 0, 0, 0, time.UTC)]
		if !ok {
			continue
		}
		out[i].Bookings += r.Bookings
		rev, ok1 := s.convert(ctx, r.Revenue, r.Currency, cur, on)
		mar, ok2 := s.convert(ctx, r.PnL.Margin(), r.Currency, cur, on)
		if !ok1 || !ok2 {
			out[i].Partial = true
			continue
		}
		out[i].Revenue += rev
		out[i].Margin += mar
	}
	return out
}

func (s *OverviewService) exposure(ctx context.Context, cash []Money, cur string, on time.Time) []domain.CurrencyShare {
	in := make([]domain.CurrencyShare, 0, len(cash))
	for _, m := range cash {
		v, ok := s.convert(ctx, m.Amount, m.Currency, cur, on)
		in = append(in, domain.CurrencyShare{Currency: m.Currency, Amount: m.Amount, Converted: v, Unconverted: !ok})
	}
	return domain.Shares(in)
}
