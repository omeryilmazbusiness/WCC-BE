package ai

import (
	"context"
	"errors"
	"strings"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Stable error codes the frontend localizes; each message is "<code>: <detail>".
const (
	CodeProviderError = "ai_provider_error"
	CodeModelNotFound = "ai_model_not_found"
	CodeKeyInvalid    = "ai_key_invalid"
	CodeRateLimited   = "ai_rate_limited"
)

const verifyTimeout = 20 * time.Second

// providerFailure maps a failed vendor call to a user-actionable validation error.
func providerFailure(err error, model string) *shared.AppError {
	code, field := classifyProviderError(err)
	var app *shared.AppError
	switch code {
	case CodeModelNotFound:
		app = shared.NewValidation(code + ": model " + quote(model) + " is not available for this provider")
	case CodeKeyInvalid:
		app = shared.NewValidation(code + ": the provider rejected the API key")
	case CodeRateLimited:
		app = shared.NewValidation(code + ": provider quota or rate limit reached")
	default:
		return shared.NewValidation(CodeProviderError + ": " + trimErr(err))
	}
	app.Details = map[string]any{field: code}
	return app
}

func classifyProviderError(err error) (code, field string) {
	var pe *domain.ProviderError
	if !errors.As(err, &pe) {
		return CodeProviderError, ""
	}
	body := strings.ToLower(pe.Body)
	switch {
	case pe.Status == 401 || pe.Status == 403,
		pe.Status == 400 && (strings.Contains(body, "api key") || strings.Contains(body, "api_key")):
		return CodeKeyInvalid, "api_key"
	case pe.Status == 404,
		pe.Status == 400 && strings.Contains(body, "model"):
		return CodeModelNotFound, "model"
	case pe.Status == 429:
		return CodeRateLimited, "api_key"
	default:
		return CodeProviderError, ""
	}
}

// verifyCredentials makes one tiny call so a wrong key or model fails at setup, not on first use.
func (s *Service) verifyCredentials(ctx context.Context, provider domain.Provider, key, model string) error {
	if s.registry == nil {
		return shared.NewValidation("ai provider adapter missing")
	}
	p, ok := s.registry.Get(provider)
	if !ok {
		return shared.NewValidation("ai provider adapter missing")
	}
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	_, err := p.Complete(ctx, key, domain.CompletionRequest{Model: model, User: "Reply with OK.", MaxTokens: 8})
	if err != nil {
		return providerFailure(err, model)
	}
	return nil
}

func quote(s string) string { return "\"" + s + "\"" }
