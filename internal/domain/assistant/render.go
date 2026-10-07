package assistant

import (
	"fmt"
	"strconv"
	"strings"
)

// Notice explains why an answer did not come from the model.
type Notice string

const (
	NoticeNone          Notice = ""
	NoticeQuotaReached  Notice = "quota_reached"
	NoticeNotConfigured Notice = "not_configured"
	NoticeAIUnavailable Notice = "ai_unavailable"
	NoticeNotAllowed    Notice = "not_allowed"
)

type copyset struct {
	greeting, outOfScope, notAllowed, helpFallback string
	can                                            map[CapabilityID]string
	summaryHead, leadsHead, revenueHead, noTarget  string
	openLeads, overdue, unpaid, missing, nothing   string
	targetLine                                     string
	status                                         map[string]string
	draft                                          string
}

var copies = map[string]copyset{
	"en": {
		greeting:     "Hello! I can brief you on today, point out leads to focus on, explain revenue against your target, draft customer messages and help with any screen. What do you need?",
		outOfScope:   "I can only help with WODI work. Here is what I can do for you:",
		notAllowed:   "Your role doesn't include access to this information, so I can't answer it. Here is what I can do for you:",
		helpFallback: "Step-by-step help for every screen is in **Settings → Help & FAQ**: search for the screen or task. For anything not covered there, ask your administrator.",
		can: map[CapabilityID]string{
			CapOpsSummary: "Summarise today's operations", CapLeadFocus: "Point out leads to focus on",
			CapRevenueStatus: "Explain revenue against your target", CapMessageDraft: "Draft a customer message",
			CapAppHelp: "Explain how to use a screen",
		},
		summaryHead: "### Today at a glance", leadsHead: "### Where to focus", revenueHead: "### Revenue vs target",
		noTarget:  "There is no active revenue target for you right now.",
		openLeads: "**Open leads:** %d", overdue: "**Overdue tasks:** %d", unpaid: "**Unpaid bookings:** %d", missing: "**Bookings missing documents:** %d",
		nothing:    "Nothing needs urgent attention right now.",
		targetLine: "**%s** — %s collected of %s (expected by now: %s). Status: **%s**.",
		status:     map[string]string{"ahead": "ahead", "on_track": "on track", "behind": "behind", "placeholder": "not started"},
		draft:      "Hello [Customer name],\n\nThank you for contacting us. %s\n\nPlease let me know if you have any questions.\n\nKind regards,\n[Your name]",
	},
	"ar": {
		greeting:     "مرحبًا! يمكنني تلخيص يومك، والإشارة إلى العملاء المحتملين الأهم، وشرح الإيرادات مقابل هدفك، وكتابة رسائل للعملاء، والمساعدة في أي شاشة. بماذا أساعدك؟",
		outOfScope:   "يمكنني المساعدة في أعمال WODI فقط. هذا ما يمكنني فعله لك:",
		notAllowed:   "دورك لا يتيح الوصول إلى هذه المعلومات، لذا لا يمكنني الإجابة. هذا ما يمكنني فعله لك:",
		helpFallback: "تجد شرحًا خطوة بخطوة لكل شاشة في **الإعدادات ← المساعدة والأسئلة الشائعة**: ابحث عن الشاشة أو المهمة. وما لم يُذكر هناك، اسأل المسؤول.",
		can: map[CapabilityID]string{
			CapOpsSummary: "تلخيص عمليات اليوم", CapLeadFocus: "الإشارة إلى العملاء المحتملين الأهم",
			CapRevenueStatus: "شرح الإيرادات مقابل هدفك", CapMessageDraft: "كتابة مسودة رسالة لعميل",
			CapAppHelp: "شرح طريقة استخدام شاشة",
		},
		summaryHead: "### اليوم في لمحة", leadsHead: "### أين تركّز", revenueHead: "### الإيرادات مقابل الهدف",
		noTarget:  "لا يوجد لديك هدف إيرادات نشط حاليًا.",
		openLeads: "**العملاء المحتملون المفتوحون:** %d", overdue: "**المهام المتأخرة:** %d", unpaid: "**الحجوزات غير المدفوعة:** %d", missing: "**حجوزات تنقصها مستندات:** %d",
		nothing:    "لا يوجد ما يحتاج إلى اهتمام عاجل الآن.",
		targetLine: "**%s** — المحصّل %s من %s (المتوقع حتى الآن: %s). الحالة: **%s**.",
		status:     map[string]string{"ahead": "متقدم", "on_track": "على المسار", "behind": "متأخر", "placeholder": "لم يبدأ"},
		draft:      "مرحبًا [اسم العميل]،\n\nشكرًا لتواصلك معنا. %s\n\nلا تتردد في السؤال عن أي استفسار.\n\nمع أطيب التحيات،\n[اسمك]",
	},
}

