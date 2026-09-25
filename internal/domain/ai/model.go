package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Provider is a BYO LLM vendor accepted at setup (T-189).
type Provider string

const (
	ProviderNone       Provider = ""
	ProviderOpenAI     Provider = "openai"
	ProviderAnthropic  Provider = "anthropic"
	ProviderGemini     Provider = "gemini"
)

func ValidProvider(p Provider) bool {
	switch p {
	case ProviderOpenAI, ProviderAnthropic, ProviderGemini:
		return true
	default:
		return false
	}
}

func DefaultModel(p Provider) string {
	switch p {
	case ProviderOpenAI:
		return "gpt-4o-mini"
	case ProviderAnthropic:
		return "claude-sonnet-4-20250514"
	case ProviderGemini:
		return "gemini-2.0-flash"
	default:
		return ""
	}
}

// Kind of audited AI invocation.
type Kind string

const (
	KindDailySummary      Kind = "manager.daily_summary"
	KindConversationSum   Kind = "conversation.summary"
	KindReplyDraft        Kind = "conversation.reply_draft"
	KindLeadExplain       Kind = "lead.priority_explain"
	KindTargetInsight     Kind = "target.recovery_insight"
	KindOCRExtract        Kind = "document.ocr_extract"
)

// Settings is per-branch BYO AI configuration (secrets in ConfigJSON).
type Settings struct {
	BranchID          uuid.UUID
	Provider          Provider
	Model             string
	Enabled           bool
	ConfigJSON        json.RawMessage // api_key etc — never expose raw
	SetupCompletedAt  *time.Time
	UpdatedBy         *uuid.UUID
	UpdatedAt         time.Time
	CreatedAt         time.Time
	// Public view (no secrets)
	PublicMeta map[string]any
}

// MaskedKeyHint returns last 4 chars for UI confirmation.
func MaskedKeyHint(apiKey string) string {
	k := strings.TrimSpace(apiKey)
	if len(k) < 4 {
		return ""
	}
	return "••••" + k[len(k)-4:]
}

// ParseAPIKey extracts api_key from config_json without logging it.
func ParseAPIKey(cfg json.RawMessage) string {
	if len(cfg) == 0 {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(cfg, &m); err != nil {
		return ""
	}
	return strings.TrimSpace(m["api_key"])
}

// Run is an auditable AI invocation record (T-189).
type Run struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	ActorID      *uuid.UUID
	Kind         Kind
	Provider     Provider
	Model        string
	ScopeJSON    json.RawMessage
	InputHash    string
	OutputJSON   json.RawMessage
	Feedback     string
	Status       string // ok|error|skipped
	ErrorMessage string
	CreatedAt    time.Time
}

func HashInput(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CompletionRequest is provider-agnostic prompt payload.
type CompletionRequest struct {
	Model      string
	System     string
	User       string
	MaxTokens  int
	JSONMode   bool
	ImageB64   string // optional OCR
	ImageMIME  string
}

// CompletionResponse is normalized LLM output.
type CompletionResponse struct {
	Text  string
	Model string
	Raw   json.RawMessage
}

// CompletionProvider is the DIP port for OpenAI / Anthropic / Gemini.
type CompletionProvider interface {
	Name() Provider
	Complete(ctx context.Context, apiKey string, req CompletionRequest) (*CompletionResponse, error)
}

// ErrNotConfigured is returned when branch has no AI key / provider.
func ErrNotConfigured() *shared.AppError {
	return shared.NewValidation("ai_not_configured: complete AI setup with a provider API key")
}

// Repository persists settings + runs.
type Repository interface {
	GetSettings(ctx context.Context, branchID uuid.UUID) (*Settings, error)
	UpsertSettings(ctx context.Context, s *Settings) error

	InsertRun(ctx context.Context, r *Run) error
	UpdateRunFeedback(ctx context.Context, id uuid.UUID, feedback string) error
	ListRuns(ctx context.Context, branchID uuid.UUID, kind Kind, limit int) ([]Run, error)
}
