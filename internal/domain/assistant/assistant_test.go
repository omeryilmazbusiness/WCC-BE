package assistant

import (
	"strings"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	cases := map[string]CapabilityID{
		"Give me a summary of today's operations":        CapOpsSummary,
		"how many bookings are unpaid?":                  CapOpsSummary,
		"Bugünün özeti nedir":                            CapOpsSummary,
		"أعطني ملخصًا لعمليات اليوم":                     CapOpsSummary,
		"Which leads should I focus on this week?":       CapLeadFocus,
		"ما الطلبات التي يجب أن أركز عليها هذا الأسبوع؟": CapLeadFocus,
		"How is revenue tracking against our targets?":   CapRevenueStatus,
		"hedefe göre satışlar nasıl":                     CapRevenueStatus,
		"كيف تسير الإيرادات مقارنة بأهدافنا؟":            CapRevenueStatus,
		"Draft a follow-up message for a customer":       CapMessageDraft,
		"write a whatsapp reply to Ahmed about visas":    CapMessageDraft,
		"اكتب مسودة رسالة إلى عميل":                      CapMessageDraft,
		"How do I create a booking?":                     CapAppHelp,
		"where is the import screen":                     CapAppHelp,
		"Kullanıcı nasıl eklenir, ayarlar nerede?":       CapAppHelp,
		"hi":                             CapGreeting,
		"Thanks!":                        CapGreeting,
		"مرحبا":                          CapGreeting,
		"What is the capital of France?": CapOutOfScope,
		"tell me a joke":                 CapOutOfScope,
		"":                               CapOutOfScope,
	}
	for q, want := range cases {
		if got := Classify(q); got != want {
			t.Errorf("Classify(%q) = %s, want %s", q, got, want)
		}
	}
}

func TestClassifyWordBoundaries(t *testing.T) {
	// "hi" inside "this" / "which" must not read as a greeting or anything else.
	if got := Classify("this is it"); got != CapOutOfScope {
		t.Fatalf("got %s", got)
	}
	if got := Classify("hi, give me today's summary please"); got != CapOpsSummary {
		t.Fatalf("a greeting with a real question routes to the question, got %s", got)
	}
	// "missing" must not be matched by "miss".
	if got := Classify("I miss you"); got != CapOutOfScope {
		t.Fatalf("got %s", got)
	}
}

func TestCatalog(t *testing.T) {
	seen := map[CapabilityID]bool{}
	for _, c := range Capabilities() {
		if seen[c.ID] {
			t.Fatalf("duplicate %s", c.ID)
		}
		seen[c.ID] = true
		if c.Kind == KindRule && c.UsesModel() {
			t.Errorf("%s: rule capabilities never use the model", c.ID)
		}
		if c.UsesModel() && (c.Task == "" || c.MaxOutputTokens > 400) {
			t.Errorf("%s: model capabilities need a task and a small output cap", c.ID)
		}
		if c.ID != CapOutOfScope && (len(c.Requires) == 0 || c.Requires[0] != PermAIRead) {
			t.Errorf("%s: every capability starts with ai.read", c.ID)
		}
	}
	for _, s := range signals {
		if _, ok := Lookup(s.id); !ok {
			t.Errorf("router targets unknown capability %s", s.id)
		}
	}
	if len(Rules()) != 10 {
		t.Fatalf("rules = %d", len(Rules()))
	}
	draft, _ := Lookup(CapMessageDraft)
	if draft.Allowed(func(p string) bool { return p == PermAIRead }) {
		t.Fatal("drafting needs ai.write")
	}
	cats := Capabilities()
	cats[0].ID = "mutated"
	if _, ok := Lookup(CapOpsSummary); !ok {
		t.Fatal("Capabilities returns a copy")
	}
}

