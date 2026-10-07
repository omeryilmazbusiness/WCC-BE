package assistant

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Facts are the server-computed, already scoped numbers a capability may use.
type Facts struct {
	LeadsOpen      int
	TasksOverdue   int
	BookingsUnpaid int
	MissingDocs    int
	Attention      []string
	Target         *TargetFacts
}

// TargetFacts describes the viewer's active revenue target; amounts in minor units.
type TargetFacts struct {
	Label    string
	Status   string // ahead | on_track | behind | placeholder
	Currency string
	Target   int64
	Actual   int64
	Expected int64
}

const (
	maxAttention      = 5
	maxAttentionRunes = 70
)

// Compact returns only what `c` needs, with short keys, for the prompt.
func (f *Facts) Compact(c CapabilityID) map[string]any {
	if f == nil {
		return map[string]any{}
	}
	switch c {
	case CapOpsSummary:
		return map[string]any{
			"open_leads": f.LeadsOpen, "overdue_tasks": f.TasksOverdue,
			"unpaid_bookings": f.BookingsUnpaid, "missing_docs": f.MissingDocs,
			"attention": f.attention(),
		}
	case CapLeadFocus:
		return map[string]any{"open_leads": f.LeadsOpen, "attention": f.attention()}
	case CapRevenueStatus:
		if f.Target == nil {
			return map[string]any{"target": nil}
		}
		t := f.Target
		return map[string]any{"target": map[string]any{
			"label": t.Label, "status": t.Status, "currency": t.Currency,
			"goal": major(t.Target), "actual": major(t.Actual), "expected_by_now": major(t.Expected),
		}}
	default:
		return map[string]any{}
	}
}

// attention is the first distinct titles; repeats ("Follow up lead" ×3) add tokens, not meaning.
func (f *Facts) attention() []string {
	out := make([]string, 0, maxAttention)
	seen := make(map[string]bool, maxAttention)
	for _, a := range f.Attention {
		if len(out) == maxAttention {
			break
		}
		a = truncateRunes(strings.TrimSpace(a), maxAttentionRunes)
		key := strings.ToLower(a)
		if a == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

// Hash identifies the compact facts of `c` for the answer cache.
func (f *Facts) Hash(c CapabilityID) string {
	b, _ := json.Marshal(f.Compact(c))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func major(minor int64) int64 { return minor / 100 }

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
