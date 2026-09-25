package ai

import (
	"encoding/json"
	"strings"
	"time"
)

// PriorityBand is a deterministic urgency band (T-193 / T-196 — score in code).
type PriorityBand string

const (
	BandLow    PriorityBand = "low"
	BandNormal PriorityBand = "normal"
	BandHigh   PriorityBand = "high"
	BandUrgent PriorityBand = "urgent"
)

// Signal is an explainable scoring factor.
type Signal struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Points int    `json:"points"`
}

// LeadSignals is the input for pure scoring (no LLM).
type LeadSignals struct {
	Stage           string
	NoFollowUp      bool
	HoursSinceTouch float64
	HasOpenTask     bool
	OverdueTask     bool
	Source          string
	UnansweredInbox bool
}

// ScoreLead computes priority_score (0–100) and band with explainable signals.
func ScoreLead(in LeadSignals) (score int, band PriorityBand, signals []Signal) {
	add := func(code, label string, pts int) {
		if pts == 0 {
			return
		}
		signals = append(signals, Signal{Code: code, Label: label, Points: pts})
		score += pts
	}

	stage := strings.ToLower(strings.TrimSpace(in.Stage))
	switch stage {
	case "qualified", "proposal":
		add("stage_hot", "Hot pipeline stage", 25)
	case "contacted", "new":
		add("stage_active", "Active pipeline stage", 10)
	case "won", "lost":
		add("stage_closed", "Closed stage", -20)
	}

	if in.NoFollowUp {
		add("no_follow_up", "Marked no follow-up", 30)
	}
	if in.HoursSinceTouch >= 72 {
		add("stale_72h", "No touch in 72h+", 25)
	} else if in.HoursSinceTouch >= 24 {
		add("stale_24h", "No touch in 24h+", 15)
	}
	if in.OverdueTask {
		add("overdue_task", "Has overdue task", 20)
	} else if in.HasOpenTask {
		add("open_task", "Has open task", 5)
	}
	if in.UnansweredInbox {
		add("unanswered_inbox", "Unanswered inbox thread", 20)
	}
	src := strings.ToLower(in.Source)
	if src == "whatsapp" || src == "instagram" || src == "referral" {
		add("warm_source", "Warm inbound source", 10)
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	switch {
	case score >= 70:
		band = BandUrgent
	case score >= 45:
		band = BandHigh
	case score >= 20:
		band = BandNormal
	default:
		band = BandLow
	}
	return score, band, signals
}

// HoursSince returns hours from t to now (0 if nil/zero).
func HoursSince(t *time.Time, now time.Time) float64 {
	if t == nil || t.IsZero() {
		return 999
	}
	return now.Sub(*t).Hours()
}

// SignalsJSON marshals signals for persistence.
func SignalsJSON(signals []Signal) json.RawMessage {
	if len(signals) == 0 {
		return json.RawMessage(`[]`)
	}
	b, _ := json.Marshal(signals)
	return b
}

// DeterministicTargetInsight builds recovery copy without LLM (T-194 / T-196).
func DeterministicTargetInsight(label string, target, actual, expected int64, status string) map[string]any {
	variance := actual - expected
	gap := target - actual
	paceNeeded := int64(0)
	recs := []string{}
	if gap > 0 {
		recs = append(recs, "Focus on unpaid confirmed bookings to lift collected revenue.")
		recs = append(recs, "Prioritize high-intent leads in proposal/qualified stages.")
	}
	if status == "behind" {
		recs = append(recs, "Schedule daily recovery huddle for owners behind pace.")
	}
	if variance < 0 {
		recs = append(recs, "Variance vs expected-to-date is negative — protect conversion this week.")
	}
	return map[string]any{
		"label":            label,
		"status":           status,
		"target_amount":    target,
		"actual_amount":    actual,
		"expected_to_date": expected,
		"variance":         variance,
		"gap_to_target":    gap,
		"required_daily":   paceNeeded,
		"recommendations":  recs,
		"source":           "deterministic",
	}
}
