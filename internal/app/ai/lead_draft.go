package ai

import (
	"context"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// DraftConversation is what lead extraction reads from a conversation.
// Lines are "customer: …" / "agent: …", oldest first, internal notes excluded.
type DraftConversation struct {
	ContactName  string
	ContactPhone string
	LeadID       *uuid.UUID
	Lines        []string
}

type DraftConversationReader interface {
	DraftConversation(ctx context.Context, conversationID, branchID uuid.UUID, limit int) (*DraftConversation, error)
}

type CatalogPackage struct {
	ID   uuid.UUID
	Code string
	Name string
}

type PackageCatalog interface {
	ActivePackages(ctx context.Context, branchID uuid.UUID) ([]CatalogPackage, error)
}

func (s *Service) SetDraftConversationReader(r DraftConversationReader) { s.drafts = r }
func (s *Service) SetPackageCatalog(c PackageCatalog)                   { s.catalog = c }

// LeadDraft is a lead form prefill. BudgetAmount is in minor units; AIFields
// names the fields the model filled so the UI can mark them for review.
type LeadDraft struct {
	FullName        string     `json:"full_name"`
	Phone           string     `json:"phone"`
	TravelDate      string     `json:"travel_date"`
	TravelWindow    string     `json:"travel_window"`
	PaxCount        *int       `json:"pax_count"`
	BudgetAmount    *int64     `json:"budget_amount"`
	BudgetCurrency  string     `json:"budget_currency"`
	PackageID       *uuid.UUID `json:"package_id"`
	PackageInterest string     `json:"package_interest"`
	Notes           string     `json:"notes"`
	AIFields        []string   `json:"ai_fields"`
}

const (
	draftMessages      = 60
	draftTranscriptMax = 9000
	draftCatalogMax    = 60
	draftMaxTokens     = 600
	draftNameMax       = 120
	draftWindowMax     = 79 // truncation adds an ellipsis; stays within the lead limits
	draftInterestMax   = 199
	draftNotesMax      = 500
	draftMaxPax        = 500
	draftMaxBudget     = 1e10 // major units
)

const leadDraftSystem = `You extract sales-lead details from a chat between a Hajj and Umrah travel agency ("agent") and a customer ("customer").
Reply with JSON only:
{"full_name":null,"travel_date":null,"travel_window":null,"pax_count":null,"budget_amount":null,"budget_currency":null,"package_code":null,"package_interest":null,"notes":null}
Rules:
- Use only what the chat states. When something is unknown or unclear, use null. Never guess.
- full_name: the customer's own name as written.
- travel_date: YYYY-MM-DD only when a specific day is stated; resolve relative dates ("next Friday") from today's date.
- travel_window: short text when only a month, season or period is given, e.g. "March 2027", "Ramadan 2027", "after Eid".
- pax_count: total number of travellers as an integer.
- budget_amount: total budget as a number in the currency's normal units. If a per-person budget and the traveller count are both given, multiply them.
- budget_currency: ISO 4217 code, e.g. SAR, USD, TRY, EUR.
- package_code: a code from the catalogue only when the customer clearly means that package.
- package_interest: when no catalogue package clearly matches, a short description of what they want, e.g. "5-star Umrah, 10 nights".
- notes: at most two short English sentences with other useful facts (departure city, room type, special needs). No greetings.`

// LeadDraftFromConversation asks the branch's AI to read a conversation and
// fill a lead form. It needs AI; without it the caller gets ai_not_configured.
func (s *Service) LeadDraftFromConversation(ctx context.Context, branchID, actorID, conversationID uuid.UUID) (map[string]any, error) {
	if s.drafts == nil {
		return nil, shared.NewValidation("conversation reader not wired")
	}
	conv, err := s.drafts.DraftConversation(ctx, conversationID, branchID, draftMessages)
	if err != nil {
		return nil, err
	}
	if !hasCustomerLine(conv.Lines) {
		return nil, shared.NewValidation("conversation has no customer messages yet")
	}
	st, p, key, err := s.resolve(ctx, branchID)
	if err != nil {
		return nil, err
	}
	var catalog []CatalogPackage
	if s.catalog != nil {
		if catalog, err = s.catalog.ActivePackages(ctx, branchID); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	transcript := strings.Join(conv.Lines, "\n")
	if len(transcript) > draftTranscriptMax {
		transcript = transcript[len(transcript)-draftTranscriptMax:]
	}
	scope := map[string]any{"conversation_id": conversationID, "message_count": len(conv.Lines), "catalog_size": len(catalog)}
	hash := domain.HashInput("lead_draft", conversationID.String(), transcript)

	resp, err := s.complete(ctx, st, p, key, domain.CompletionRequest{
		System: leadDraftSystem, User: leadDraftPrompt(now, catalog, transcript),
		JSONMode: true, MaxTokens: draftMaxTokens,
	})
	if err != nil {
		_, _ = s.record(ctx, branchID, &actorID, domain.KindLeadDraft, st, scope, nil, hash, "error", err.Error())
		return nil, providerFailure(err, st.Model)
	}
	draft := sanitizeLeadDraft(parseJSONObject(resp.Text), catalog, conv, now)
	run, _ := s.record(ctx, branchID, &actorID, domain.KindLeadDraft, st, scope, draft, hash, "ok", "")
	return map[string]any{
		"draft":   draft,
		"lead_id": conv.LeadID,
		"source":  "ai",
		"model":   resp.Model,
		"run_id":  runID(run),
	}, nil
}

func hasCustomerLine(lines []string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, "customer:") {
			return true
		}
	}
	return false
}

