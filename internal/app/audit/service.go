package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Recorder re-exports the domain port for composition roots.
type Recorder = domain.Recorder

type Service struct {
	repo domain.Repository
}

func NewService(repo domain.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Record(ctx context.Context, in domain.RecordInput) error {
	if in.Action == "" || in.EntityType == "" {
		return shared.NewValidation("action and entity_type are required")
	}
	meta := map[string]any{}
	if in.Before != nil {
		meta["before"] = in.Before
	}
	if in.After != nil {
		meta["after"] = in.After
	}
	for k, v := range in.Extra {
		meta[k] = v
	}
	raw, _ := json.Marshal(meta)
	actor := in.ActorID
	e := &domain.Event{
		ID:         uuid.New(),
		ActorID:    &actor,
		Action:     in.Action,
		EntityType: in.EntityType,
		EntityID:   in.EntityID,
		BranchID:   in.BranchID,
		Metadata:   raw,
		IP:         in.IP,
		UserAgent:  in.UserAgent,
		CreatedAt:  time.Now().UTC(),
	}
	return s.repo.Insert(ctx, e)
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
