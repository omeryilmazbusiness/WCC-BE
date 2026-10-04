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
	StagePaid      Stage = "paid" // customer paid; the booking is not created yet
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
	Profile            Profile
	Interest           TripInterest
	NoFollowUp         bool
	ConvertedBookingID *uuid.UUID
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// LostRecord is a lead lost within a period, as loss analysis sees it: the
// reason and context only, never the customer's name or contact details.
type LostRecord struct {
	ReasonCode string
	Note       string
	Source     string
	FromStage  string
	LostAt     time.Time
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
	CustomerID *uuid.UUID
	Stage      Stage
	Source     string
	Priority   Priority
	Query      string
	NoFollowUp *bool
	// CreatedFrom and CreatedTo bound created_at as [from, to).
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Sort        Sort
	Limit       int
	Offset      int
}

// Sort is a whitelisted list ordering; the zero value is SortUpdated.
type Sort string

const (
	SortUpdated Sort = "updated"
	SortCreated Sort = "created"
	SortOldest  Sort = "oldest"
	SortName    Sort = "name"
	SortBudget  Sort = "budget"
	SortTravel  Sort = "travel"
)

func ValidSort(s Sort) bool {
	switch s {
	case "", SortUpdated, SortCreated, SortOldest, SortName, SortBudget, SortTravel:
		return true
	}
	return false
}

// MaxBoardPerStage caps the cards returned per lane in one board request.
const MaxBoardPerStage = 100

// MaxBulk caps how many leads one bulk request may touch.
const MaxBulk = 200

// BudgetSum totals the budgets of a lane in one currency, minor units.
type BudgetSum struct {
	Currency string `json:"currency"`
	Amount   int64  `json:"amount"`
	Count    int    `json:"count"`
}

// BoardColumn is one pipeline lane: the filtered total, its budgets and the
// first page of cards.
type BoardColumn struct {
	Stage      Stage
	Total      int
	NoFollowUp int
	Budgets    []BudgetSum
	Items      []Lead
}

type Board struct {
	Columns []BoardColumn
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
	StageProposal:  {StagePaid, StageLost},
	StagePaid:      {StageWon, StageLost},
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

// ConversionPath is the stages a lead walks through to reach won when it is
// converted to a booking: proposal and paid leads qualify, won needs no steps.
func ConversionPath(from Stage) ([]Stage, bool) {
	switch from {
	case StageProposal:
		return []Stage{StagePaid, StageWon}, true
	case StagePaid:
		return []Stage{StageWon}, true
	case StageWon:
		return nil, true
	default:
		return nil, false
	}
}

// AllStages is the pipeline order, closed stages last.
func AllStages() []Stage {
	return []Stage{StageNew, StageContacted, StageQualified, StageProposal, StagePaid, StageWon, StageLost}
}

func ValidStage(s Stage) bool {
	for _, x := range AllStages() {
		if x == s {
			return true
		}
	}
	return false
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
	Analytics(ctx context.Context, branchID *uuid.UUID) (*Analytics, error)
	// Board summarises every stage for f (f.Stage, Limit and Offset are
	// ignored) and returns up to perStage cards per lane; 0 skips the cards.
	Board(ctx context.Context, f ListFilter, perStage int) (*Board, error)
	// SoftDelete hides a live lead and cancels its open tasks.
	SoftDelete(ctx context.Context, id, by uuid.UUID, at time.Time) error
	// Restore brings back a deleted lead and reopens the tasks its deletion cancelled.
	Restore(ctx context.Context, id uuid.UUID, at time.Time) error
}
