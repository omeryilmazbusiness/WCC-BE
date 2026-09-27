package adminconfig

import (
	"context"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
)

func (s *Service) SetAuditor(a audit.Recorder) { s.audit = a }

// recordSettings audits a branch settings change inside the caller's
// transaction (fail closed).
func (s *Service) recordSettings(ctx context.Context, action, section string, branchID uuid.UUID, before, after any, extra map[string]any) error {
	if s.audit == nil {
		return nil
	}
	if extra == nil {
		extra = map[string]any{}
	}
	extra["section"] = section
	branch := branchID
	return s.audit.Record(ctx, audit.RecordInput{
		Action: action, EntityType: "settings", BranchID: &branch, Before: before, After: after, Extra: extra,
	})
}