func TestQuota(t *testing.T) {
	q := Quota{Limit: 2, Used: 1}
	if q.Exhausted() || q.Remaining() != 1 || q.Mode() != ModeFull {
		t.Fatalf("%+v", q)
	}
	q.Used = 2
	if !q.Exhausted() || q.Remaining() != 0 || q.Mode() != ModeEssential {
		t.Fatalf("%+v", q)
	}
	q.Used = 9
	if q.Remaining() != 0 {
		t.Fatal("remaining never negative")
	}
	if (Quota{Limit: 0, Used: 999}).Exhausted() || (Quota{}).Remaining() != -1 {
		t.Fatal("non-positive limit is unlimited")
	}
	ist := time.FixedZone("IST", 3*3600)
	now := time.Date(2026, 10, 7, 22, 30, 0, 0, time.UTC) // 01:30 next day in +3
	if d := Day(now, ist); d.Day() != 8 || d.Hour() != 0 {
		t.Fatalf("day bucket follows the company zone: %v", d)
	}
	if r := NextReset(now, ist); !r.Equal(time.Date(2026, 10, 9, 0, 0, 0, 0, ist)) {
		t.Fatalf("reset %v", r)
	}
}

func TestPromptIsSmall(t *testing.T) {
	facts := &Facts{LeadsOpen: 4, TasksOverdue: 6, Attention: []string{"a", "b", "c", "d", "e", "f", "g"}}
	c, _ := Lookup(CapOpsSummary)
	p := BuildPrompt(c, "ar", facts, nil, "  ملخص اليوم  ")
	if !strings.Contains(p.System, "Arabic") || !strings.Contains(p.System, c.Task) {
		t.Fatal("system names language and task")
	}
	if !strings.HasSuffix(p.User, "Q: ملخص اليوم") || !strings.Contains(p.User, `"open_leads":4`) {
		t.Fatalf("user: %s", p.User)
	}
	if strings.Count(p.User, `"g"`) != 0 {
		t.Fatal("attention is capped at five items")
	}
	if EstimateTokens(p.System, p.User) > 220 {
		t.Fatalf("prompt too large: %d tokens", EstimateTokens(p.System, p.User))
	}
	if p.MaxTokens != c.MaxOutputTokens {
		t.Fatal("output cap comes from the capability")
	}

	draft, _ := Lookup(CapMessageDraft)
	if strings.Contains(BuildPrompt(draft, "en", facts, nil, "write").User, "Facts:") {
		t.Fatal("drafts never receive facts")
	}
}

func TestHistoryTrim(t *testing.T) {
	var h []Turn
	for i := 0; i < 10; i++ {
		h = append(h, Turn{Role: "user", Content: strings.Repeat("x", 500) + string(rune('a'+i))})
	}
	got := trimHistory(h, HistoryTurns)
	if len(got) > HistoryTurns {
		t.Fatalf("turns %d", len(got))
	}
	total := 0
	for _, t := range got {
		total += len([]rune(t.Content))
	}
	if total > HistoryChars {
		t.Fatalf("history chars %d", total)
	}
	if !strings.HasPrefix(got[len(got)-1].Content, "xxx") {
		t.Fatal("newest turn kept last")
	}
	multi := trimHistory([]Turn{{Role: "assistant", Content: "### Today\n- **Open leads:** 4\n\n  line2  "}, {Role: "user", Content: "   "}}, 4)
	if len(multi) != 1 || multi[0].Content != "Today Open leads: 4 line2" {
		t.Fatalf("collapse, strip markdown, drop empty: %+v", multi)
	}
	if trimHistory(h, 0) != nil || len(trimHistory(h, 99)) > HistoryTurns {
		t.Fatal("turns are clamped to 0..HistoryTurns")
	}
}

func TestHistoryPerCapability(t *testing.T) {
	h := []Turn{{Role: "user", Content: "earlier question"}, {Role: "assistant", Content: "earlier answer"}}
	for _, c := range Capabilities() {
		if c.History > HistoryTurns {
			t.Fatalf("%s history above the cap", c.ID)
		}
		has := strings.Contains(BuildPrompt(c, "en", &Facts{}, h, "q").User, "History:")
		if c.Kind == KindData && has {
			t.Fatalf("%s: data answers send no history (facts are the context, answers stay cacheable)", c.ID)
		}
		if c.UsesModel() && c.Kind != KindData && !has {
			t.Fatalf("%s: follow-ups keep context", c.ID)
		}
	}
}

