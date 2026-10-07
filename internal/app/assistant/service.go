package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/assistant"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Source tells where an answer came from; the UI labels it.
type Source string

const (
	SourceAI    Source = "ai"
	SourceCache Source = "cache"
	SourceData  Source = "data" // essential template over facts
	SourceRule  Source = "rule" // fixed rule reply
)

// Viewer is the asking user and what their role grants.
type Viewer struct {
	UserID   uuid.UUID
	BranchID uuid.UUID
	Can      func(permission string) bool
}

type Request struct {
	Messages []domain.Turn
	Locale   string
	Screen   string
}

type Answer struct {
	Reply      string
	Capability domain.CapabilityID
	Source     Source
	Notice     domain.Notice
	Quota      domain.Quota
}

type Config struct {
	DailyQuota int
	Location   *time.Location
	Now        func() time.Time
	Logger     *slog.Logger
	// ModelTimeout bounds one model call; past it the essential answer is shown.
	ModelTimeout time.Duration
	// BurstPerMinute caps messages per user per minute (flood guard, not the quota).
	BurstPerMinute int
	// MemoTTL reuses facts and provider status across a burst of questions.
	MemoTTL time.Duration
	// Cooldown skips the model for a branch after a provider failure, so the
	// next questions get the essential answer at once instead of waiting.
	Cooldown time.Duration
}

const (
	defaultModelTimeout   = 15 * time.Second
	defaultCooldown       = time.Minute
	defaultBurstPerMinute = 20
	defaultMemoTTL        = 30 * time.Second
	memoEntries           = 2000
)

type Service struct {
	facts      FactsReader
	completer  Completer
	usage      UsageStore
	cache      AnswerCache
	burst      RateWindow
	cfg        Config
	factsMemo  *memo[*domain.Facts]
	configMemo *memo[bool]
	breaker    *breaker
}

func NewService(facts FactsReader, completer Completer, usage UsageStore, cache AnswerCache, cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.ModelTimeout <= 0 {
		cfg.ModelTimeout = defaultModelTimeout
	}
	if cfg.BurstPerMinute <= 0 {
		cfg.BurstPerMinute = defaultBurstPerMinute
	}
	if cfg.MemoTTL == 0 {
		cfg.MemoTTL = defaultMemoTTL
	}
	if cfg.Cooldown == 0 {
		cfg.Cooldown = defaultCooldown
	}
	return &Service{
		facts: facts, completer: completer, usage: usage, cache: cache, cfg: cfg,
		factsMemo:  newMemo[*domain.Facts](cfg.MemoTTL, memoEntries, cfg.Now),
		configMemo: newMemo[bool](cfg.MemoTTL, memoEntries, cfg.Now),
		breaker:    newBreaker(cfg.Cooldown, cfg.Now),
	}
}

// SetRateWindow enables the per-user flood guard (optional; nil disables it).
func (s *Service) SetRateWindow(w RateWindow) { s.burst = w }

func (s *Service) checkBurst(ctx context.Context, userID uuid.UUID) error {
	if s.burst == nil {
		return nil
	}
	n, err := s.burst.Hit(ctx, "assistant:chat:"+userID.String(), time.Minute)
	if err != nil {
		s.cfg.Logger.WarnContext(ctx, "assistant rate window unavailable", "err", err)
		return nil
	}
	if n > s.cfg.BurstPerMinute {
		return shared.NewRateLimited("too many assistant messages; wait a moment", time.Minute)
	}
	return nil
}

// factsFor is keyed by user and branch: facts follow the viewer's access scope.
func (s *Service) factsFor(ctx context.Context, v Viewer) (*domain.Facts, error) {
	return s.factsMemo.get(v.UserID.String()+"|"+v.BranchID.String(), func() (*domain.Facts, error) {
		return s.facts.Facts(ctx, v.BranchID)
	})
}

func (s *Service) configured(ctx context.Context, branchID uuid.UUID) (bool, error) {
	return s.configMemo.get(branchID.String(), func() (bool, error) {
		return s.completer.Configured(ctx, branchID)
	})
}

