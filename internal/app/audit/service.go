package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Recorder re-exports the domain port for composition roots.
type Recorder = domain.Recorder

type Service struct {
	repo domain.Repository
	now  func() time.Time
}

func NewService(repo domain.Repository) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// Record appends an event. Inside a transaction on ctx the insert joins it,
// so callers that return this error make the change fail closed.
func (s *Service) Record(ctx context.Context, in domain.RecordInput) error {
	if in.Action == "" || in.EntityType == "" {
		return shared.NewValidation("action and entity_type are required")
	}
	before, err := snapshot(in.Before)
	if err != nil {
		return fmt.Errorf("audit before: %w", err)
	}
	after, err := snapshot(in.After)
	if err != nil {
		return fmt.Errorf("audit after: %w", err)
	}
	extra := in.Extra
	if extra == nil {
		extra = map[string]any{}
	}
	meta, err := json.Marshal(extra)
	if err != nil {
		return fmt.Errorf("audit extra: %w", err)
	}
	actor := domain.ResolveActor(ctx, in)
	return s.repo.Insert(ctx, &domain.Event{
		ID:         uuid.New(),
		ActorID:    actor.UserID,
		ActorType:  actor.Type,
		Action:     in.Action,
		EntityType: in.EntityType,
		EntityID:   in.EntityID,
		BranchID:   in.BranchID,
		Before:     before,
		After:      after,
		Metadata:   meta,
		IP:         actor.IP,
		UserAgent:  actor.UserAgent,
		SessionID:  actor.SessionID,
		RequestID:  actor.RequestID,
		CreatedAt:  s.now(),
	})
}

func (s *Service) List(ctx context.Context, f domain.ListFilter) ([]domain.Event, int64, error) {
	if f.Limit <= 0 {
		f.Limit = shared.DefaultPageLimit
	}
	if f.Limit > shared.MaxPageLimit {
		f.Limit = shared.MaxPageLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return s.repo.List(ctx, f)
}

// Actions lists the distinct actions visible to the caller.
func (s *Service) Actions(ctx context.Context) ([]string, error) {
	return s.repo.ListActions(ctx)
}

// Export audits the export itself before any row leaves the system (fail
// closed), then streams up to domain.ExportMaxRows events to fn.
func (s *Service) Export(ctx context.Context, f domain.ListFilter, fn func(domain.Event) error) error {
	if err := s.Record(ctx, domain.RecordInput{
		Action: "audit.exported", EntityType: "audit_event", BranchID: f.BranchID,
		Extra: map[string]any{"filters": exportFilters(f), "max_rows": domain.ExportMaxRows, "format": "csv"},
	}); err != nil {
		return err
	}
	return s.repo.Stream(ctx, f, domain.ExportMaxRows, fn)
}

func exportFilters(f domain.ListFilter) map[string]any {
	m := map[string]any{}
	if f.ActorID != nil {
		m["actor_id"] = *f.ActorID
	}
	if f.EntityType != "" {
		m["entity_type"] = f.EntityType
	}
	if f.EntityID != nil {
		m["entity_id"] = *f.EntityID
	}
	if f.Action != "" {
		m["action"] = f.Action
	}
	if f.BranchID != nil {
		m["branch_id"] = *f.BranchID
	}
	if f.From != nil {
		m["from"] = f.From.UTC().Format(time.RFC3339)
	}
	if f.To != nil {
		m["to"] = f.To.UTC().Format(time.RFC3339)
	}
	return m
}

func snapshot(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	return raw, nil
}
