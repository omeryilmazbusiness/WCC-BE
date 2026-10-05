package supplier

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Health statuses.
const (
	HealthUnknown  = "unknown"
	HealthActive   = "active"
	HealthDegraded = "degraded"
	HealthDown     = "down"
)

// SlowLatency marks a reachable API as degraded.
const SlowLatency = 1500 * time.Millisecond

const MaxHealthNote = 300

// Health is the last known state of the supplier connection.
type Health struct {
	Status    string
	LatencyMs int
	CheckedAt *time.Time
	Note      string
}

// ClassifyProbe turns a probe result into a status: transport errors and 5xx
// are down, slow answers degraded. 4xx counts as reachable, since API roots
// commonly answer 401/404 without credentials.
func ClassifyProbe(latency time.Duration, statusCode int, err error) string {
	switch {
	case err != nil || statusCode == 0 || statusCode >= 500:
		return HealthDown
	case latency > SlowLatency:
		return HealthDegraded
	default:
		return HealthActive
	}
}

// SetManual records an operator-set status (e.g. planned maintenance).
func (h *Health) SetManual(status, note string, at time.Time) error {
	status = strings.ToLower(strings.TrimSpace(status))
	note = strings.TrimSpace(note)
	f := fields{}
	switch status {
	case HealthActive, HealthDegraded, HealthDown, HealthUnknown:
	default:
		f.add("status", "active, degraded, down or unknown")
	}
	if utf8.RuneCountInString(note) > MaxHealthNote {
		f.add("note", "at most 300 characters")
	}
	if err := f.err("invalid health status"); err != nil {
		return err
	}
	h.Status, h.Note, h.CheckedAt = status, note, &at
	return nil
}
