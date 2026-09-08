package lead

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Stage is the CRM pipeline stage (append-only history on change).
type Stage string

const (
	StageNew        Stage = "new"
	StageContacted  Stage = "contacted"
	StageQualified  Stage = "qualified"
	StageProposal   Stage = "proposal"
	StageWon        Stage = "won"
	StageLost       Stage = "lost"
)

type Lead struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	CustomerID   *uuid.UUID
	FullName     string
	Phone        string
	Source       string
	Stage        Stage
	OwnerID      uuid.UUID
	LostReason   string
	Notes        string
	ConvertedBookingID *uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type StageHistory struct {
	ID        uuid.UUID
	LeadID    uuid.UUID
	FromStage *Stage
	ToStage   Stage
	ChangedBy uuid.UUID
	Note      string
	CreatedAt time.Time
}

var allowedTransitions = map[Stage][]Stage{
	StageNew:       {StageContacted, StageLost},
	StageContacted: {StageQualified, StageLost},
	StageQualified: {StageProposal, StageLost},
	StageProposal:  {StageWon, StageLost},
	StageWon:       {},
	StageLost:      {},
}

// CanTransition enforces the status machine (domain rule, SRP).
func CanTransition(from, to Stage) bool {
	for _, s := range allowedTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

func (l *Lead) TransitionTo(to Stage) error {
	if !CanTransition(l.Stage, to) {
		return shared.NewInvalidState("cannot transition lead from " + string(l.Stage) + " to " + string(to))
	}
	l.Stage = to
	l.UpdatedAt = time.Now().UTC()
	return nil
}

type Repository interface {
	Create(ctx context.Context, lead *Lead) error
	Update(ctx context.Context, lead *Lead) error
	FindByID(ctx context.Context, id uuid.UUID) (*Lead, error)
	AppendStageHistory(ctx context.Context, h *StageHistory) error
	ListByOwner(ctx context.Context, ownerID uuid.UUID, limit, offset int) ([]Lead, int, error)
}
