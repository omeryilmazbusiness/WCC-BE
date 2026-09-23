package importexport

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/importexport"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// MemRepo is an in-memory importexport.Repository for unit tests.
type MemRepo struct {
	mu        sync.Mutex
	jobs      map[uuid.UUID]*domain.ImportJob
	errors    map[uuid.UUID][]domain.RowError
	templates map[uuid.UUID]*domain.MappingTemplate
}

func NewMemRepo() *MemRepo {
	return &MemRepo{
		jobs:      make(map[uuid.UUID]*domain.ImportJob),
		errors:    make(map[uuid.UUID][]domain.RowError),
		templates: make(map[uuid.UUID]*domain.MappingTemplate),
	}
}

func (m *MemRepo) CreateJob(_ context.Context, j *domain.ImportJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *j
	if cp.Mapping == nil {
		cp.Mapping = map[string]string{}
	}
	m.jobs[j.ID] = &cp
	return nil
}

func (m *MemRepo) UpdateJob(_ context.Context, j *domain.ImportJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[j.ID]; !ok {
		return shared.NewNotFound("import_job")
	}
	cp := *j
	m.jobs[j.ID] = &cp
	return nil
}

func (m *MemRepo) GetJob(_ context.Context, id uuid.UUID) (*domain.ImportJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, nil
	}
	cp := *j
	if len(j.FileBytes) > 0 {
		cp.FileBytes = append([]byte(nil), j.FileBytes...)
	}
	return &cp, nil
}

func (m *MemRepo) ListJobs(_ context.Context, branchID uuid.UUID, limit int) ([]domain.ImportJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.ImportJob
	for _, j := range m.jobs {
		if j.BranchID == branchID {
			cp := *j
			cp.FileBytes = nil
			out = append(out, cp)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemRepo) ReplaceRowErrors(_ context.Context, jobID uuid.UUID, errs []domain.RowError) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]domain.RowError, len(errs))
	copy(cp, errs)
	m.errors[jobID] = cp
	return nil
}

func (m *MemRepo) ListRowErrors(_ context.Context, jobID uuid.UUID) ([]domain.RowError, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.RowError(nil), m.errors[jobID]...), nil
}

func (m *MemRepo) CreateTemplate(_ context.Context, t *domain.MappingTemplate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	m.templates[t.ID] = &cp
	return nil
}

func (m *MemRepo) ListTemplates(_ context.Context, branchID uuid.UUID, entityType domain.EntityType) ([]domain.MappingTemplate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.MappingTemplate
	for _, t := range m.templates {
		if t.BranchID != branchID {
			continue
		}
		if entityType != "" && t.EntityType != entityType {
			continue
		}
		out = append(out, *t)
	}
	return out, nil
}

func (m *MemRepo) DeleteTemplate(_ context.Context, id, branchID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.templates[id]
	if !ok || t.BranchID != branchID {
		return shared.NewNotFound("import_template")
	}
	delete(m.templates, id)
	return nil
}

func (m *MemRepo) ExportCustomers(context.Context, uuid.UUID, int) ([]domain.ExportRow, error) {
	return nil, nil
}
func (m *MemRepo) ExportBookings(context.Context, uuid.UUID, int) ([]domain.ExportRow, error) {
	return nil, nil
}
func (m *MemRepo) ExportPayments(context.Context, uuid.UUID, int) ([]domain.ExportRow, error) {
	return nil, nil
}
func (m *MemRepo) ExportDepartures(context.Context, uuid.UUID, int) ([]domain.ExportRow, error) {
	return nil, nil
}

// MemCustomers implements customer.Repository for import Process unit tests.
type MemCustomers struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*customer.Customer
}

func NewMemCustomers() *MemCustomers {
	return &MemCustomers{byID: make(map[uuid.UUID]*customer.Customer)}
}

func (m *MemCustomers) Create(_ context.Context, c *customer.Customer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *c
	m.byID[c.ID] = &cp
	return nil
}

func (m *MemCustomers) Update(_ context.Context, c *customer.Customer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[c.ID]; !ok {
		return shared.NewNotFound("customer")
	}
	cp := *c
	m.byID[c.ID] = &cp
	return nil
}

func (m *MemCustomers) FindByID(_ context.Context, id uuid.UUID) (*customer.Customer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

func (m *MemCustomers) FindByPhone(_ context.Context, phone string, branchID uuid.UUID) (*customer.Customer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	norm := shared.NormalizePhone(phone)
	for _, c := range m.byID {
		if c.BranchID == branchID && shared.NormalizePhone(c.Phone) == norm && c.IsActive && c.MergedIntoID == nil {
			cp := *c
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *MemCustomers) Search(context.Context, customer.SearchFilter) ([]customer.Customer, int, error) {
	return nil, 0, nil
}
func (m *MemCustomers) FindByEmail(context.Context, string, uuid.UUID) (*customer.Customer, error) {
	return nil, nil
}
func (m *MemCustomers) FindByPassport(context.Context, string, uuid.UUID) (*customer.Customer, error) {
	return nil, nil
}
func (m *MemCustomers) FindNameCandidates(context.Context, string, uuid.UUID, int) ([]customer.Customer, error) {
	return nil, nil
}
func (m *MemCustomers) MarkMerged(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (m *MemCustomers) ReassignLeads(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (m *MemCustomers) ReassignBookings(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (m *MemCustomers) ListCompanions(context.Context, uuid.UUID) ([]customer.CompanionLink, error) {
	return nil, nil
}
func (m *MemCustomers) LinkCompanion(context.Context, *customer.CompanionLink) error { return nil }
func (m *MemCustomers) UnlinkCompanion(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (m *MemCustomers) ListTimeline(context.Context, uuid.UUID, int) ([]customer.TimelineItem, error) {
	return nil, nil
}
