package revenuetarget

import (
	"fmt"
	"time"
)

// Engine is a pure deterministic calculator (SRP / OCP) — T-131/T-132.
type Engine struct{}

func (Engine) ValidateWeights(curve CurveType, weights []Weight) error {
	if curve != CurveSeasonal {
		return nil
	}
	if len(weights) == 0 {
		return fmt.Errorf("seasonal curve requires weights")
	}
	sum := 0
	seen := map[int]struct{}{}
	for _, w := range weights {
		if w.WeightBps < 0 || w.WeightBps > 10000 {
			return fmt.Errorf("weight_bps out of range")
		}
		if _, ok := seen[w.Bucket]; ok {
			return fmt.Errorf("duplicate bucket %d", w.Bucket)
		}
		seen[w.Bucket] = struct{}{}
		sum += w.WeightBps
	}
	if sum != 10000 {
		return fmt.Errorf("weights must sum to 10000 bps (got %d)", sum)
	}
	return nil
}

func (Engine) ValidateShares(shares []Share) error {
	if len(shares) == 0 {
		return nil
	}
	sum := 0
	seen := map[string]struct{}{}
	for _, s := range shares {
		if s.ShareBps < 0 || s.ShareBps > 10000 {
			return fmt.Errorf("share_bps out of range")
		}
		k := s.UserID.String()
		if _, ok := seen[k]; ok {
			return fmt.Errorf("duplicate share user")
		}
		seen[k] = struct{}{}
		sum += s.ShareBps
	}
	if sum != 10000 {
		return fmt.Errorf("shares must sum to 10000 bps (got %d)", sum)
	}
	return nil
}

type CalcInput struct {
	TargetAmount int64
	Actual       int64
	PeriodStart  time.Time
	PeriodEnd    time.Time
	AsOf         time.Time
	Curve        CurveType
	Weights      []Weight
}

type CalcResult struct {
	ExpectedToDate    int64
	Variance          int64
	ProgressBps       int
	PaceBps           int
	ForecastAmount    int64
	RequiredPaceDaily int64
	Status            Status
}

func (Engine) Calculate(in CalcInput) CalcResult {
	if in.TargetAmount <= 0 {
		return CalcResult{Status: StatusPlaceholder}
	}
	asOf := truncateDate(in.AsOf)
	start := truncateDate(in.PeriodStart)
	end := truncateDate(in.PeriodEnd)
	if asOf.Before(start) {
		asOf = start
	}
	if end.Before(start) {
		return CalcResult{Status: StatusPlaceholder}
	}

	expected := expectedToDate(in.TargetAmount, start, end, asOf, in.Curve, in.Weights)
	variance := in.Actual - expected
	progress := int((in.Actual * 10000) / in.TargetAmount)
	pace := 0
	if expected > 0 {
		pace = int((in.Actual * 10000) / expected)
	} else if in.Actual > 0 {
		pace = 10000
	}

	elapsedDays := daysInclusive(start, minTime(asOf, end))
	totalDays := daysInclusive(start, end)
	forecast := int64(0)
	if elapsedDays > 0 {
		forecast = (in.Actual * int64(totalDays)) / int64(elapsedDays)
	}
	remainingDays := daysInclusive(minTime(asOf.AddDate(0, 0, 1), end), end)
	if asOf.After(end) || asOf.Equal(end) {
		remainingDays = 0
	}
	remainingAmt := in.TargetAmount - in.Actual
	reqDaily := int64(0)
	if remainingDays > 0 && remainingAmt > 0 {
		reqDaily = (remainingAmt + int64(remainingDays) - 1) / int64(remainingDays)
	}

	return CalcResult{
		ExpectedToDate: expected, Variance: variance, ProgressBps: progress,
		PaceBps: pace, ForecastAmount: forecast, RequiredPaceDaily: reqDaily,
		Status: StatusFromPace(expected, in.Actual),
	}
}

