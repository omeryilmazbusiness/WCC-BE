package revenuetarget

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/revenuetarget"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// RecoveryTaskCreator creates behind-pace recovery tasks once per day (ISP).
type RecoveryTaskCreator interface {
	EnsureTargetRecoveryTask(ctx context.Context, branchID, targetID, assigneeID uuid.UUID, label string, deficit int64) error
}

type CreateInput struct {
	BranchID     uuid.UUID
	OwnerID      *uuid.UUID
	TeamID       *uuid.UUID
	Label        string
	TargetAmount int64
	Currency     string
	Metric       domain.Metric
	ScopeType    domain.ScopeType
	CurveType    domain.CurveType
	PeriodStart  time.Time
	PeriodEnd    time.Time
	ActorID      uuid.UUID
}

// PatchInput is the HTTP-friendly update DTO (nil fields = leave unchanged).
type PatchInput struct {
	ID           uuid.UUID
	OwnerID      *uuid.UUID
	ClearOwner   bool
	TeamID       *uuid.UUID
	ClearTeam    bool
	Label        *string
	TargetAmount *int64
	Currency     *string
	Metric       *domain.Metric
	ScopeType    *domain.ScopeType
	CurveType    *domain.CurveType
	PeriodStart  *time.Time
	PeriodEnd    *time.Time
	ActorID      uuid.UUID
}

type Service struct {
	repo   domain.Repository
	tx     *tx.Manager
	audit  audit.Recorder
	engine domain.Engine
	tasks  RecoveryTaskCreator
}

func NewService(repo domain.Repository, txm *tx.Manager) *Service {
	return &Service{repo: repo, tx: txm, engine: domain.Engine{}}
}

func (s *Service) SetAuditor(a audit.Recorder)          { s.audit = a }
func (s *Service) SetTaskCreator(t RecoveryTaskCreator) { s.tasks = t }

func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Target, error) {
	label := strings.TrimSpace(in.Label)
	if label == "" {
		return nil, shared.NewValidation("label is required")
	}
	if in.TargetAmount <= 0 {
		return nil, shared.NewValidation("target_amount must be > 0")
	}
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	if in.PeriodEnd.Before(in.PeriodStart) {
		return nil, shared.NewValidation("period_end must be >= period_start")
	}
	metric := in.Metric
	if metric == "" {
		metric = domain.MetricCollected
	}
	if metric != domain.MetricCollected && metric != domain.MetricBooked {
		return nil, shared.NewValidation("metric must be collected or booked")
	}
	scope := in.ScopeType
	if scope == "" {
		if in.OwnerID != nil {
			scope = domain.ScopeEmployee
		} else {
			scope = domain.ScopeBranch
		}
	}
	curve := in.CurveType
	if curve == "" {
		curve = domain.CurveLinear
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "SAR"
	}
	now := time.Now().UTC()
	actor := in.ActorID
	t := &domain.Target{
		ID: uuid.New(), BranchID: in.BranchID, OwnerID: in.OwnerID, TeamID: in.TeamID,
		Label: label, TargetAmount: in.TargetAmount, Currency: currency,
		Metric: metric, ScopeType: scope, CurveType: curve,
		PeriodStart: truncateDate(in.PeriodStart), PeriodEnd: truncateDate(in.PeriodEnd),
		CreatedBy: &actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	s.auditTarget(ctx, in.ActorID, "revenue_target.created", t, nil)
	return t, nil
}

func (s *Service) Update(ctx context.Context, in PatchInput) (*domain.Target, error) {
	t, err := s.repo.Get(ctx, in.ID)
	if err != nil || t == nil {
		return nil, shared.NewNotFound("revenue_target")
	}
	beforeJSON, _ := json.Marshal(targetSnapshot(t))

	if in.ClearOwner {
		t.OwnerID = nil
	} else if in.OwnerID != nil {
		t.OwnerID = in.OwnerID
	}
	if in.ClearTeam {
		t.TeamID = nil
	} else if in.TeamID != nil {
		t.TeamID = in.TeamID
	}
	if in.Label != nil {
		t.Label = strings.TrimSpace(*in.Label)
		if t.Label == "" {
			return nil, shared.NewValidation("label is required")
		}
	}
	if in.TargetAmount != nil {
		if *in.TargetAmount <= 0 {
			return nil, shared.NewValidation("target_amount must be > 0")
		}
		t.TargetAmount = *in.TargetAmount
	}
	if in.Currency != nil {
		t.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if in.Metric != nil {
		if *in.Metric != domain.MetricCollected && *in.Metric != domain.MetricBooked {
			return nil, shared.NewValidation("metric must be collected or booked")
		}
		t.Metric = *in.Metric
	}
	if in.ScopeType != nil {
		t.ScopeType = *in.ScopeType
	}
	if in.CurveType != nil {
		t.CurveType = *in.CurveType
	}
	if in.PeriodStart != nil {
		t.PeriodStart = truncateDate(*in.PeriodStart)
	}
	if in.PeriodEnd != nil {
		t.PeriodEnd = truncateDate(*in.PeriodEnd)
	}
	if t.PeriodEnd.Before(t.PeriodStart) {
		return nil, shared.NewValidation("period_end must be >= period_start")
	}
	t.UpdatedAt = time.Now().UTC()

	afterJSON, _ := json.Marshal(targetSnapshot(t))
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Update(ctx, t); err != nil {
			return err
		}
		return s.repo.InsertRevision(ctx, &domain.Revision{
			ID: uuid.New(), TargetID: t.ID, ActorID: in.ActorID, Action: "revise",
			BeforeJSON: beforeJSON, AfterJSON: afterJSON, CreatedAt: time.Now().UTC(),
		})
	})
	if err != nil {
		return nil, err
	}
	s.auditTarget(ctx, in.ActorID, "revenue_target.updated", t, beforeJSON)
	return t, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Target, error) {
	t, err := s.repo.Get(ctx, id)
	if err != nil || t == nil {
		return nil, shared.NewNotFound("revenue_target")
	}
	return t, nil
}