func leadDraftPrompt(now time.Time, catalog []CatalogPackage, transcript string) string {
	var b strings.Builder
	b.WriteString("Today: " + now.Format(time.DateOnly) + " (" + now.Weekday().String() + ")\n\nPackage catalogue (code: name):\n")
	if len(catalog) == 0 {
		b.WriteString("(none)\n")
	}
	for i, p := range catalog {
		if i == draftCatalogMax {
			break
		}
		b.WriteString(p.Code + ": " + p.Name + "\n")
	}
	b.WriteString("\nChat:\n" + transcript)
	return b.String()
}

var isoCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

// sanitizeLeadDraft turns model output into a safe prefill: every field is
// type-checked and range-checked, the package must come from the catalogue
// and the phone always comes from the channel, never from the model.
func sanitizeLeadDraft(raw map[string]any, catalog []CatalogPackage, conv *DraftConversation, now time.Time) LeadDraft {
	d := LeadDraft{Phone: conv.ContactPhone, AIFields: []string{}}
	mark := func(f string) { d.AIFields = append(d.AIFields, f) }

	if name := truncateRunes(str(raw["full_name"]), draftNameMax); name != "" {
		d.FullName = name
		mark("full_name")
	} else {
		d.FullName = strings.TrimSpace(conv.ContactName)
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if t, err := time.Parse(time.DateOnly, str(raw["travel_date"])); err == nil &&
		!t.Before(today.AddDate(0, 0, -1)) && !t.After(today.AddDate(3, 0, 0)) {
		d.TravelDate = t.Format(time.DateOnly)
		mark("travel_date")
	}
	if w := truncateRunes(str(raw["travel_window"]), draftWindowMax); w != "" {
		d.TravelWindow = w
		mark("travel_window")
	}

	if n, ok := number(raw["pax_count"]); ok && n == math.Trunc(n) && n >= 1 && n <= draftMaxPax {
		pax := int(n)
		d.PaxCount = &pax
		mark("pax_count")
	}

	cur := strings.ToUpper(str(raw["budget_currency"]))
	if n, ok := number(raw["budget_amount"]); ok && n > 0 && n <= draftMaxBudget && isoCurrency.MatchString(cur) {
		minor := int64(math.Round(n * 100))
		d.BudgetAmount = &minor
		d.BudgetCurrency = cur
		mark("budget_amount")
	}

	if code := str(raw["package_code"]); code != "" {
		for _, p := range catalog {
			if strings.EqualFold(p.Code, code) {
				id := p.ID
				d.PackageID = &id
				mark("package_id")
				break
			}
		}
	}
	if d.PackageID == nil {
		if in := truncateRunes(str(raw["package_interest"]), draftInterestMax); in != "" {
			d.PackageInterest = in
			mark("package_interest")
		}
	}

	if notes := truncateRunes(str(raw["notes"]), draftNotesMax); notes != "" {
		d.Notes = notes
		mark("notes")
	}
	return d
}

func str(v any) string {
	s, _ := v.(string)
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "null") || strings.EqualFold(s, "unknown") {
		return ""
	}
	return s
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case string:
		f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(n), ",", ""), 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}
