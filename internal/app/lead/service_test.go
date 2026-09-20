package lead_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	applead "github.com/wodi-crm/wodi-crm-be/internal/app/lead"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memRepo struct {
	leads   map[uuid.UUID]*domain.Lead
	history []domain.StageHistory
}

func newMem() *memRepo {
	return &memRepo{leads: map[uuid.UUID]*domain.Lead{}}
}

func (m *memRepo) Create(_ context.Context, l *domain.Lead) error {
	cp := *l
	m.leads[l.ID] = &cp
	return nil
}
func (m *memRepo) Update(_ context.Context, l *domain.Lead) error {
	cp := *l
	m.leads[l.ID] = &cp
	return nil
}
func (m *memRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Lead, error) {
	l, ok := m.leads[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *l
	return &cp, nil
}
func (m *memRepo) List(_ context.Context, _ domain.ListFilter) ([]domain.Lead, int, error) {
	out := make([]domain.Lead, 0, len(m.leads))
	for _, l := range m.leads {
		out = append(out, *l)
	}
	return out, len(out), nil
}
func (m *memRepo) AppendStageHistory(_ context.Context, h *domain.StageHistory) error {
	m.history = append(m.history, *h)
	return nil
}
func (m *memRepo) ListStageHistory(_ context.Context, leadID uuid.UUID) ([]domain.StageHistory, error) {
	var out []domain.StageHistory
	for _, h := range m.history {
		if h.LeadID == leadID {
			out = append(out, h)
		}
	}
	return out, nil
}
func (m *memRepo) Analytics(_ context.Context, _ uuid.UUID) (*domain.Analytics, error) {
	return &domain.Analytics{Total: len(m.leads)}, nil
}

func TestCreateAssignLostAndNoFollowUp(t *testing.T) {
	repo := newMem()
	svc := applead.NewService(repo, tx.Nop{}, events.NewBus(nil))

	branch := uuid.New()
	owner := uuid.New()
	actor := uuid.New()
	l, err := svc.Create(context.Background(), applead.CreateInput{
		BranchID: branch, FullName: "Test Lead", Phone: "+966500000099",
		OwnerID: owner, ActorID: actor, Source: "WhatsApp",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if l.Stage != domain.StageNew {
		t.Fatalf("stage=%s", l.Stage)
	}
	if len(repo.history) != 1 {
		t.Fatalf("history=%d", len(repo.history))
	}

	other := uuid.New()
	assigned, err := svc.Assign(context.Background(), applead.AssignInput{
		LeadIDs: []uuid.UUID{l.ID}, OwnerID: other, ActorID: actor,
	})
	if err != nil || len(assigned) != 1 || assigned[0].OwnerID != other {
		t.Fatalf("assign: %v %#v", err, assigned)
	}

	_, err = svc.ChangeStage(context.Background(), applead.ChangeStageInput{
		LeadID: l.ID, To: domain.StageLost, ActorID: actor,
	})
	if err == nil {
		t.Fatal("expected lost reason validation")
	}
	var app *shared.AppError
	if !errors.As(err, &app) {
		t.Fatalf("want AppError, got %v", err)
	}

	updated, err := svc.ChangeStage(context.Background(), applead.ChangeStageInput{
		LeadID: l.ID, To: domain.StageLost, LostReasonCode: domain.LostPrice,
		LostReasonNote: "too expensive", ActorID: actor,
	})
	if err != nil {
		t.Fatalf("lost: %v", err)
	}
	if updated.LostReasonCode != domain.LostPrice || updated.LostReason != "too expensive" {
		t.Fatalf("lost fields: %+v", updated)
	}

	// reopen path not allowed from lost — create fresh for no_follow_up
	open, err := svc.Create(context.Background(), applead.CreateInput{
		BranchID: branch, FullName: "Open", Phone: "+966500000088",
		OwnerID: owner, ActorID: actor,
	})
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	flagged, err := svc.SetNoFollowUp(context.Background(), applead.SetNoFollowUpInput{
		LeadID: open.ID, NoFollowUp: true, ActorID: actor,
	})
	if err != nil || !flagged.NoFollowUp {
		t.Fatalf("nofollow: %v %+v", err, flagged)
	}
}

type fakeBooking struct{ id uuid.UUID }

func (f fakeBooking) CreateDraftFromLead(_ context.Context, _ applead.ConvertBookingInput) (uuid.UUID, error) {
	return f.id, nil
}

func TestConvertRequiresCustomer(t *testing.T) {
	repo := newMem()
	svc := applead.NewService(repo, tx.Nop{}, events.NewBus(nil))
	svc.SetBookingCreator(fakeBooking{id: uuid.New()})

	l, err := svc.Create(context.Background(), applead.CreateInput{
		BranchID: uuid.New(), FullName: "A", Phone: "+966511",
		OwnerID: uuid.New(), ActorID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Walk to proposal so convert can win
	for _, st := range []domain.Stage{domain.StageContacted, domain.StageQualified, domain.StageProposal} {
		l, err = svc.ChangeStage(context.Background(), applead.ChangeStageInput{
			LeadID: l.ID, To: st, ActorID: uuid.New(),
		})
		if err != nil {
			t.Fatalf("stage %s: %v", st, err)
		}
	}
	_, err = svc.Convert(context.Background(), applead.ConvertInput{
		LeadID: l.ID, DepartureID: uuid.New(), PaxCount: 2, ActorID: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected customer required")
	}
}