func (s *Service) List(ctx context.Context, branchID uuid.UUID) ([]domain.Target, error) {
	return s.repo.List(ctx, branchID)
}

func (s *Service) ListWeights(ctx context.Context, targetID uuid.UUID) ([]domain.Weight, error) {
	t, err := s.repo.Get(ctx, targetID)
	if err != nil || t == nil {
		return nil, shared.NewNotFound("revenue_target")
	}
	_ = t
	return s.repo.ListWeights(ctx, targetID)
}

func (s *Service) SetWeights(ctx context.Context, targetID, actorID uuid.UUID, weights []domain.Weight) error {
	t, err := s.repo.Get(ctx, targetID)
	if err != nil || t == nil {
		return shared.NewNotFound("revenue_target")
	}
	if err := s.engine.ValidateWeights(t.CurveType, weights); err != nil {
		return shared.NewValidation(err.Error())
	}
	before, _ := json.Marshal(map[string]any{"weights": mustListWeights(ctx, s.repo, targetID)})
	if err := s.repo.ReplaceWeights(ctx, targetID, weights); err != nil {
		return err
	}
	after, _ := json.Marshal(map[string]any{"weights": weights})
	_ = s.repo.InsertRevision(ctx, &domain.Revision{
		ID: uuid.New(), TargetID: targetID, ActorID: actorID, Action: "set_weights",
		BeforeJSON: before, AfterJSON: after, CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (s *Service) SetShares(ctx context.Context, targetID, actorID uuid.UUID, shares []domain.Share) error {
	t, err := s.repo.Get(ctx, targetID)
	if err != nil || t == nil {
		return shared.NewNotFound("revenue_target")
	}
	_ = t
	if err := s.engine.ValidateShares(shares); err != nil {
		return shared.NewValidation(err.Error())
	}
	before, _ := json.Marshal(map[string]any{"shares": mustListShares(ctx, s.repo, targetID)})
	if err := s.repo.ReplaceShares(ctx, targetID, shares); err != nil {
		return err
	}
	after, _ := json.Marshal(map[string]any{"shares": shares})
	_ = s.repo.InsertRevision(ctx, &domain.Revision{
		ID: uuid.New(), TargetID: targetID, ActorID: actorID, Action: "set_shares",
		BeforeJSON: before, AfterJSON: after, CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (s *Service) Progress(ctx context.Context, id uuid.UUID) (*domain.Progress, error) {
	t, err := s.repo.Get(ctx, id)
	if err != nil || t == nil {
		return nil, shared.NewNotFound("revenue_target")
	}
	weights, err := s.repo.ListWeights(ctx, id)
	if err != nil {
		return nil, err
	}
	asOf := truncateDate(time.Now().UTC())
	from, to := t.PeriodStart, minTime(asOf, t.PeriodEnd)
	actual, err := s.repo.SumActual(ctx, t, from, to)
	if err != nil {
		return nil, err
	}
	calc := s.engine.Calculate(domain.CalcInput{
		TargetAmount: t.TargetAmount, Actual: actual,
		PeriodStart: t.PeriodStart, PeriodEnd: t.PeriodEnd, AsOf: asOf,
		Curve: t.CurveType, Weights: weights,
	})
	snap := &domain.Snapshot{
		TargetID: t.ID, AsOf: asOf, ActualAmount: actual,
		ExpectedToDate: calc.ExpectedToDate, Variance: calc.Variance,
		ProgressBps: calc.ProgressBps, PaceBps: calc.PaceBps,
		ForecastAmount: calc.ForecastAmount, Status: calc.Status,
		UpdatedAt: time.Now().UTC(),
	}
	_ = s.repo.UpsertSnapshot(ctx, snap)

	return &domain.Progress{
		TargetID: t.ID, Label: t.Label, Currency: t.Currency, Metric: t.Metric,
		ScopeType: t.ScopeType, CurveType: t.CurveType, TargetAmount: t.TargetAmount,
		ActualAmount: actual, ExpectedToDate: calc.ExpectedToDate, Variance: calc.Variance,
		ProgressBps: calc.ProgressBps, PaceBps: calc.PaceBps, ForecastAmount: calc.ForecastAmount,
		RequiredPaceDaily: calc.RequiredPaceDaily, Status: calc.Status,
		PeriodStart: t.PeriodStart.Format("2006-01-02"), PeriodEnd: t.PeriodEnd.Format("2006-01-02"),
		AsOf: asOf.Format("2006-01-02"),
	}, nil
}

func (s *Service) Contributions(ctx context.Context, id uuid.UUID) ([]domain.Contribution, error) {
	t, err := s.repo.Get(ctx, id)
	if err != nil || t == nil {
		return nil, shared.NewNotFound("revenue_target")
	}
	shares, err := s.repo.ListShares(ctx, id)
	if err != nil {
		return nil, err
	}
	asOf := truncateDate(time.Now().UTC())
	from, to := t.PeriodStart, minTime(asOf, t.PeriodEnd)
	byOwner, err := s.repo.SumActualByOwner(ctx, t, from, to)
	if err != nil {
		return nil, err
	}
	actualMap := map[uuid.UUID]domain.Contribution{}
	for _, c := range byOwner {
		actualMap[c.UserID] = c
	}
	if len(shares) == 0 {
		for i := range byOwner {
			byOwner[i].ShareAmount = 0
		}
		return byOwner, nil
	}
	out := make([]domain.Contribution, 0, len(shares))
	for i, sh := range shares {
		c := domain.Contribution{
			UserID: sh.UserID, UserName: sh.UserName, ShareBps: sh.ShareBps,
			ShareAmount: (t.TargetAmount * int64(sh.ShareBps)) / 10000,
			Rank:        i + 1,
		}
		if a, ok := actualMap[sh.UserID]; ok {
			c.ActualAmount = a.ActualAmount
			if a.UserName != "" {
				c.UserName = a.UserName
			}
		}
		out = append(out, c)
	}
	// Rank by actual desc
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].ActualAmount > out[i].ActualAmount {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

func (s *Service) Series(ctx context.Context, id uuid.UUID) ([]domain.SeriesPoint, error) {
	t, err := s.repo.Get(ctx, id)
	if err != nil || t == nil {
		return nil, shared.NewNotFound("revenue_target")
	}
	weights, err := s.repo.ListWeights(ctx, id)
	if err != nil {
		return nil, err
	}
	asOf := truncateDate(time.Now().UTC())
	from, to := t.PeriodStart, minTime(asOf, t.PeriodEnd)
	actual, err := s.repo.SumActual(ctx, t, from, to)
	if err != nil {
		return nil, err
	}
	return s.engine.CumulativeSeries(t.TargetAmount, actual, t.PeriodStart, t.PeriodEnd, asOf, t.CurveType, weights, nil), nil
}

func (s *Service) Sources(ctx context.Context, id uuid.UUID, limit int) ([]domain.SourceRow, error) {
	t, err := s.repo.Get(ctx, id)
	if err != nil || t == nil {
		return nil, shared.NewNotFound("revenue_target")
	}
	asOf := truncateDate(time.Now().UTC())
	from, to := t.PeriodStart, minTime(asOf, t.PeriodEnd)
	return s.repo.ListSources(ctx, t, from, to, limit)
}

func (s *Service) Revisions(ctx context.Context, id uuid.UUID, limit int) ([]domain.Revision, error) {
	t, err := s.repo.Get(ctx, id)
	if err != nil || t == nil {
		return nil, shared.NewNotFound("revenue_target")
	}
	_ = t
	return s.repo.ListRevisions(ctx, id, limit)
}

func (s *Service) Recompute(ctx context.Context, id uuid.UUID) (*domain.Progress, error) {
	return s.Progress(ctx, id)
}

func (s *Service) RecomputeBranch(ctx context.Context, branchID uuid.UUID) ([]domain.Progress, error) {
	targets, err := s.repo.ListTargetsForBranch(ctx, branchID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Progress, 0, len(targets))
	for i := range targets {
		p, err := s.Progress(ctx, targets[i].ID)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, nil
}

// CheckBehindAlerts creates recovery tasks for behind targets (once per as_of day).
func (s *Service) CheckBehindAlerts(ctx context.Context, branchID, actorID uuid.UUID) (int, error) {
	progresses, err := s.RecomputeBranch(ctx, branchID)
	if err != nil {
		return 0, err
	}
	if s.tasks == nil {
		return 0, nil
	}
	managers, err := s.repo.ManagerUserIDs(ctx, branchID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range progresses {
		if p.Status != domain.StatusBehind {
			continue
		}
		assignee := actorID
		if len(managers) > 0 {
			assignee = managers[0]
		}
		deficit := p.ExpectedToDate - p.ActualAmount
		if deficit < 0 {
			deficit = 0
		}
		if err := s.tasks.EnsureTargetRecoveryTask(ctx, branchID, p.TargetID, assignee, p.Label, deficit); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Service) auditTarget(ctx context.Context, actorID uuid.UUID, action string, t *domain.Target, before []byte) {
	if s.audit == nil || t == nil {
		return
	}
	id := t.ID
	after := targetSnapshot(t)
	in := audit.RecordInput{
		ActorID: actorID, Action: action, EntityType: "revenue_target", EntityID: &id,
		After: after,
	}
	if before != nil {
		var m map[string]any
		_ = json.Unmarshal(before, &m)
		in.Before = m
	}
	_ = s.audit.Record(ctx, in)
}

func targetSnapshot(t *domain.Target) map[string]any {
	m := map[string]any{
		"id": t.ID, "branch_id": t.BranchID, "label": t.Label,
		"target_amount": t.TargetAmount, "currency": t.Currency,
		"metric": t.Metric, "scope_type": t.ScopeType, "curve_type": t.CurveType,
		"period_start": t.PeriodStart.Format("2006-01-02"),
		"period_end":   t.PeriodEnd.Format("2006-01-02"),
	}
	if t.OwnerID != nil {
		m["owner_id"] = *t.OwnerID
	}
	if t.TeamID != nil {
		m["team_id"] = *t.TeamID
	}
	return m
}

func mustListWeights(ctx context.Context, repo domain.Repository, id uuid.UUID) []domain.Weight {
	w, _ := repo.ListWeights(ctx, id)
	return w
}

func mustListShares(ctx context.Context, repo domain.Repository, id uuid.UUID) []domain.Share {
	sh, _ := repo.ListShares(ctx, id)
	return sh
}

func truncateDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
