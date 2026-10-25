package planupgrade

import (
	"context"
	"testing"

	"github.com/google/uuid"

	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	appsupport "github.com/wodi-crm/wodi-crm-be/internal/app/support"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domainsupport "github.com/wodi-crm/wodi-crm-be/internal/domain/support"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type memTenants struct{ c *domain.Company }

func (m memTenants) GetCompany(context.Context, uuid.UUID) (*domain.Company, error) {
	if m.c == nil {
		return nil, shared.NewNotFound("company")
	}
	cp := *m.c
	return &cp, nil
}

type memTickets struct {
	open    *domainsupport.Request
	created *domainsupport.Request
}

func (m *memTickets) Submit(_ context.Context, _ appsupport.Actor, d domainsupport.Draft) (*domainsupport.Request, error) {
	r := &domainsupport.Request{ID: uuid.New(), Number: 7, Title: d.Title, Description: d.Description, Page: d.Page, Status: domainsupport.StatusOpen}
	m.created = r
	return r, nil
}

func (m *memTickets) FindOpenByCompanyPage(context.Context, uuid.UUID, string) (*domainsupport.Request, error) {
	if m.open == nil {
		return nil, nil
	}
	cp := *m.open
	return &cp, nil
}

type memPeople struct {
	user   *identity.User
	admins []identity.User
}

func (m memPeople) FindUserByID(context.Context, uuid.UUID) (*identity.User, error) {
	return m.user, nil
}
func (m memPeople) ListPlatformAdmins(context.Context) ([]identity.User, error) {
	return m.admins, nil
}

type memAlerts struct{ n int }

func (m *memAlerts) Emit(context.Context, appnotification.EmitInput) (*notification.Notification, error) {
	m.n++
	return &notification.Notification{}, nil
}

func scoped(company, branch, user uuid.UUID) context.Context {
	return access.WithScope(context.Background(), access.Scope{
		Level: access.LevelCompany, CompanyID: company, BranchID: branch, UserID: user,
	})
}

func TestRequestOpensTicketAndPingsAdmins(t *testing.T) {
	companyID, branchID, userID, adminID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tickets, alerts := &memTickets{}, &memAlerts{}
	svc := New(
		memTenants{c: &domain.Company{ID: companyID, NameEN: "Al Noor", Plan: domain.PlanStandard}},
		tickets,
		memPeople{
			user:   &identity.User{ID: userID, FullName: "Sara", Email: "sara@alnoor.test", Role: platformauth.RoleGM},
			admins: []identity.User{{ID: adminID}},
		},
		alerts, nil,
	)
	out, err := svc.Request(scoped(companyID, branchID, userID), Actor{UserID: userID, BranchID: branchID}, Input{
		Target: domain.PlanPremium, Feature: "Finance", Locale: "en",
	})
	if err != nil || out.Duplicate || tickets.created == nil {
		t.Fatalf("%v %+v", err, out)
	}
	if alerts.n != 1 {
		t.Fatalf("admin pings %d", alerts.n)
	}
	if tickets.created.Page != "/upgrade/premium" {
		t.Fatal(tickets.created.Page)
	}
}

func TestRequestDedupesOpenTicket(t *testing.T) {
	companyID := uuid.New()
	existing := &domainsupport.Request{ID: uuid.New(), Number: 3}
	tickets := &memTickets{open: existing}
	svc := New(memTenants{c: &domain.Company{ID: companyID, Plan: domain.PlanStandard}}, tickets, memPeople{}, &memAlerts{}, nil)
	out, err := svc.Request(scoped(companyID, uuid.New(), uuid.New()), Actor{UserID: uuid.New(), BranchID: uuid.New()}, Input{Target: domain.PlanPremium})
	if err != nil || !out.Duplicate || out.Number != 3 || tickets.created != nil {
		t.Fatalf("%v %+v", err, out)
	}
}

func TestRequestRejectsDowngrade(t *testing.T) {
	companyID := uuid.New()
	svc := New(memTenants{c: &domain.Company{ID: companyID, Plan: domain.PlanUltimate}}, &memTickets{}, nil, nil, nil)
	_, err := svc.Request(scoped(companyID, uuid.New(), uuid.New()), Actor{UserID: uuid.New(), BranchID: uuid.New()}, Input{Target: domain.PlanPremium})
	if err == nil {
		t.Fatal("ultimate cannot request premium")
	}
}
