package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/bus"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memRepo struct{ items map[uuid.UUID]domain.Company }

func newMem() *memRepo { return &memRepo{items: map[uuid.UUID]domain.Company{}} }

func (m *memRepo) Create(_ context.Context, c *domain.Company) error {
	m.items[c.ID] = *c
	return nil
}
func (m *memRepo) Update(_ context.Context, c *domain.Company) error {
	if _, ok := m.items[c.ID]; !ok {
		return shared.NewNotFound("bus company")
	}
	m.items[c.ID] = *c
	return nil
}
func (m *memRepo) Find(_ context.Context, id uuid.UUID) (*domain.Company, error) {
	c, ok := m.items[id]
	if !ok {
		return nil, shared.NewNotFound("bus company")
	}
	return &c, nil
}
func (m *memRepo) List(_ context.Context, f domain.ListFilter) ([]domain.Company, error) {
	out := []domain.Company{}
	for _, c := range m.items {
		if c.BranchID != f.BranchID {
			continue
		}
		if f.ActiveOnly && !c.IsActive {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

type memAudit struct{ actions []string }

func (a *memAudit) Record(_ context.Context, in audit.RecordInput) error {
	a.actions = append(a.actions, in.Action)
	return nil
}

func fixture(t *testing.T) (*Service, *memAudit, uuid.UUID) {
	t.Helper()
	svc := NewService(newMem(), tx.Nop{})
	rec := &memAudit{}
	svc.SetAuditor(rec)
	svc.now = func() time.Time { return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC) }
	branch := uuid.New()
	c, err := svc.Create(context.Background(), branch, CompanyInput{
		Name: "Kamil Koç", Phone: "+90 212 111 22 33", City: "Istanbul", Country: "Turkey", VehicleClass: domain.ClassVIP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.VehicleClass != domain.ClassVIP || !c.IsActive || rec.actions[0] != "bus.created" {
		t.Fatalf("created: %+v actions=%v", c, rec.actions)
	}
	return svc, rec, branch
}

func TestCreateListUpdate(t *testing.T) {
	svc, rec, branch := fixture(t)
	rows, err := svc.List(context.Background(), domain.ListFilter{BranchID: branch})
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v n=%d", err, len(rows))
	}
	off := false
	upd, err := svc.Update(context.Background(), rows[0].ID, CompanyInput{
		Name: "Kamil Koç Anadolu", Phone: "+90 212 111 22 33", City: "Ankara", Country: "Turkey",
		VehicleClass: domain.ClassMixed, IsActive: &off,
	})
	if err != nil || upd.City != "Ankara" || upd.IsActive || rec.actions[1] != "bus.updated" {
		t.Fatalf("update: %+v err=%v actions=%v", upd, err, rec.actions)
	}
	got, err := svc.Get(context.Background(), rows[0].ID)
	if err != nil || got.Name != "Kamil Koç Anadolu" {
		t.Fatalf("get: %+v err=%v", got, err)
	}
}

func TestCreateRejectsInvalid(t *testing.T) {
	svc := NewService(newMem(), tx.Nop{})
	_, err := svc.Create(context.Background(), uuid.New(), CompanyInput{Name: "x", Email: "bad"})
	if !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation, got %v", err)
	}
	if _, err := svc.Create(context.Background(), uuid.Nil, CompanyInput{Name: "Metro"}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("nil branch: %v", err)
	}
}
