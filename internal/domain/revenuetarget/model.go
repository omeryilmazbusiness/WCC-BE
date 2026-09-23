package revenuetarget

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Metric string

const (
	MetricCollected Metric = "collected"
	MetricBooked    Metric = "booked"
)

type ScopeType string

const (
	ScopeBranch   ScopeType = "branch"
	ScopeTeam     ScopeType = "team"
	ScopeEmployee ScopeType = "employee"
)

type CurveType string

const (
	CurveLinear   CurveType = "linear"
	CurveSeasonal CurveType = "seasonal"
)

type Status string

const (
	StatusAhead       Status = "ahead"
	StatusOnTrack     Status = "on_track"
	StatusBehind      Status = "behind"
	StatusPlaceholder Status = "placeholder"
)

type Target struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	OwnerID      *uuid.UUID
	TeamID       *uuid.UUID
	Label        string
	TargetAmount int64
	Currency     string
	Metric       Metric
	ScopeType    ScopeType
	CurveType    CurveType
	PeriodStart  time.Time // date
	PeriodEnd    time.Time
	CreatedBy    *uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Weight struct {
	Bucket    int // 0-based month index within period (or custom)
	WeightBps int
}

type Share struct {
	UserID   uuid.UUID
	UserName string
	ShareBps int
}

type Snapshot struct {
	TargetID       uuid.UUID
	AsOf           time.Time
	ActualAmount   int64
	ExpectedToDate int64
	Variance       int64
	ProgressBps    int // actual/target * 10000
	PaceBps        int // actual/expected * 10000
	ForecastAmount int64
	Status         Status
	UpdatedAt      time.Time
}

type Revision struct {
	ID         uuid.UUID
	TargetID   uuid.UUID
	ActorID    uuid.UUID
	Action     string
	BeforeJSON []byte
	AfterJSON  []byte
	CreatedAt  time.Time
}

type Progress struct {
	TargetID         uuid.UUID `json:"target_id"`
	Label            string    `json:"label"`
	Currency         string    `json:"currency"`
	Metric           Metric    `json:"metric"`
	ScopeType        ScopeType `json:"scope_type"`
	CurveType        CurveType `json:"curve_type"`
	TargetAmount     int64     `json:"target_amount"`
	ActualAmount     int64     `json:"actual_amount"`
	ExpectedToDate   int64     `json:"expected_to_date"`
	Variance         int64     `json:"variance"`
	ProgressBps      int       `json:"progress_bps"`
	PaceBps          int       `json:"pace_bps"`
	ForecastAmount   int64     `json:"forecast_amount"`
	RequiredPaceDaily int64    `json:"required_pace_daily"`
	Status           Status    `json:"status"`
	PeriodStart      string    `json:"period_start"`
	PeriodEnd        string    `json:"period_end"`
	AsOf             string    `json:"as_of"`
}

type Contribution struct {
	UserID       uuid.UUID `json:"user_id"`
	UserName     string    `json:"user_name"`
	ShareBps     int       `json:"share_bps"`
	ActualAmount int64     `json:"actual_amount"`
	ShareAmount  int64     `json:"share_amount"` // target * share
	Rank         int       `json:"rank"`
}

type SeriesPoint struct {
	Date     string `json:"date"`
	Actual   int64  `json:"actual"`
	Expected int64  `json:"expected"`
}

type SourceRow struct {
	Kind        string    `json:"kind"` // booking|payment
	ID          uuid.UUID `json:"id"`
	BookingID   uuid.UUID `json:"booking_id"`
	Amount      int64     `json:"amount"`
	Currency    string    `json:"currency"`
	OwnerID     uuid.UUID `json:"owner_id"`
	OwnerName   string    `json:"owner_name"`
	OccurredAt  string    `json:"occurred_at"`
	Label       string    `json:"label"`
}

// Repository is the persistence port (DIP).
type Repository interface {
	Create(ctx context.Context, t *Target) error
	Update(ctx context.Context, t *Target) error
	Get(ctx context.Context, id uuid.UUID) (*Target, error)
	List(ctx context.Context, branchID uuid.UUID) ([]Target, error)
	ReplaceWeights(ctx context.Context, targetID uuid.UUID, weights []Weight) error
	ListWeights(ctx context.Context, targetID uuid.UUID) ([]Weight, error)
	ReplaceShares(ctx context.Context, targetID uuid.UUID, shares []Share) error
	ListShares(ctx context.Context, targetID uuid.UUID) ([]Share, error)
	UpsertSnapshot(ctx context.Context, s *Snapshot) error
	LatestSnapshot(ctx context.Context, targetID uuid.UUID) (*Snapshot, error)
	InsertRevision(ctx context.Context, r *Revision) error
	ListRevisions(ctx context.Context, targetID uuid.UUID, limit int) ([]Revision, error)

	SumActual(ctx context.Context, t *Target, from, to time.Time) (int64, error)
	SumActualByOwner(ctx context.Context, t *Target, from, to time.Time) ([]Contribution, error)
	ListSources(ctx context.Context, t *Target, from, to time.Time, limit int) ([]SourceRow, error)
	ListTargetsForBranch(ctx context.Context, branchID uuid.UUID) ([]Target, error)
	ManagerUserIDs(ctx context.Context, branchID uuid.UUID) ([]uuid.UUID, error)
}
