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
	StageNew       Stage = "new"
	StageContacted Stage = "contacted"
	StageQualified Stage = "qualified"
	StageProposal  Stage = "proposal"
	StageWon       Stage = "won"
	StageLost      Stage = "lost"
)

// Lost reason taxonomy (T-038). Free-text note lives alongside the code.
const (
	LostPrice         = "price"
	LostCompetitor    = "competitor"
	LostTiming        = "timing"
	LostNoResponse    = "no_response"
	LostNotInterested = "not_interested"
	LostOther         = "other"
)

func LostReasonCodes() []string {
	return []string{
		LostPrice, LostCompetitor, LostTiming, LostNoResponse, LostNotInterested, LostOther,
	}
}

func ValidLostReasonCode(code string) bool {
	for _, c := range LostReasonCodes() {
		if c == code {
			return true
		}
	}
	return false
}

type Lead struct {
	ID                 uuid.UUID
	BranchID           uuid.UUID
	CustomerID         *uuid.UUID
	FullName           string
	Phone              string
	Source             string
	Stage              Stage
	OwnerID            uuid.UUID
	OwnerName          string // join enrichment (not persisted)
	LostReasonCode     string
	LostReason         string
	Notes              string
	NoFollowUp         bool
	ConvertedBookingID *uuid.UUID
	CreatedAt          time.Time
	UpdatedAt          time.Time
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

type ListFilter struct {
	BranchID   *uuid.UUID
	OwnerID    *uuid.UUID
	Stage      Stage
	Query      string
	NoFollowUp *bool
	Limit      int
	Offset     int
}

type Analytics struct {
	Total          int            `json:"total"`
	Open           int            `json:"open"`
	Won            int            `json:"won"`
	Lost           int            `json:"lost"`
	ConversionRate float64        `json:"conversion_rate"` // won / (won+lost)
	ByStage        []CountBucket  `json:"by_stage"`
	BySource       []SourceBucket `json:"by_source"`
	ByOwner        []OwnerBucket  `json:"by_owner"`
	NoFollowUp     int            `json:"no_follow_up"`
}

type CountBucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type SourceBucket struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
	Won    int    `json:"won"`
}

type OwnerBucket struct {
	OwnerID   uuid.UUID `json:"owner_id"`
	OwnerName string    `json:"owner_name"`
	Count     int       `json:"count"`
	Won       int       `json:"won"`
	Lost      int       `json:"lost"`
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

func (l *Lead) IsOpen() bool {
	return l.Stage != StageWon && l.Stage != StageLost
}

type Repository interface {
	Create(ctx context.Context, lead *Lead) error
	Update(ctx context.Context, lead *Lead) error
	FindByID(ctx context.Context, id uuid.UUID) (*Lead, error)
	List(ctx context.Context, f ListFilter) ([]Lead, int, error)
	AppendStageHistory(ctx context.Context, h *StageHistory) error
	ListStageHistory(ctx context.Context, leadID uuid.UUID) ([]StageHistory, error)
	Analytics(ctx context.Context, branchID uuid.UUID) (*Analytics, error)
}
