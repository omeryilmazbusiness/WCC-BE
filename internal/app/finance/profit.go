package finance

import (
	"context"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// MaxProfitWindow bounds a profitability query.
const MaxProfitWindow = 366 * 24 * time.Hour

// Profitability is the P&L screen for a booking-date window.
type Profitability struct {
	From, To      time.Time
	CommissionBPS int
	Bookings      []BookingPnLRow
	Departures    []DeparturePnLRow
	Reps          []domain.RepEarnings
}

// ProfitService reports profit and pays out sales commission.
type ProfitService struct {
	Base
	reader   ProfitReader
	budgets  BudgetStore
	settings SettingsStore
	tx       tx.Runner
}

func NewProfitService(b Base, reader ProfitReader, budgets BudgetStore, settings SettingsStore, txm tx.Runner) *ProfitService {
	return &ProfitService{Base: b, reader: reader, budgets: budgets, settings: settings, tx: txm}
}

// Window resolves from/to (YYYY-MM-DD, to inclusive), defaulting to the
// current month.
func (s *ProfitService) Window(from, to string) (time.Time, time.Time, error) {
	start, end := domain.MonthBounds(s.today())
	var err error
	if from != "" {
		if start, err = domain.ParseDay(from); err != nil {
			return start, end, shared.NewValidation("from must be YYYY-MM-DD")
		}
	}
	if to != "" {
		d, err := domain.ParseDay(to)
		if err != nil {
			return start, end, shared.NewValidation("to must be YYYY-MM-DD")
		}
		end = d.AddDate(0, 0, 1)
	}
	if !end.After(start) || end.Sub(start) > MaxProfitWindow {
		return start, end, shared.NewValidation("the window must be 1-366 days")
	}
	return start, end, nil
}

func (s *ProfitService) Profitability(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*Profitability, error) {
	set, err := s.settings.FinanceSettings(ctx, branchID)
	if err != nil {
		return nil, err
	}
	f := ProfitFilter{BranchID: branchID, From: from, To: to, Limit: 200}
	p := &Profitability{From: from, To: to, CommissionBPS: set.CommissionBPS}
	if p.Bookings, err = s.reader.BookingPnL(ctx, f); err != nil {
		return nil, err
	}
	if p.Departures, err = s.reader.DeparturePnL(ctx, f); err != nil {
		return nil, err
	}
	reps, err := s.reader.RepPnL(ctx, f)
	if err != nil {
		return nil, err
	}
	p.Reps = make([]domain.RepEarnings, 0, len(reps))
	for _, r := range reps {
		e := domain.RepEarnings{UserID: r.UserID, Name: r.Name, Currency: r.Currency, Bookings: r.Bookings, Uncosted: r.Uncosted, PnL: r.PnL}
		e.Settle(r.Margins, set.CommissionBPS)
		p.Reps = append(p.Reps, e)
	}
	return p, nil
}

// BudgetInput sets a departure's plan.
type BudgetInput struct {
	Currency string `json:"currency"`
	Revenue  int64  `json:"revenue"`
	Cost     int64  `json:"cost"`
	Note     string `json:"note"`
}

func (s *ProfitService) SetBudget(ctx context.Context, departureID, actor uuid.UUID, in BudgetInput) (*domain.Budget, error) {
	b := domain.Budget{DepartureID: departureID, Currency: in.Currency, Revenue: in.Revenue, Cost: in.Cost, Note: in.Note}
	if err := b.Normalize(); err != nil {
		return nil, err
	}
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		branchID, err := s.budgets.DepartureBranch(ctx, departureID)
		if err != nil {
			return err
		}
		if err := s.budgets.UpsertBudget(ctx, branchID, b, actor); err != nil {
			return err
		}
		return s.record(ctx, actor, "finance.budget_set", "departure", departureID, branchID, b)
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// RatesInput updates the sales commission rate of a branch.
type RatesInput struct {
	CommissionBPS int `json:"commission_bps"`
}

func (s *ProfitService) Settings(ctx context.Context, branchID *uuid.UUID) (Settings, error) {
	return s.settings.FinanceSettings(ctx, branchID)
}

func (s *ProfitService) SetRates(ctx context.Context, branchID, actor uuid.UUID, in RatesInput) (Settings, error) {
	if in.CommissionBPS < 0 || in.CommissionBPS > domain.MaxCommissionRateBPS {
		return Settings{}, shared.NewValidation("commission_bps must be 0-5000")
	}
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.settings.SaveFinanceRates(ctx, branchID, in.CommissionBPS); err != nil {
			return err
		}
		return s.record(ctx, actor, "finance.rates_set", "branch", branchID, branchID, in)
	})
	if err != nil {
		return Settings{}, err
	}
	return s.settings.FinanceSettings(ctx, &branchID)
}