// Ask answers the last user message, spending model tokens only when no
// cheaper step can.
func (s *Service) Ask(ctx context.Context, v Viewer, req Request) (*Answer, error) {
	question, history, err := validate(req.Messages)
	if err == nil {
		err = s.checkBurst(ctx, v.UserID)
	}
	if err != nil {
		return nil, err
	}
	locale := domain.NormalizeLocale(req.Locale)
	capID := domain.Classify(question)
	cap, _ := domain.Lookup(capID)

	quota, err := s.quota(ctx, v.UserID)
	if err != nil {
		return nil, err
	}
	answer := func(reply string, src Source, notice domain.Notice) *Answer {
		return &Answer{Reply: reply, Capability: capID, Source: src, Notice: notice, Quota: quota}
	}

	switch {
	case capID == domain.CapGreeting:
		return answer(domain.RenderGreeting(locale), SourceRule, domain.NoticeNone), nil
	case capID == domain.CapOutOfScope:
		return answer(domain.RenderOutOfScope(locale, allowedIDs(v)), SourceRule, domain.NoticeNone), nil
	case !cap.Allowed(v.Can):
		return answer(domain.RenderNotAllowed(locale, allowedIDs(v)), SourceRule, domain.NoticeNotAllowed), nil
	}

	var facts *domain.Facts
	if cap.NeedsFacts {
		if facts, err = s.factsFor(ctx, v); err != nil {
			return nil, err
		}
	}
	essential := func(n domain.Notice) *Answer {
		src := SourceRule
		if cap.NeedsFacts {
			src = SourceData
		}
		return answer(domain.RenderEssential(capID, locale, facts), src, n)
	}

	configured, err := s.configured(ctx, v.BranchID)
	if err != nil {
		return nil, err
	}
	if !configured {
		return essential(domain.NoticeNotConfigured), nil
	}
	if quota.Exhausted() {
		return essential(domain.NoticeQuotaReached), nil
	}
	if s.breaker.open(v.BranchID) {
		return essential(domain.NoticeAIUnavailable), nil
	}

	prompt := domain.BuildPrompt(cap, locale, facts, history, question)
	if screen := screenHint(req.Screen); capID == domain.CapAppHelp && screen != "" {
		prompt.User = "Screen: " + screen + "\n" + prompt.User
	}
	key := cacheKey(v, capID, locale, facts, prompt.User)
	if hit, ok := s.cache.Get(key); ok {
		return answer(hit, SourceCache, domain.NoticeNone), nil
	}

	callCtx, cancel := context.WithTimeout(ctx, s.cfg.ModelTimeout)
	text, err := s.completer.Complete(callCtx, Completion{
		BranchID: v.BranchID, ActorID: v.UserID, Capability: capID, Prompt: prompt, InputHash: key,
	})
	cancel()
	reply := cleanReply(text)
	if err != nil || reply == "" {
		s.breaker.trip(v.BranchID)
		s.cfg.Logger.WarnContext(ctx, "assistant model fallback", "capability", capID, "err", err)
		return essential(domain.NoticeAIUnavailable), nil
	}
	day := domain.Day(s.cfg.Now(), s.cfg.Location)
	if err := s.usage.Add(ctx, v.UserID, v.BranchID, day, domain.EstimateTokens(prompt.System, prompt.User, reply)); err != nil {
		s.cfg.Logger.WarnContext(ctx, "assistant usage not recorded", "err", err)
	}
	quota.Used++
	s.cache.Put(key, reply)
	return answer(reply, SourceAI, domain.NoticeNone), nil
}

// Protocol is what the assistant may do for this viewer, today.
type Protocol struct {
	Version      string
	Rules        []domain.RuleID
	Capabilities []CapabilityView
	Quota        domain.Quota
	AIConfigured bool
}

type CapabilityView struct {
	domain.Capability
	Allowed bool
}

func (s *Service) Protocol(ctx context.Context, v Viewer) (*Protocol, error) {
	quota, err := s.quota(ctx, v.UserID)
	if err != nil {
		return nil, err
	}
	configured, err := s.configured(ctx, v.BranchID)
	if err != nil {
		return nil, err
	}
	caps := domain.Capabilities()
	views := make([]CapabilityView, 0, len(caps))
	for _, c := range caps {
		views = append(views, CapabilityView{Capability: c, Allowed: c.Allowed(v.Can)})
	}
	return &Protocol{Version: domain.ProtocolVersion, Rules: domain.Rules(), Capabilities: views, Quota: quota, AIConfigured: configured}, nil
}

func (s *Service) quota(ctx context.Context, userID uuid.UUID) (domain.Quota, error) {
	now := s.cfg.Now()
	used, err := s.usage.Used(ctx, userID, domain.Day(now, s.cfg.Location))
	if err != nil {
		return domain.Quota{}, err
	}
	return domain.Quota{Limit: s.cfg.DailyQuota, Used: used, ResetsAt: domain.NextReset(now, s.cfg.Location)}, nil
}

// validate returns the question (last message, from the user) and the turns before it.
func validate(messages []domain.Turn) (string, []domain.Turn, error) {
	if len(messages) == 0 || len(messages) > domain.MaxMessages {
		return "", nil, shared.NewValidation("messages must hold 1 to 50 turns")
	}
	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			return "", nil, shared.NewValidation("role must be user or assistant")
		}
		if utf8.RuneCountInString(m.Content) > domain.MaxReplyChars {
			return "", nil, shared.NewValidation("message too long")
		}
	}
	last := messages[len(messages)-1]
	question := strings.TrimSpace(last.Content)
	if last.Role != "user" || question == "" {
		return "", nil, shared.NewValidation("the last message must be a non-empty user question")
	}
	if utf8.RuneCountInString(question) > domain.MaxPromptChars {
		return "", nil, shared.NewValidation("question too long")
	}
	return question, messages[:len(messages)-1], nil
}

func allowedIDs(v Viewer) []domain.CapabilityID {
	var out []domain.CapabilityID
	for _, c := range domain.Capabilities() {
		if c.Kind != domain.KindRule && c.Allowed(v.Can) {
			out = append(out, c.ID)
		}
	}
	return out
}

// cacheKey covers everything that changes the answer; user-bound because facts
// are scoped to the viewer.
func cacheKey(v Viewer, c domain.CapabilityID, locale string, f *domain.Facts, promptUser string) string {
	h := sha256.New()
	for _, p := range []string{v.UserID.String(), v.BranchID.String(), string(c), locale, domain.ProtocolVersion, f.Hash(c), promptUser} {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// screenHint keeps a short, letters-only screen id so it cannot carry instructions.
func screenHint(screen string) string {
	screen = strings.TrimSpace(screen)
	if screen == "" || len(screen) > 40 {
		return ""
	}
	for _, r := range screen {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || r == '-') {
			return ""
		}
	}
	return screen
}

func cleanReply(text string) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > domain.MaxReplyChars {
		text = string([]rune(text)[:domain.MaxReplyChars])
	}
	return text
}
