package supplier

import (
	"slices"
	"sort"
	"time"
)

// CategoriesFor lists the supplier categories able to serve a product.
func CategoriesFor(product string) []string {
	switch product {
	case ProductFlight:
		return []string{CategoryGDS}
	case ProductHotel:
		return []string{CategoryWholesaler, CategoryDMC}
	case ProductTransfer:
		return []string{CategoryTransfer, CategoryDMC}
	case ProductVisa:
		return []string{CategoryVisa, CategoryDMC}
	case ProductInsurance:
		return []string{CategoryInsurance}
	case ProductPackage:
		return []string{CategoryDMC, CategoryWholesaler}
	default:
		return nil
	}
}

// RouteOption is one candidate of a routing decision.
type RouteOption struct {
	Supplier     Supplier
	Availability Availability
	Score        int
	MarkupBps    int
}

// Rank orders the suppliers able to serve product for a search: bookable
// first, then by score (connection health, funding headroom, latency and
// contract runway), then by code for a stable order.
func Rank(suppliers []Supplier, product string, today time.Time) []RouteOption {
	cats := CategoriesFor(product)
	out := make([]RouteOption, 0, len(suppliers))
	for _, s := range suppliers {
		if !slices.Contains(cats, s.Category) {
			continue
		}
		out = append(out, RouteOption{
			Supplier: s, Availability: s.AvailabilityOn(today), Score: routeScore(&s, today), MarkupBps: s.Markups[product],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Availability.Bookable != b.Availability.Bookable {
			return a.Availability.Bookable
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Supplier.Code < b.Supplier.Code
	})
	return out
}

// routeScore is 0-100: health 40, funding 30, latency 20, contract 10.
func routeScore(s *Supplier, today time.Time) int {
	score := 0
	switch s.Health.Status {
	case HealthActive:
		score += 40
	case HealthUnknown:
		score += 20
	case HealthDegraded:
		score += 10
	}
	avail, limited := s.Finance.Available()
	switch {
	case !limited:
		score += 30
	case avail > 0 && s.Finance.LowBalanceThreshold > 0:
		score += int(min(30, 30*avail/(4*s.Finance.LowBalanceThreshold)))
	case avail > 0:
		score += 20
	}
	if s.Health.LatencyMs > 0 {
		switch {
		case s.Health.LatencyMs <= 400:
			score += 20
		case s.Health.LatencyMs <= 1000:
			score += 12
		case s.Health.LatencyMs <= int(SlowLatency/time.Millisecond):
			score += 6
		}
	} else if s.Integration.Type != IntegrationAPI {
		score += 10
	}
	days, hasEnd := s.ContractDaysLeft(today)
	switch {
	case !hasEnd || days > ContractWarnDays:
		score += 10
	case days >= 0:
		score += 4
	}
	return min(score, 100)
}
