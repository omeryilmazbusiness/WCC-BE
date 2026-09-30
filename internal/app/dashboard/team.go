package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// TeamMember is per-owner performance for the manager board — T-086.
type TeamMember struct {
	OwnerID      uuid.UUID `json:"owner_id"`
	OwnerName    string    `json:"owner_name"`
	Role         string    `json:"role"`
	LeadsHandled int       `json:"leads_handled"`
	LeadsWon     int       `json:"leads_won"`
	OpenTasks    int       `json:"open_tasks"`
	OverdueTasks int       `json:"overdue_tasks"`
	// CollectedAmt is money collected on the member's bookings created in the
	// period, in minor units of Currency. Currencies that cannot be converted
	// are left out and listed in Unconverted.
	CollectedAmt int64    `json:"collected_amt"`
	Currency     string   `json:"currency"`
	Unconverted  []string `json:"unconverted"`
	// Collected is the raw per-currency sum read by the aggregator.
	Collected []Amount `json:"-"`
}

func (s *Service) Team(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]TeamMember, error) {
	from, to, err := NormalizePeriod(from, to, s.now())
	if err != nil {
		return nil, err
	}
	if branchID, err = resolveBranch(ctx, branchID); err != nil {
		return nil, err
	}
	members, err := s.agg.TeamPerformance(ctx, branchID, from, to)
	if err != nil {
		return nil, err
	}
	currency, err := s.teamCurrency(ctx, branchID)
	if err != nil {
		return nil, err
	}
	for i := range members {
		if err := s.convertCollected(ctx, &members[i], currency, to); err != nil {
			return nil, err
		}
	}
	return members, nil
}

// teamCurrency is the branch reporting currency, or "" across all branches or
// when finance settings are not wired.
func (s *Service) teamCurrency(ctx context.Context, branchID *uuid.UUID) (string, error) {
	if branchID == nil || s.reportingCur == nil {
		return "", nil
	}
	return s.reportingCur.GetFinanceSettings(ctx, *branchID)
}

// convertCollected sums m.Collected into one currency. Without a reporting
// currency or FX converter only a single-currency sum is meaningful.
func (s *Service) convertCollected(ctx context.Context, m *TeamMember, currency string, on time.Time) error {
	m.CollectedAmt = 0
	m.Unconverted = []string{}
	if currency != "" && s.fx != nil {
		c := &converter{ctx: ctx, fx: s.fx, to: currency, missing: map[string]bool{}}
		st, err := c.sum(m.Collected, on)
		if err != nil {
			return err
		}
		m.Currency, m.CollectedAmt, m.Unconverted = currency, st.Amount, c.missingCurrencies()
		return nil
	}
	if currency == "" {
		currency = dominantCurrency(m.Collected)
	}
	m.Currency = currency
	seen := map[string]bool{}
	for _, r := range m.Collected {
		switch {
		case r.Minor == 0:
		case r.Currency == currency:
			m.CollectedAmt += r.Minor
		case !seen[r.Currency]:
			seen[r.Currency] = true
			m.Unconverted = append(m.Unconverted, r.Currency)
		}
	}
	return nil
}

// dominantCurrency is the currency holding the largest non-zero sum.
func dominantCurrency(rows []Amount) string {
	best, cur := int64(0), ""
	for _, r := range rows {
		if r.Minor > best {
			best, cur = r.Minor, r.Currency
		}
	}
	return cur
}
