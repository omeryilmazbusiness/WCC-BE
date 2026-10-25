package planupgrade

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	appsupport "github.com/wodi-crm/wodi-crm-be/internal/app/support"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domainsupport "github.com/wodi-crm/wodi-crm-be/internal/domain/support"
)

// Tenants loads the caller's company.
type Tenants interface {
	GetCompany(ctx context.Context, id uuid.UUID) (*domain.Company, error)
}

// Tickets is the platform help inbox (ISP).
type Tickets interface {
	Submit(ctx context.Context, a appsupport.Actor, d domainsupport.Draft) (*domainsupport.Request, error)
	FindOpenByCompanyPage(ctx context.Context, companyID uuid.UUID, page string) (*domainsupport.Request, error)
}

// People resolves the requester and platform operators.
type People interface {
	FindUserByID(ctx context.Context, id uuid.UUID) (*identity.User, error)
	ListPlatformAdmins(ctx context.Context) ([]identity.User, error)
}

// Alerts writes in-app notifications for platform admins.
type Alerts interface {
	Emit(ctx context.Context, in appnotification.EmitInput) (*notification.Notification, error)
}

type Actor struct {
	UserID    uuid.UUID
	BranchID  uuid.UUID
	IP        string
	UserAgent string
}

type Input struct {
	Target  domain.Plan
	Feature string
	Locale  string
}

type Result struct {
	RequestID uuid.UUID
	Number    int64
	Target    domain.Plan
	Duplicate bool
}

type Service struct {
	tenants Tenants
	tickets Tickets
	people  People
	alerts  Alerts
	log     *slog.Logger
}

func New(tenants Tenants, tickets Tickets, people People, alerts Alerts, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{tenants: tenants, tickets: tickets, people: people, alerts: alerts, log: log}
}

// Request opens (or returns) a platform ticket and pings every platform admin.
func (s *Service) Request(ctx context.Context, a Actor, in Input) (*Result, error) {
	sc, err := access.Require(ctx)
	if err != nil || sc.CompanyID == uuid.Nil {
		return nil, shared.NewForbidden("upgrade requests need a company")
	}
	if a.UserID == uuid.Nil || a.BranchID == uuid.Nil {
		return nil, shared.NewValidation("requester and branch are required")
	}
	c, err := s.tenants.GetCompany(ctx, sc.CompanyID)
	if err != nil {
		return nil, err
	}
	if !domain.CanUpgradeTo(c.Plan, in.Target) {
		err := shared.NewValidation("cannot upgrade to that plan")
		err.Details = map[string]any{"plan": c.Plan, "target": in.Target}
		return nil, err
	}
	page := domain.UpgradePage(in.Target)
	if existing, err := s.tickets.FindOpenByCompanyPage(ctx, c.ID, page); err != nil {
		return nil, err
	} else if existing != nil {
		return &Result{RequestID: existing.ID, Number: existing.Number, Target: in.Target, Duplicate: true}, nil
	}

	who, role, email := "a teammate", "", ""
	if s.people != nil {
		if u, err := s.people.FindUserByID(ctx, a.UserID); err == nil && u != nil {
			who, role, email = u.FullName, string(u.Role), u.Email
		}
	}
	msg := domain.ComposeUpgrade(in.Locale, who, email, role, c.NameEN, c.Plan, in.Target, in.Feature)
	req, err := s.tickets.Submit(ctx, appsupport.Actor{
		UserID: a.UserID, BranchID: a.BranchID, IP: a.IP, UserAgent: a.UserAgent,
	}, domainsupport.Draft{Title: msg.Title, Description: msg.Description, Page: msg.Page, Locale: msg.Locale})
	if err != nil {
		return nil, err
	}
	s.notifyAdmins(ctx, a.BranchID, c, in.Target, who, req)
	return &Result{RequestID: req.ID, Number: req.Number, Target: in.Target}, nil
}

func (s *Service) notifyAdmins(ctx context.Context, branchID uuid.UUID, c *domain.Company, to domain.Plan, who string, req *domainsupport.Request) {
	if s.people == nil || s.alerts == nil {
		return
	}
	admins, err := s.people.ListPlatformAdmins(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "plan upgrade: list admins", "err", err)
		return
	}
	title := "Plan upgrade requested"
	body := strings.TrimSpace(who) + " at " + c.NameEN + " asked for " + string(to) + "."
	id := req.ID
	companyID := c.ID
	for _, admin := range admins {
		if _, err := s.alerts.Emit(ctx, appnotification.EmitInput{
			BranchID: branchID, RecipientUserID: admin.ID,
			Kind: notification.KindPlanUpgrade, Title: title, Body: body,
			EntityType: "company", EntityID: &companyID,
			HrefHint: "/admin/settings/supportInbox",
			Meta:     map[string]any{"request_id": id.String(), "target": string(to)},
			Severity: notification.SeverityInfo,
		}); err != nil {
			s.log.WarnContext(ctx, "plan upgrade: notify admin", "admin", admin.ID, "err", err)
		}
	}
}
