package hotel

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	MarkupPercent = "percent"
	MarkupFixed   = "fixed"

	// MaxMarkupBps caps a percent markup at 500%.
	MaxMarkupBps   = 50_000
	MaxMoneyAmount = 1_000_000_000_00
)

// Markup turns a net rate into the selling (gross) rate. Percent values are
// basis points (1500 = 15%); fixed values are minor units added per paying
// guest per night.
type Markup struct {
	Kind  string `json:"kind"`
	Value int64  `json:"value"`
}

func (m *Markup) Normalize() error {
	m.Kind = strings.ToLower(strings.TrimSpace(m.Kind))
	if m.Kind == "" {
		m.Kind = MarkupPercent
	}
	switch m.Kind {
	case MarkupPercent:
		if m.Value < 0 || m.Value > MaxMarkupBps {
			return errors.New("percent markup must be 0-500%")
		}
	case MarkupFixed:
		if m.Value < 0 || m.Value > MaxMoneyAmount {
			return errors.New("fixed markup out of range")
		}
	default:
		return errors.New("kind must be percent or fixed")
	}
	return nil
}

// Apply prices net for payingGuests guests; zero net stays zero.
func (m Markup) Apply(net int64, payingGuests int) int64 {
	if net <= 0 {
		return net
	}
	if m.Kind == MarkupFixed {
		return net + m.Value*int64(payingGuests)
	}
	return net + roundDiv(net*m.Value, 10_000)
}

// Child pricing modes.
const (
	ChildFree    = "free"
	ChildPercent = "percent"
	ChildFixed   = "fixed"
)

// ChildRule prices one age band: free, a percent of the adult per-person
// rate, or a fixed net amount per night.
type ChildRule struct {
	Mode  string `json:"mode"`
	Value int64  `json:"value"`
}

func (r *ChildRule) normalize(name string, allowPercent bool) error {
	r.Mode = strings.ToLower(strings.TrimSpace(r.Mode))
	if r.Mode == "" {
		r.Mode = ChildFree
	}
	switch r.Mode {
	case ChildFree:
		r.Value = 0
	case ChildPercent:
		if !allowPercent {
			return fmt.Errorf("%s must be free or fixed", name)
		}
		if r.Value < 0 || r.Value > 100 {
			return fmt.Errorf("%s percent must be 0-100", name)
		}
	case ChildFixed:
		if r.Value < 0 || r.Value > MaxMoneyAmount {
			return fmt.Errorf("%s amount out of range", name)
		}
	default:
		return fmt.Errorf("%s mode must be free, percent or fixed", name)
	}
	return nil
}

func (r ChildRule) charge(adultPerPerson int64) int64 {
	switch r.Mode {
	case ChildPercent:
		return roundDiv(adultPerPerson*r.Value, 100)
	case ChildFixed:
		return r.Value
	default:
		return 0
	}
}

// Age bands, as returned by ChildPolicy.Band.
const (
	BandInfant = "infant"
	BandChild1 = "child1"
	BandChild2 = "child2"
	BandAdult  = "adult"
)

// ChildPolicy holds the age breaks (exclusive upper bounds, e.g. infants are
// 0-1.99 when InfantMaxAge is 2) and the extra bed price for adults.
type ChildPolicy struct {
	InfantMaxAge  int       `json:"infant_max_age"`
	Child1MaxAge  int       `json:"child1_max_age"`
	Child2MaxAge  int       `json:"child2_max_age"`
	Infant        ChildRule `json:"infant"`
	Child1        ChildRule `json:"child1"`
	Child2WithBed ChildRule `json:"child2_with_bed"`
	Child2NoBed   ChildRule `json:"child2_no_bed"`
	ExtraBedAdult int64     `json:"extra_bed_adult"`
}

// DefaultChildPolicy is the common Saudi/Turkish contract shape.
func DefaultChildPolicy() ChildPolicy {
	return ChildPolicy{
		InfantMaxAge: 2, Child1MaxAge: 6, Child2MaxAge: 12,
		Infant:        ChildRule{Mode: ChildFree},
		Child1:        ChildRule{Mode: ChildFree},
		Child2WithBed: ChildRule{Mode: ChildPercent, Value: 75},
		Child2NoBed:   ChildRule{Mode: ChildPercent, Value: 50},
	}
}

func (p *ChildPolicy) Normalize() error {
	if p.InfantMaxAge == 0 && p.Child1MaxAge == 0 && p.Child2MaxAge == 0 {
		d := DefaultChildPolicy()
		p.InfantMaxAge, p.Child1MaxAge, p.Child2MaxAge = d.InfantMaxAge, d.Child1MaxAge, d.Child2MaxAge
	}
	if p.InfantMaxAge < 1 || p.InfantMaxAge >= p.Child1MaxAge || p.Child1MaxAge >= p.Child2MaxAge || p.Child2MaxAge > 18 {
		return errors.New("age breaks must rise: infant < child 1 < child 2 <= 18")
	}
	if err := p.Infant.normalize("infant", false); err != nil {
		return err
	}
	if err := p.Child1.normalize("child1", true); err != nil {
		return err
	}
	if err := p.Child2WithBed.normalize("child2_with_bed", true); err != nil {
		return err
	}
	if err := p.Child2NoBed.normalize("child2_no_bed", true); err != nil {
		return err
	}
	if p.ExtraBedAdult < 0 || p.ExtraBedAdult > MaxMoneyAmount {
		return errors.New("extra_bed_adult out of range")
	}
	return nil
}