func TestAttentionDeduplicated(t *testing.T) {
	f := &Facts{Attention: []string{"Follow up lead", "follow up lead ", "Follow up lead", "Confirm hotel", "", "a", "b", "c", "d"}}
	got := f.Compact(CapLeadFocus)["attention"].([]string)
	if len(got) != 5 || got[0] != "Follow up lead" || got[1] != "Confirm hotel" {
		t.Fatalf("distinct titles only, capped: %v", got)
	}
}

func TestCompactFactsAreMinimal(t *testing.T) {
	f := &Facts{LeadsOpen: 1, TasksOverdue: 2, Target: &TargetFacts{Label: "Q4", Status: "behind", Currency: "SAR", Target: 1_000_000, Actual: 250_050, Expected: 500_000}}
	if _, ok := f.Compact(CapLeadFocus)["overdue_tasks"]; ok {
		t.Fatal("lead focus does not get task counts")
	}
	if _, ok := f.Compact(CapOpsSummary)["target"]; ok {
		t.Fatal("summary does not get money")
	}
	tgt := f.Compact(CapRevenueStatus)["target"].(map[string]any)
	if tgt["actual"] != int64(2500) || tgt["goal"] != int64(10000) {
		t.Fatalf("amounts in major units: %+v", tgt)
	}
	if len(f.Compact(CapMessageDraft)) != 0 || len((*Facts)(nil).Compact(CapOpsSummary)) != 0 {
		t.Fatal("nothing for drafts / nil facts")
	}
	if f.Hash(CapOpsSummary) == f.Hash(CapRevenueStatus) {
		t.Fatal("hash depends on capability")
	}
	long := &Facts{Attention: []string{strings.Repeat("é", 200)}}
	if n := len([]rune(long.attention()[0])); n != maxAttentionRunes {
		t.Fatalf("attention truncated to %d runes, got %d", maxAttentionRunes, n)
	}
}

func TestRender(t *testing.T) {
	f := &Facts{LeadsOpen: 4, TasksOverdue: 6, BookingsUnpaid: 1, Attention: []string{"Call Ahmed"},
		Target: &TargetFacts{Label: "October", Status: "behind", Currency: "SAR", Target: 123456789, Actual: 100000, Expected: 200000}}
	s := RenderEssential(CapOpsSummary, "en", f)
	for _, want := range []string{"**Open leads:** 4", "**Overdue tasks:** 6", "1. Call Ahmed"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q:\n%s", want, s)
		}
	}
	if r := RenderEssential(CapRevenueStatus, "en", f); !strings.Contains(r, "1,234,567 SAR") || !strings.Contains(r, "**behind**") {
		t.Errorf("revenue: %s", r)
	}
	if r := RenderEssential(CapRevenueStatus, "ar", &Facts{}); !strings.Contains(r, "لا يوجد") {
		t.Errorf("no target ar: %s", r)
	}
	if r := RenderEssential(CapLeadFocus, "en", &Facts{}); !strings.Contains(r, "Nothing needs urgent attention") {
		t.Errorf("empty attention: %s", r)
	}
	if !strings.Contains(RenderEssential(CapAppHelp, "en", nil), "Help & FAQ") {
		t.Error("help fallback points to FAQ")
	}
	list := RenderOutOfScope("ar", []CapabilityID{CapOpsSummary, CapGreeting, CapMessageDraft})
	if strings.Count(list, "\n- ") != 2 {
		t.Errorf("only real capabilities are listed: %s", list)
	}
	for _, loc := range []string{"en", "ar"} {
		c := copies[loc]
		for _, id := range []CapabilityID{CapOpsSummary, CapLeadFocus, CapRevenueStatus, CapMessageDraft, CapAppHelp} {
			if c.can[id] == "" {
				t.Errorf("%s: label for %s", loc, id)
			}
		}
	}
	if groupThousands(-1234) != "-1,234" || groupThousands(999) != "999" {
		t.Error("grouping")
	}
}
