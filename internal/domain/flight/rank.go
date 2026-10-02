package flight

import (
	"sort"
	"time"
)

// Ranked is a fare placed relative to the wanted departure.
type Ranked struct {
	Fare
	// GapMinutes is the signed distance from the wanted time (negative = earlier).
	GapMinutes int
	Cheapest   bool
	Closest    bool
}

// Rank keeps unique fares within SearchWindow of the wanted time, ordered by
// closeness and then price, and marks the cheapest and the closest one. With
// anyTime closeness is measured in whole days, so the wanted day comes first.
func Rank(fares []Fare, wanted time.Time, anyTime bool, limit int) []Ranked {
	seen := make(map[string]struct{}, len(fares))
	out := make([]Ranked, 0, len(fares))
	for _, f := range fares {
		if _, dup := seen[f.Key()]; dup {
			continue
		}
		seen[f.Key()] = struct{}{}
		gap := f.LocalDeparture().Sub(wanted)
		if anyTime {
			gap = dayOf(f.LocalDeparture()).Sub(dayOf(wanted))
		}
		if gap < -SearchWindow || gap > SearchWindow || f.Price <= 0 {
			continue
		}
		out = append(out, Ranked{Fare: f, GapMinutes: int(gap / time.Minute)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := abs(out[i].GapMinutes), abs(out[j].GapMinutes)
		if a != b {
			return a < b
		}
		return out[i].Price < out[j].Price
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	if len(out) == 0 {
		return out
	}
	out[0].Closest = true
	cheapest := 0
	for i := range out {
		if out[i].Price < out[cheapest].Price {
			cheapest = i
		}
	}
	out[cheapest].Cheapest = true
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
