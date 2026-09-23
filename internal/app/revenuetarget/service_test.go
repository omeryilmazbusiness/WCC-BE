package revenuetarget_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/revenuetarget"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/revenuetarget"
)

type memRepo struct {
	targets  map[uuid.UUID]*domain.Target
	weights  map[uuid.UUID][]domain.Weight
	shares   map[uuid.UUID][]domain.Share
	snaps    map[string]*domain.Snapshot
	actual   int64
	byOwner  []domain.Contribution
}

func newMemRepo() *memRepo {
	return &memRepo{
		targets: map[uuid.UUID]*domain.Target{},
		weights: map[uuid.UUID][]domain.Weight{},
		shares:  map[uuid.UUID][]domain.Share{},
		snaps:   map[string]*domain.Snapshot{},
	}
}

func (m *memRepo) Create(ctx context.Context, t *domain.Target) error {
	cp := *t
	m.targets[t.ID] = &cp
	return nil
}
func (m *memRepo) Update(ctx context.Context, t *domain.Target) error {
	cp := *t
	m.targets[t.ID] = &cp
	return nil
}
func (m *memRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Target, error) {
	t := m.targets[id]
	if t == nil {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}
func (m *memRepo) List(ctx context.Context, branchID uuid.UUID) ([]domain.Target, error) {
	return m.ListTargetsForBranch(ctx, branchID)
}
func (m *memRepo) ListTargetsForBranch(ctx context.Context, branchID uuid.UUID) ([]domain.Target, error) {
	var out []domain.Target
	for _, t := range m.targets {
		if t.BranchID == branchID {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (m *memRepo) ReplaceWeights(ctx context.Context, targetID uuid.UUID, weights []domain.Weight) error {
	m.weights[targetID] = append([]domain.Weight(nil), weights...)
	return nil
}
func (m *memRepo) ListWeights(ctx context.Context, targetID uuid.UUID) ([]domain.Weight, error) {
	return append([]domain.Weight(nil), m.weights[targetID]...), nil
}
func (m *memRepo) ReplaceShares(ctx context.Context, targetID uuid.UUID, shares []domain.Share) error {
	m.shares[targetID] = append([]domain.Share(nil), shares...)
	return nil
}
func (m *memRepo) ListShares(ctx context.Context, targetID uuid.UUID) ([]domain.Share, error) {
	return append([]domain.Share(nil), m.shares[targetID]...), nil
}
func (m *memRepo) UpsertSnapshot(ctx context.Context, s *domain.Snapshot) error {
	key := s.TargetID.String() + ":" + s.AsOf.Format("2006-01-02")
	cp := *s
	m.snaps[key] = &cp
	return nil
}
func (m *memRepo) LatestSnapshot(ctx context.Context, targetID uuid.UUID) (*domain.Snapshot, error) {
	return nil, nil
}
func (m *memRepo) InsertRevision(ctx context.Context, r *domain.Revision) error { return nil }
func (m *memRepo) ListRevisions(ctx context.Context, targetID uuid.UUID, limit int) ([]domain.Revision, error) {
	return nil, nil
}
func (m *memRepo) SumActual(ctx context.Context, t *domain.Target, from, to time.Time) (int64, error) {
	return m.actual, nil
}
func (m *memRepo) SumActualByOwner(ctx context.Context, t *domain.Target, from, to time.Time) ([]domain.Contribution, error) {
	return m.byOwner, nil
}
func (m *memRepo) ListSources(ctx context.Context, t *domain.Target, from, to time.Time, limit int) ([]domain.SourceRow, error) {
	return nil, nil
}
func (m *memRepo) ManagerUserIDs(ctx context.Context, branchID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func TestProgressCalculatesAndSnapshots(t *testing.T) {
	repo := newMemRepo()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	repo.targets[id] = &domain.Target{
		ID: id, BranchID: uuid.New(), Label: "Jan", TargetAmount: 10000,
		Currency: "SAR", Metric: domain.MetricCollected, ScopeType: domain.ScopeBranch,
		CurveType: domain.CurveLinear, PeriodStart: start, PeriodEnd: end,
	}
	repo.actual = 6000

	svc := appsvc.NewService(repo, nil)
	// Progress uses engine only; tx unused for Progress path
	p, err := svc.Progress(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.ActualAmount != 6000 {
		t.Fatalf("actual=%d", p.ActualAmount)
	}
	if p.Status == "" || p.Status == domain.StatusPlaceholder {
		t.Fatalf("status=%s", p.Status)
	}
	if len(repo.snaps) != 1 {
		t.Fatalf("expected snapshot upsert, got %d", len(repo.snaps))
	}
}

func TestSetSharesValidation(t *testing.T) {
	repo := newMemRepo()
	id := uuid.New()
	repo.targets[id] = &domain.Target{ID: id, CurveType: domain.CurveLinear, TargetAmount: 1}
	svc := appsvc.NewService(repo, nil)
	u1, u2 := uuid.New(), uuid.New()
	err := svc.SetShares(context.Background(), id, uuid.New(), []domain.Share{
		{UserID: u1, ShareBps: 4000}, {UserID: u2, ShareBps: 4000},
	})
	if err == nil {
		t.Fatal("expected validation error for share sum")
	}
}
