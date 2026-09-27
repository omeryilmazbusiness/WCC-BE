package report

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Frequency string

const (
	FrequencyDaily   Frequency = "daily"
	FrequencyWeekly  Frequency = "weekly"
	FrequencyMonthly Frequency = "monthly"
)

// MaxRecipients bounds one schedule's fan-out.
const MaxRecipients = 20

// Schedule is a recurring report delivered in-app to its recipients.
type Schedule struct {
	ID           uuid.UUID   `json:"id"`
	BranchID     uuid.UUID   `json:"branch_id"`
	Kind         Kind        `json:"kind"`
	Frequency    Frequency   `json:"frequency"`
	RecipientIDs []uuid.UUID `json:"recipient_ids"`
	Enabled      bool        `json:"enabled"`
	CreatedBy    *uuid.UUID  `json:"created_by,omitempty"`
	LastRunAt    *time.Time  `json:"last_run_at,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

// ScheduledRun is one generated report file.
type ScheduledRun struct {
	ID         uuid.UUID `json:"id"`
	ScheduleID uuid.UUID `json:"schedule_id"`
	BranchID   uuid.UUID `json:"branch_id"`
	Kind       Kind      `json:"kind"`
	Period     string    `json:"period"`
	Filename   string    `json:"filename"`
	RowCount   int       `json:"row_count"`
	CreatedAt  time.Time `json:"created_at"`
	Content    []byte    `json:"-"`
}

func (s *Schedule) Normalize() error {
	if s.BranchID == uuid.Nil {
		return shared.NewValidation("branch_id is required")
	}
	if !ValidKind(s.Kind) || s.Kind == KindIntegrations {
		return shared.NewValidation("unsupported report kind")
	}
	switch s.Frequency {
	case FrequencyDaily, FrequencyWeekly, FrequencyMonthly:
	default:
		return shared.NewValidation("frequency must be daily, weekly or monthly")
	}
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(s.RecipientIDs))
	for _, id := range s.RecipientIDs {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return shared.NewValidation("at least one recipient is required")
	}
	if len(out) > MaxRecipients {
		return shared.NewValidation(fmt.Sprintf("at most %d recipients", MaxRecipients))
	}
	s.RecipientIDs = out
	return nil
}

// Period is the last complete period of a frequency as of now, in loc:
// yesterday, the previous ISO week or the previous month. from is inclusive,
// to exclusive; key names the period and dedupes runs.
func (f Frequency) Period(now time.Time, loc *time.Location) (key string, from, to time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	switch f {
	case FrequencyWeekly:
		offset := (int(today.Weekday()) + 6) % 7 // days since Monday
		to = today.AddDate(0, 0, -offset)
		from = to.AddDate(0, 0, -7)
		y, w := from.ISOWeek()
		key = fmt.Sprintf("%d-W%02d", y, w)
	case FrequencyMonthly:
		to = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
		from = to.AddDate(0, -1, 0)
		key = from.Format("2006-01")
	default:
		to = today
		from = today.AddDate(0, 0, -1)
		key = from.Format("2006-01-02")
	}
	return key, from.UTC(), to.UTC()
}

// Filename names a run's CSV.
func (r ScheduledRun) FileName() string {
	return "report-" + string(r.Kind) + "-" + strings.ReplaceAll(r.Period, "/", "-") + ".csv"
}

// ScheduleRepository persists schedules and their runs (ISP).
type ScheduleRepository interface {
	ListSchedules(ctx context.Context, branchID uuid.UUID) ([]Schedule, error)
	GetSchedule(ctx context.Context, id uuid.UUID) (*Schedule, error)
	InsertSchedule(ctx context.Context, s *Schedule) error
	UpdateSchedule(ctx context.Context, s *Schedule) error
	DeleteSchedule(ctx context.Context, id uuid.UUID) error
	ListEnabledSchedules(ctx context.Context, limit int) ([]Schedule, error)
	RunExists(ctx context.Context, scheduleID uuid.UUID, period string) (bool, error)
	// InsertRun returns false when the period was already generated.
	InsertRun(ctx context.Context, r *ScheduledRun) (bool, error)
	MarkScheduleRun(ctx context.Context, id uuid.UUID, at time.Time) error
	ListRuns(ctx context.Context, branchID uuid.UUID, scheduleID *uuid.UUID, limit int) ([]ScheduledRun, error)
	GetRun(ctx context.Context, id uuid.UUID) (*ScheduledRun, error)
}
