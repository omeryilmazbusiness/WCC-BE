package ai

import (
	"context"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
)

// AssistantCall is one in-app assistant model call; the prompt is built and
// capped by the assistant package.
type AssistantCall struct {
	BranchID   uuid.UUID
	ActorID    uuid.UUID
	Capability string
	System     string
	User       string
	MaxTokens  int
	InputHash  string
}

// AssistantComplete runs one assistant answer on the branch's provider and
// audits it as an AI run (capability + input hash; the reply as output).
func (s *Service) AssistantComplete(ctx context.Context, in AssistantCall) (string, error) {
	st, p, key, err := s.resolve(ctx, in.BranchID)
	if err != nil {
		return "", err
	}
	actor := in.ActorID
	scope := map[string]any{"capability": in.Capability}
	resp, err := s.complete(ctx, st, p, key, domain.CompletionRequest{System: in.System, User: in.User, MaxTokens: in.MaxTokens, LowReasoning: true})
	if err != nil {
		_, _ = s.record(ctx, in.BranchID, &actor, domain.KindAssistantChat, st, scope, nil, in.InputHash, "error", trimErr(err))
		return "", providerFailure(err, st.Model)
	}
	_, _ = s.record(ctx, in.BranchID, &actor, domain.KindAssistantChat, st, scope,
		map[string]any{"text": resp.Text, "model": resp.Model, "source": "assistant"}, in.InputHash, "ok", "")
	return resp.Text, nil
}
