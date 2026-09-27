package ai

import (
	"context"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
)

func (s *Service) SetAuditor(a audit.Recorder) { s.audit = a }

// settingsMeta is the audit view of AI settings: metadata only, never keys.
func settingsMeta(st *domain.Settings) map[string]any {
	if st == nil {
		return nil
	}
	return map[string]any{
		"provider": st.Provider, "model": st.Model, "enabled": st.Enabled,
		"setup_completed": st.SetupCompletedAt != nil, "key_sealed": st.SecretsEnc != "",
	}
}

func (s *Service) currentMeta(ctx context.Context, branchID uuid.UUID) map[string]any {
	if s.audit == nil {
		return nil
	}
	st, _ := s.repo.GetSettings(ctx, branchID)
	return settingsMeta(st)
}

// recordSettings is best-effort: AI settings are not a fail-closed domain.
func (s *Service) recordSettings(ctx context.Context, action string, branchID, actorID uuid.UUID, before map[string]any, after *domain.Settings, keyRotated bool) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: actorID, Action: action, EntityType: "ai_settings", EntityID: &branchID, BranchID: &branchID,
		Before: before, After: settingsMeta(after), Extra: map[string]any{"key_rotated": keyRotated},
	})
}