// StatusFromPace mirrors Epic 7 thresholds vs expected: ahead ≥105%, on_track ≥90%.
func StatusFromPace(expected, actual int64) Status {
	if expected <= 0 {
		if actual > 0 {
			return StatusAhead
		}
		return StatusOnTrack
	}
	ratio := float64(actual) / float64(expected)
	switch {
	case ratio >= 1.05:
		return StatusAhead
	case ratio >= 0.90:
		return StatusOnTrack
	default:
		return StatusBehind
	}
}

func expectedToDate(target int64, start, end, asOf time.Time, curve CurveType, weights []Weight) int64 {
	if !asOf.After(start) && !asOf.Equal(start) {
		return 0
	}
	totalDays := daysInclusive(start, end)
	if totalDays <= 0 {
		return 0
	}
	elapsed := daysInclusive(start, minTime(asOf, end))
	if curve != CurveSeasonal || len(weights) == 0 {
		return (target * int64(elapsed)) / int64(totalDays)
	}
	// Distribute target across calendar months in period by weight_bps on bucket index.
	months := monthBuckets(start, end)
	if len(months) == 0 {
		return (target * int64(elapsed)) / int64(totalDays)
	}
	wmap := map[int]int{}
	for _, w := range weights {
		wmap[w.Bucket] = w.WeightBps
	}
	// If weights cover fewer buckets than months, fall back to equal for missing.
	var expected int64
	for i, m := range months {
		bps := wmap[i]
		if bps == 0 && len(wmap) > 0 {
			// missing bucket → 0 weight
		}
		monthTarget := (target * int64(bps)) / 10000
		ms, me := m.start, m.end
		if me.After(end) {
			me = end
		}
		if ms.Before(start) {
			ms = start
		}
		mdays := daysInclusive(ms, me)
		if mdays <= 0 {
			continue
		}
		if asOf.Before(ms) {
			continue
		}
		cut := me
		if asOf.Before(me) {
			cut = asOf
		}
		ed := daysInclusive(ms, cut)
		expected += (monthTarget * int64(ed)) / int64(mdays)
	}
	return expected
}

type monthSpan struct{ start, end time.Time }

func monthBuckets(start, end time.Time) []monthSpan {
	var out []monthSpan
	cur := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !cur.After(end) {
		next := cur.AddDate(0, 1, 0)
		ms, me := cur, next.AddDate(0, 0, -1)
		out = append(out, monthSpan{ms, me})
		cur = next
		if len(out) >= 24 {
			break
		}
	}
	return out
}

func daysInclusive(a, b time.Time) int {
	a, b = truncateDate(a), truncateDate(b)
	if b.Before(a) {
		return 0
	}
	return int(b.Sub(a).Hours()/24) + 1
}

func truncateDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// CumulativeSeries builds daily cumulative actual vs expected for charts (T-142).
func (e Engine) CumulativeSeries(targetAmount, actualNow int64, start, end, asOf time.Time, curve CurveType, weights []Weight, dailyActuals map[string]int64) []SeriesPoint {
	start, end, asOf = truncateDate(start), truncateDate(end), truncateDate(asOf)
	if asOf.After(end) {
		asOf = end
	}
	var points []SeriesPoint
	var cumActual int64
	for d := start; !d.After(asOf); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		cumActual += dailyActuals[key]
		// If no daily breakdown, distribute actual linearly for display until asOf.
		displayActual := cumActual
		if len(dailyActuals) == 0 {
			elapsed := daysInclusive(start, d)
			totalElapsed := daysInclusive(start, asOf)
			if totalElapsed > 0 {
				displayActual = (actualNow * int64(elapsed)) / int64(totalElapsed)
			}
		}
		exp := expectedToDate(targetAmount, start, end, d, curve, weights)
		points = append(points, SeriesPoint{
			Date: key, Actual: displayActual, Expected: exp,
		})
	}
	return points
}