// Band places an age in the policy's bands.
func (p ChildPolicy) Band(age int) string {
	switch {
	case age < p.InfantMaxAge:
		return BandInfant
	case age < p.Child1MaxAge:
		return BandChild1
	case age < p.Child2MaxAge:
		return BandChild2
	default:
		return BandAdult
	}
}

// ChildCharge is the net nightly price of a child; ok is false for ages
// priced as adults.
func (p ChildPolicy) ChildCharge(age int, withBed bool, adultPerPerson int64) (band string, net int64, ok bool) {
	band = p.Band(age)
	switch band {
	case BandInfant:
		return band, p.Infant.charge(adultPerPerson), true
	case BandChild1:
		return band, p.Child1.charge(adultPerPerson), true
	case BandChild2:
		if withBed {
			return band, p.Child2WithBed.charge(adultPerPerson), true
		}
		return band, p.Child2NoBed.charge(adultPerPerson), true
	default:
		return band, 0, false
	}
}

// Penalty kinds.
const (
	PenaltyNights  = "nights"
	PenaltyPercent = "percent"
)

// PenaltyTier applies when the guest cancels MinDays or more (but fewer than
// the next tier's MinDays) before check-in.
type PenaltyTier struct {
	MinDays int    `json:"min_days"`
	Kind    string `json:"kind"`
	Value   int    `json:"value"`
}

// CancellationPolicy: free until FreeDays before check-in, then the tiers;
// a no-show forfeits NoShowPct of the stay.
type CancellationPolicy struct {
	FreeDays  int           `json:"free_days"`
	Tiers     []PenaltyTier `json:"tiers"`
	NoShowPct int           `json:"no_show_pct"`
}

func DefaultCancellationPolicy() CancellationPolicy {
	return CancellationPolicy{
		FreeDays: 14,
		Tiers: []PenaltyTier{
			{MinDays: 7, Kind: PenaltyNights, Value: 1},
			{MinDays: 0, Kind: PenaltyPercent, Value: 100},
		},
		NoShowPct: 100,
	}
}

func (c *CancellationPolicy) Normalize() error {
	if c.FreeDays < 0 || c.FreeDays > 365 {
		return errors.New("free_days must be 0-365")
	}
	if c.NoShowPct < 0 || c.NoShowPct > 100 {
		return errors.New("no_show_pct must be 0-100")
	}
	seen := map[int]bool{}
	for i := range c.Tiers {
		t := &c.Tiers[i]
		t.Kind = strings.ToLower(strings.TrimSpace(t.Kind))
		if t.MinDays < 0 || t.MinDays >= c.FreeDays {
			return errors.New("tier min_days must be below free_days")
		}
		if seen[t.MinDays] {
			return errors.New("tier min_days must be unique")
		}
		seen[t.MinDays] = true
		switch t.Kind {
		case PenaltyNights:
			if t.Value < 1 || t.Value > 60 {
				return errors.New("nights penalty must be 1-60")
			}
		case PenaltyPercent:
			if t.Value < 1 || t.Value > 100 {
				return errors.New("percent penalty must be 1-100")
			}
		default:
			return errors.New("tier kind must be nights or percent")
		}
	}
	slices.SortFunc(c.Tiers, func(a, b PenaltyTier) int { return b.MinDays - a.MinDays })
	return nil
}

// TierFor returns the tier applying daysBefore check-in; nil means free.
// Inside the penalty window with no matching tier the whole stay is due.
func (c CancellationPolicy) TierFor(daysBefore int) *PenaltyTier {
	if daysBefore >= c.FreeDays {
		return nil
	}
	for i := range c.Tiers {
		if daysBefore >= c.Tiers[i].MinDays {
			t := c.Tiers[i]
			return &t
		}
	}
	return &PenaltyTier{MinDays: 0, Kind: PenaltyPercent, Value: 100}
}

// Penalty prices a cancellation daysBefore check-in for a stay whose nightly
// amounts are given in stay order.
func (c CancellationPolicy) Penalty(daysBefore int, nightly []int64) int64 {
	t := c.TierFor(daysBefore)
	if t == nil {
		return 0
	}
	return t.charge(nightly)
}

// NoShowPenalty is what a no-show forfeits.
func (c CancellationPolicy) NoShowPenalty(nightly []int64) int64 {
	return roundDiv(sum(nightly)*int64(c.NoShowPct), 100)
}

// FreeUntil is the last day a booking checking in on checkIn cancels free.
func (c CancellationPolicy) FreeUntil(checkIn time.Time) time.Time {
	return checkIn.AddDate(0, 0, -c.FreeDays)
}

func (t PenaltyTier) charge(nightly []int64) int64 {
	if t.Kind == PenaltyNights {
		n := min(t.Value, len(nightly))
		return sum(nightly[:n])
	}
	return roundDiv(sum(nightly)*int64(t.Value), 100)
}

func sum(xs []int64) int64 {
	var s int64
	for _, x := range xs {
		s += x
	}
	return s
}

// roundDiv divides rounding half away from zero (amounts are non-negative).
func roundDiv(a, b int64) int64 {
	return (a + b/2) / b
}
