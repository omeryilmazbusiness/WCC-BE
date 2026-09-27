package automation

import (
	"context"
	"time"

	"github.com/google/uuid"

	notificationdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	taskdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

// RuleSource yields a branch's effective escalation matrix (adminconfig).
type RuleSource interface {
	EscalationRules(ctx context.Context, branchID uuid.UUID) ([]notificationdomain.Rule, error)
}

// TaskGrace implements task.GracePolicy from the branch escalation matrix:
// a disabled task.overdue rule stops task escalation, a changed window
// applies to manual tasks, rule tasks keep their rule grace.
type TaskGrace struct {
	Rules RuleSource
}

func (g TaskGrace) Grace(ctx context.Context, branchID uuid.UUID, rule string) (time.Duration, bool) {
	fallback := taskdomain.Grace(rule, nil)
	if g.Rules == nil {
		return fallback, true
	}
	rules, err := g.Rules.EscalationRules(ctx, branchID)
	if err != nil {
		return fallback, true
	}
	for _, r := range rules {
		if r.Kind != notificationdomain.KindTaskOverdue {
			continue
		}
		def := notificationdomain.MatchRule(notificationdomain.KindTaskOverdue)
		if rule == "" && def != nil && r.EscalateAfter > 0 && r.EscalateAfter != def.EscalateAfter {
			return r.EscalateAfter, true
		}
		return fallback, true
	}
	return 0, false
}
