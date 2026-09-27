package report

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/report"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Generator renders one report kind for a period as CSV (satisfied by Service).
type Generator interface {
	Run(ctx context.Context, kind domain.Kind, f domain.Filter) (*domain.Result, error)
}

// Recipients lists the users of a branch allowed to receive report exports.
type Recipients interface {
	Eligible(ctx context.Context, branchID uuid.UUID) (map[uuid.UUID]bool, error)
}

// RunNotifier tells recipients a scheduled report is ready.
type RunNotifier interface {
	ReportReady(ctx context.Context, run domain.ScheduledRun, recipients []uuid.UUID) error
}

type ScheduleInput struct {
	Kind         domain.Kind      `json:"kind"`
	Frequency    domain.Frequency `json:"frequency"`
	RecipientIDs []uuid.UUID      `json:"recipient_ids"`
	Enabled      *bool            `json:"enabled"`
}

// Scheduler owns report schedules and generates due runs (T-280).
type Scheduler struct {
	repo       domain.ScheduleRepository
	gen        Generator
	recipients Recipients
	notifier   RunNotifier
	tx         tx.Runner
	loc        *time.Location
	now        func() time.Time
}

func NewScheduler(repo domain.ScheduleRepository, gen Generator, recipients Recipients, notifier RunNotifier, runner tx.Runner, loc *time.Location) *Scheduler {
	if loc == nil {
		loc = time.UTC
	}
	return &Scheduler{repo: repo, gen: gen, recipients: recipients, notifier: notifier, tx: runner, loc: loc,
		now: func() time.Time { return time.Now().UTC() }}
}

func (s *Scheduler) List(ctx context.Context, branchID uuid.UUID) ([]domain.Schedule, error) {
	return s.repo.ListSchedules(ctx, branchID)
}

func (s *Scheduler) Create(ctx context.Context, branchID uuid.UUID, in ScheduleInput) (*domain.Schedule, error) {
	now := s.now()
	sc := &domain.Schedule{
		ID: uuid.New(), BranchID: branchID, Kind: in.Kind, Frequency: in.Frequency,
		RecipientIDs: in.RecipientIDs, Enabled: in.Enabled == nil || *in.Enabled,
		CreatedAt: now, UpdatedAt: now,
	}
	if uid := access.From(ctx).UserID; uid != uuid.Nil {
		sc.CreatedBy = &uid
	}
	if err := s.validate(ctx, sc); err != nil {
		return nil, err
	}
	if err := s.repo.InsertSchedule(ctx, sc); err != nil {
		return nil, err
	}
	return sc, nil
}

func (s *Scheduler) Update(ctx context.Context, id uuid.UUID, in ScheduleInput) (*domain.Schedule, error) {
	sc, err := s.repo.GetSchedule(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Kind != "" {
		sc.Kind = in.Kind
	}
	if in.Frequency != "" {
		sc.Frequency = in.Frequency
	}
	if in.RecipientIDs != nil {
		sc.RecipientIDs = in.RecipientIDs
	}
	if in.Enabled != nil {
		sc.Enabled = *in.Enabled
	}
	sc.UpdatedAt = s.now()
	if err := s.validate(ctx, sc); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateSchedule(ctx, sc); err != nil {
		return nil, err
	}
	return sc, nil
}

func (s *Scheduler) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteSchedule(ctx, id)
}

func (s *Scheduler) Runs(ctx context.Context, branchID uuid.UUID, scheduleID *uuid.UUID, limit int) ([]domain.ScheduledRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListRuns(ctx, branchID, scheduleID, limit)
}

func (s *Scheduler) Download(ctx context.Context, id uuid.UUID) (*domain.ScheduledRun, error) {
	return s.repo.GetRun(ctx, id)
}

func (s *Scheduler) validate(ctx context.Context, sc *domain.Schedule) error {
	if err := sc.Normalize(); err != nil {
		return err
	}
	if s.recipients == nil {
		return nil
	}
	ok, err := s.recipients.Eligible(ctx, sc.BranchID)
	if err != nil {
		return err
	}
	for _, id := range sc.RecipientIDs {
		if !ok[id] {
			return shared.NewValidation("recipients must be active report users of the branch")
		}
	}
	return nil
}

// RunDue generates the last complete period of every enabled schedule once.
// Recipients who lost access since the schedule was saved are skipped.
func (s *Scheduler) RunDue(ctx context.Context, limit int) (int, error) {
	schedules, err := s.repo.ListEnabledSchedules(ctx, limit)
	if err != nil {
		return 0, err
	}
	generated := 0
	for _, sc := range schedules {
		ok, err := s.runOne(ctx, sc)
		if err != nil {
			return generated, err
		}
		if ok {
			generated++
		}
	}
	return generated, nil
}

func (s *Scheduler) runOne(ctx context.Context, sc domain.Schedule) (bool, error) {
	period, from, to := sc.Frequency.Period(s.now(), s.loc)
	exists, err := s.repo.RunExists(ctx, sc.ID, period)
	if err != nil || exists {
		return false, err
	}
	bctx := access.WithScope(ctx, access.ForBranch(sc.BranchID))
	res, err := s.gen.Run(bctx, sc.Kind, domain.Filter{BranchID: sc.BranchID, From: from, To: to, Limit: 1000})
	if err != nil {
		return false, err
	}
	csv, err := domain.BuildCSV(res.Columns, res.Rows)
	if err != nil {
		return false, err
	}
	run := domain.ScheduledRun{
		ID: uuid.New(), ScheduleID: sc.ID, BranchID: sc.BranchID, Kind: sc.Kind,
		Period: period, RowCount: len(res.Rows), Content: csv, CreatedAt: s.now(),
	}
	run.Filename = run.FileName()
	recipients, err := s.activeRecipients(ctx, sc)
	if err != nil {
		return false, err
	}
	inserted := false
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		ok, err := s.repo.InsertRun(ctx, &run)
		if err != nil || !ok {
			return err
		}
		inserted = true
		if err := s.repo.MarkScheduleRun(ctx, sc.ID, run.CreatedAt); err != nil {
			return err
		}
		if s.notifier == nil || len(recipients) == 0 {
			return nil
		}
		return s.notifier.ReportReady(ctx, run, recipients)
	})
	return inserted, err
}

func (s *Scheduler) activeRecipients(ctx context.Context, sc domain.Schedule) ([]uuid.UUID, error) {
	if s.recipients == nil {
		return sc.RecipientIDs, nil
	}
	ok, err := s.recipients.Eligible(ctx, sc.BranchID)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(sc.RecipientIDs))
	for _, id := range sc.RecipientIDs {
		if ok[id] {
			out = append(out, id)
		}
	}
	return out, nil
}
