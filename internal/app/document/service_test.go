package document_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/document"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memRepo struct {
	mu       sync.Mutex
	byID     map[uuid.UUID]*domain.Document
	policies map[uuid.UUID]*domain.Policy
}

func newMemRepo() *memRepo {
	return &memRepo{byID: map[uuid.UUID]*domain.Document{}, policies: map[uuid.UUID]*domain.Policy{}}
}

func (m *memRepo) Create(_ context.Context, d *domain.Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *d
	m.byID[d.ID] = &cp
	return nil
}
func (m *memRepo) Update(_ context.Context, d *domain.Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[d.ID]; !ok {
		return context.Canceled
	}
	cp := *d
	m.byID[d.ID] = &cp
	return nil
}
func (m *memRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byID[id]
	if !ok {
		return nil, context.Canceled
	}
	cp := *d
	return &cp, nil
}
func (m *memRepo) ListByRelated(_ context.Context, relatedType string, relatedID uuid.UUID) ([]domain.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Document
	for _, d := range m.byID {
		if d.RelatedType == relatedType && d.RelatedID == relatedID {
			out = append(out, *d)
		}
	}
	return out, nil
}
func (m *memRepo) ListExpiring(_ context.Context, onOrBefore time.Time, limit int) ([]domain.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Document
	for _, d := range m.byID {
		if d.ExpiresAt == nil {
			continue
		}
		if !d.ExpiresAt.After(onOrBefore) && (d.State == domain.StatusUploaded || d.State == domain.StatusSubmitted || d.State == domain.StatusApproved) {
			out = append(out, *d)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (m *memRepo) ListApprovedBySubjects(_ context.Context, subjects []domain.SubjectRef) ([]domain.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	set := map[string]bool{}
	for _, s := range subjects {
		set[s.RelatedType+":"+s.RelatedID.String()] = true
	}
	var out []domain.Document
	for _, d := range m.byID {
		if d.State != domain.StatusApproved {
			continue
		}
		if set[d.RelatedType+":"+d.RelatedID.String()] {
			out = append(out, *d)
		}
	}
	return out, nil
}
func (m *memRepo) CreatePolicy(_ context.Context, p *domain.Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	cp.Requirements = append([]domain.Requirement{}, p.Requirements...)
	m.policies[p.ID] = &cp
	return nil
}
func (m *memRepo) UpdatePolicy(_ context.Context, p *domain.Policy) error {
	return m.CreatePolicy(context.Background(), p)
}
func (m *memRepo) ReplaceRequirements(_ context.Context, policyID uuid.UUID, reqs []domain.Requirement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.policies[policyID]
	if !ok {
		return context.Canceled
	}
	p.Requirements = append([]domain.Requirement{}, reqs...)
	return nil
}
func (m *memRepo) FindPolicyByID(_ context.Context, id uuid.UUID) (*domain.Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.policies[id]
	if !ok {
		return nil, context.Canceled
	}
	cp := *p
	cp.Requirements = append([]domain.Requirement{}, p.Requirements...)
	return &cp, nil
}
func (m *memRepo) ListPolicies(_ context.Context, branchID uuid.UUID) ([]domain.Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Policy
	for _, p := range m.policies {
		if p.BranchID == branchID {
			cp := *p
			cp.Requirements = append([]domain.Requirement{}, p.Requirements...)
			out = append(out, cp)
		}
	}
	return out, nil
}
func (m *memRepo) FindActivePolicy(_ context.Context, branchID uuid.UUID, _ *uuid.UUID, _ string) (*domain.Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.policies {
		if p.BranchID == branchID && p.IsActive {
			cp := *p
			cp.Requirements = append([]domain.Requirement{}, p.Requirements...)
			return &cp, nil
		}
	}
	return nil, nil
}

type memStore struct{}

func (memStore) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	return "https://upload.example/put", nil
}
func (memStore) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "https://upload.example/get", nil
}

type memBookings struct {
	branchID, customerID, departureID uuid.UUID
	participants                      []uuid.UUID
}

func (m memBookings) BookingSubjects(context.Context, uuid.UUID) (uuid.UUID, uuid.UUID, uuid.UUID, []uuid.UUID, error) {
	return m.branchID, m.customerID, m.departureID, m.participants, nil
}
func (m memBookings) BookingsOnDeparture(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func TestApproveFlow(t *testing.T) {
	repo := newMemRepo()
	svc := appsvc.NewService(repo, memStore{}, tx.Nop{})
	branch := uuid.New()
	actor := uuid.New()
	related := uuid.New()

	presign, err := svc.PresignUpload(context.Background(), appsvc.PresignUploadInput{
		BranchID: branch, RelatedType: domain.RelatedBooking, RelatedID: related,
		Kind: domain.KindPassport, FileName: "pass.pdf", ContentType: "application/pdf", UploadedBy: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteUpload(context.Background(), appsvc.CompleteUploadInput{
		DocumentID: presign.DocumentID, SizeBytes: 2048, ActorID: actor,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(context.Background(), presign.DocumentID); err != nil {
		t.Fatal(err)
	}
	doc, err := svc.Approve(context.Background(), presign.DocumentID, actor, "looks good")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != domain.StatusApproved {
		t.Fatalf("got %s", doc.Status)
	}

	// checklist sees approved passport
	policyID := uuid.New()
	_ = repo.CreatePolicy(context.Background(), &domain.Policy{
		ID: policyID, BranchID: branch, Name: "Default", IsActive: true, CreatedAt: time.Now().UTC(),
		Requirements: []domain.Requirement{
			{ID: uuid.New(), PolicyID: policyID, Kind: domain.KindPassport, Required: true, Label: "Passport"},
			{ID: uuid.New(), PolicyID: policyID, Kind: domain.KindVisa, Required: true, Label: "Visa"},
		},
	})
	svc.SetBookingContext(memBookings{branchID: branch, customerID: uuid.New(), departureID: uuid.New()})
	cl, err := svc.Checklist(context.Background(), related)
	if err != nil {
		t.Fatal(err)
	}
	if len(cl.MissingRequired) != 1 || cl.MissingRequired[0] != domain.KindVisa {
		t.Fatalf("missing=%v", cl.MissingRequired)
	}
}