func copyFor(locale string) copyset { return copies[NormalizeLocale(locale)] }

// RenderGreeting is the fixed reply to hello / thanks.
func RenderGreeting(locale string) string { return copyFor(locale).greeting }

// RenderOutOfScope declines and lists what the viewer may ask instead.
func RenderOutOfScope(locale string, allowed []CapabilityID) string {
	c := copyFor(locale)
	return c.outOfScope + "\n" + bulletList(c, allowed)
}

// RenderNotAllowed declines a capability the viewer's role does not grant.
func RenderNotAllowed(locale string, allowed []CapabilityID) string {
	c := copyFor(locale)
	return c.notAllowed + "\n" + bulletList(c, allowed)
}

func bulletList(c copyset, allowed []CapabilityID) string {
	var b strings.Builder
	for _, id := range allowed {
		if label, ok := c.can[id]; ok {
			b.WriteString("- ")
			b.WriteString(label)
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderEssential answers from facts with templates — no model, no tokens.
func RenderEssential(cap CapabilityID, locale string, f *Facts) string {
	c := copyFor(locale)
	if f == nil {
		f = &Facts{}
	}
	switch cap {
	case CapOpsSummary:
		lines := []string{c.summaryHead,
			"- " + fmt.Sprintf(c.openLeads, f.LeadsOpen),
			"- " + fmt.Sprintf(c.overdue, f.TasksOverdue),
			"- " + fmt.Sprintf(c.unpaid, f.BookingsUnpaid),
			"- " + fmt.Sprintf(c.missing, f.MissingDocs),
		}
		return strings.Join(append(lines, attentionBlock(c, f)...), "\n")
	case CapLeadFocus:
		lines := []string{c.leadsHead, fmt.Sprintf(c.openLeads, f.LeadsOpen)}
		return strings.Join(append(lines, attentionBlock(c, f)...), "\n")
	case CapRevenueStatus:
		if f.Target == nil {
			return c.revenueHead + "\n" + c.noTarget
		}
		t := f.Target
		status := c.status[t.Status]
		if status == "" {
			status = t.Status
		}
		return c.revenueHead + "\n" + fmt.Sprintf(c.targetLine, t.Label,
			money(t.Actual, t.Currency), money(t.Target, t.Currency), money(t.Expected, t.Currency), status)
	case CapMessageDraft:
		return fmt.Sprintf(c.draft, "[…]")
	case CapAppHelp:
		return c.helpFallback
	case CapGreeting:
		return c.greeting
	default:
		return c.outOfScope
	}
}

func attentionBlock(c copyset, f *Facts) []string {
	items := f.attention()
	if len(items) == 0 {
		return []string{"", c.nothing}
	}
	out := []string{""}
	for i, a := range items {
		out = append(out, strconv.Itoa(i+1)+". "+a)
	}
	return out
}

func money(minor int64, currency string) string {
	s := groupThousands(major(minor))
	if currency == "" {
		return s
	}
	return s + " " + currency
}

func groupThousands(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
