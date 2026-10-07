package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	appai "github.com/wodi-crm/wodi-crm-be/internal/app/ai"
	appassistant "github.com/wodi-crm/wodi-crm-be/internal/app/assistant"
	appdashboard "github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	domainassistant "github.com/wodi-crm/wodi-crm-be/internal/domain/assistant"
)

// assistantFactsBridge reads the assistant's facts through the dashboard
// service, so the caller's access scope applies exactly as on the dashboard.
type assistantFactsBridge struct {
	dash *appdashboard.Service
}

const assistantAttention = 5

func (b assistantFactsBridge) Facts(ctx context.Context, branchID uuid.UUID) (*domainassistant.Facts, error) {
	bid := branchID
	to := time.Now().UTC()
	kpi, err := b.dash.KPIs(ctx, &bid, to.AddDate(0, 0, -30), to)
	if err != nil {
		return nil, err
	}
	f := &domainassistant.Facts{
		LeadsOpen: kpi.LeadsOpen, TasksOverdue: kpi.TasksOverdue,
		BookingsUnpaid: kpi.BookingsUnpaid, MissingDocs: kpi.MissingDocs,
	}
	if att, err := b.dash.Attention(ctx, &bid, assistantAttention); err == nil {
		for _, a := range att {
			f.Attention = append(f.Attention, a.Title)
		}
	}
	if tp, err := b.dash.MyTarget(ctx, branchID, nil); err == nil && tp != nil {
		f.Target = &domainassistant.TargetFacts{
			Label: tp.Label, Status: tp.Status, Currency: tp.Currency,
			Target: tp.TargetAmount, Actual: tp.ActualAmount, Expected: tp.ExpectedToDate,
		}
	}
	return f, nil
}

// assistantCompleterBridge runs assistant prompts on the branch's BYO provider.
type assistantCompleterBridge struct {
	ai *appai.Service
}

func (b assistantCompleterBridge) Configured(ctx context.Context, branchID uuid.UUID) (bool, error) {
	return b.ai.Configured(ctx, branchID)
}

func (b assistantCompleterBridge) Complete(ctx context.Context, in appassistant.Completion) (string, error) {
	return b.ai.AssistantComplete(ctx, appai.AssistantCall{
		BranchID: in.BranchID, ActorID: in.ActorID, Capability: string(in.Capability),
		System: in.Prompt.System, User: in.Prompt.User, MaxTokens: in.Prompt.MaxTokens, InputHash: in.InputHash,
	})
}
