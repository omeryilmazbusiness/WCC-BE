package assistant

import (
	"strings"
	"unicode"
)

// signal is a set of cues for one capability. Words match whole tokens, or a
// prefix when they end in "*"; phrases match anywhere (needed for Arabic).
type signal struct {
	id      CapabilityID
	words   []string
	phrases []string
}

// Order is priority: the first capability with a cue wins.
var signals = []signal{
	{
		id:      CapMessageDraft,
		words:   []string{"draft*", "compose", "rewrite", "taslak*", "yaz", "yazar", "yazın"},
		phrases: []string{"write a", "write an", "write me", "reply to", "message to", "message for", "mesaj yaz", "مسودة", "اكتب", "صياغة"},
	},
	{
		id:      CapAppHelp,
		phrases: []string{"how do i", "how can i", "how to", "where is", "where can", "where do", "nasıl yap", "nasıl ekle", "nasıl aç", "nerede", "nereden", "كيف أ", "كيف يمكنني", "أين", "اين"},
	},
	{
		// Advice is a work request even when it names a data topic ("tips to grow sales").
		id:      CapWorkRequest,
		words:   []string{"tip", "tips", "advice", "advise", "idea*", "strateg*", "improve", "increase", "öneri*", "tavsiye*", "fikir*", "strateji*", "artır*"},
		phrases: []string{"how should", "what should we do", "best way", "نصيحة", "نصائح", "اقتراح", "استراتيجية", "كيف نزيد"},
	},
	{
		id:      CapRevenueStatus,
		words:   []string{"revenue*", "target*", "sales", "collected", "collection*", "income", "margin*", "gelir*", "hedef*", "satış*", "tahsilat*", "ciro*"},
		phrases: []string{"إيراد", "ايراد", "هدف", "أهداف", "مبيعات", "تحصيل"},
	},
	{
		id:      CapLeadFocus,
		words:   []string{"lead*", "pipeline", "prospect*", "aday*", "fırsat*"},
		phrases: []string{"follow up", "follow-up", "focus on", "potansiyel", "طلب", "عملاء محتمل", "العملاء المحتملين", "متابعة"},
	},
	{
		id: CapOpsSummary,
		words: []string{"summar*", "today", "overview", "brief*", "status", "overdue", "unpaid", "missing", "task*", "booking*", "document*",
			"özet*", "bugün*", "durum*", "görev*", "rezervasyon*", "evrak*", "eksik"},
		phrases: []string{"what's up", "how are we", "ملخص", "اليوم", "مهام", "حجوزات", "حجز", "مستند", "متأخر"},
	},
	{
		id:      CapAppHelp,
		words:   []string{"screen*", "page*", "button*", "setting*", "ekran*", "sayfa*", "ayar*", "nasıl"},
		phrases: []string{"كيف", "شاشة", "صفحة", "إعداد"},
	},
}

// offTopic declines, without a model call, requests that are plainly not WODI work.
// It is checked only after every capability cue missed, so "draft a message about
// the weather delay" still drafts.
var offTopic = signal{
	id: CapOutOfScope,
	words: []string{"joke*", "poem*", "lyric*", "song*", "recipe*", "movie*", "football", "soccer", "horoscope*",
		"bitcoin", "crypto*", "politic*", "election*", "celebrit*", "girlfriend", "boyfriend",
		"fıkra", "şiir*", "şarkı*", "futbol", "siyaset*", "burç*", "kripto*"},
	phrases: []string{"capital of", "who won", "love you", "miss you", "date me", "yemek tarifi", "maç sonucu",
		"نكتة", "قصيدة", "أغنية", "اغنية", "وصفة", "كرة القدم", "سياسة", "أبراج"},
}

var greetings = []string{
	"hi", "hello", "hey", "thanks", "thank you", "good morning", "good evening", "how are you",
	"merhaba", "selam", "teşekkürler", "teşekkür ederim", "sağ ol", "nasılsın",
	"مرحبا", "مرحباً", "السلام عليكم", "شكرا", "شكراً", "أهلا", "اهلا", "كيف حالك",
}

const maxGreetingRunes = 24

// Classify maps a question to one capability without any model call. Anything
// without a cue is a work request: the model does it or answers OutOfScopeReply.
func Classify(question string) CapabilityID {
	text := strings.ToLower(strings.TrimSpace(question))
	if text == "" {
		return CapOutOfScope
	}
	bare := strings.TrimFunc(text, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsSymbol(r) })
	if len([]rune(bare)) <= maxGreetingRunes {
		for _, g := range greetings {
			if bare == g {
				return CapGreeting
			}
		}
	}
	tokens := tokenize(text)
	for _, s := range signals {
		if hasPhrase(text, s.phrases) || hasWord(tokens, s.words) {
			return s.id
		}
	}
	if hasPhrase(text, offTopic.phrases) || hasWord(tokens, offTopic.words) {
		return CapOutOfScope
	}
	return CapWorkRequest
}

func tokenize(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

func hasPhrase(text string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

func hasWord(tokens, words []string) bool {
	for _, w := range words {
		prefix, isPrefix := strings.CutSuffix(w, "*")
		for _, t := range tokens {
			if t == w || (isPrefix && strings.HasPrefix(t, prefix)) {
				return true
			}
		}
	}
	return false
}
