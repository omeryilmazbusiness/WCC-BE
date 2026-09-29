package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	httpadapter "github.com/wodi-crm/wodi-crm-be/internal/adapter/http"
	adminconfighttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/adminconfig"
	aihttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/ai"
	audithttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/audithttp"
	authhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/auth"
	bookinghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/booking"
	companyhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/company"
	customerhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/customer"
	dashboardhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/dashboard"
	documenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/document"
	extinthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/extint"
	filesynchttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/filesync"
	fxhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/health"
	importhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/importexport"
	inboxhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/inbox"
	leadhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/lead"
	notificationhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/notification"
	opshttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/ops"
	paymenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/payment"
	privacyhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/privacy"
	reporthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/report"
	targethttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/revenuetarget"
	roominghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/rooming"
	searchhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/search"
	setuphttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/setup"
	streamhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/stream"
	supplierhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/supplier"
	taskhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/task"
	pkghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/tourpackage"
	usershttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/users"
	visahttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/visa"
	pgbooking "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/booking"
	pgcustomer "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/customer"
	pgidentity "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/identity"
	pginbox "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/inbox"
	pglead "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/lead"
	pgrealtime "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/realtime"
	pgtask "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/task"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/queue"
	appai "github.com/wodi-crm/wodi-crm-be/internal/app/ai"
	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	appdashboard "github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	appinbox "github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	applead "github.com/wodi-crm/wodi-crm-be/internal/app/lead"
	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	appoutbox "github.com/wodi-crm/wodi-crm-be/internal/app/outbox"
	"github.com/wodi-crm/wodi-crm-be/internal/app/realtime"
	apprevenuetarget "github.com/wodi-crm/wodi-crm-be/internal/app/revenuetarget"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	domainai "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	taskdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// Realtime hub bounds per API process.
const (
	streamBuffer    = 32
	maxStreams      = 2000
	streamHeartbeat = 25 * time.Second
)

// Application is the API composition root (Dependency Injection).
type Application struct {
	Cfg    config.Config
	Log    *slog.Logger
	Pool   *pgxpool.Pool
	Server *http.Server
	Queue  *queue.Client
	mods   *modules
	stop   context.CancelFunc
}

func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*Application, error) {
	m, err := buildModules(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	hub := realtime.NewHub(streamBuffer, maxStreams)
	listenCtx, stop := context.WithCancel(context.Background())
	go pgrealtime.Listen(listenCtx, m.pool, hub, log)
	streamsClosing := make(chan struct{})

	handlers := httpadapter.Handlers{
		Health: health.Handler{
			DB:      m.pool,
			Queue:   m.queue,
			Version: cfg.App.Version,
			Env:     cfg.App.Env,
		},
		Auth:         authhttp.Handler{Svc: m.auth},
		Users:        usershttp.Handler{Svc: m.users},
		Audit:        audithttp.Handler{Svc: m.audit},
		Customer:     customerhttp.Handler{Svc: m.customers},
		Lead:         leadhttp.Handler{Svc: m.leads},
		Booking:      bookinghttp.Handler{Svc: m.bookings},
		Payment:      paymenthttp.Handler{Svc: m.payments, Promises: m.finance.Promises},
		FX:           fxhttp.Handler{Svc: m.finance.FX},
		FXLive:       liveHandler(m.finance.Live),
		Target:       targethttp.Handler{Svc: m.targets},
		Task:         taskhttp.Handler{Svc: m.tasks},
		Dashboard:    dashboardhttp.Handler{Svc: m.dashboard},
		Document:     documenthttp.Handler{Svc: m.documents},
		Visa:         visahttp.Handler{Svc: m.visas},
		Supplier:     supplierhttp.Handler{Svc: m.suppliers},
		Package:      pkghttp.Handler{Svc: m.packages},
		Ops:          opshttp.Handler{Queue: m.queue, Cleanup: m.retention, Backfill: m.backfill, Outbox: appoutbox.NewAdmin(m.outboxRepo)},
		Inbox:        inboxhttp.Handler{Svc: m.inbox},
		Import:       importhttp.Handler{Svc: m.imports},
		Notification: notificationhttp.Handler{Svc: m.notify},
		Report:       reporthttp.Handler{Svc: m.reports, Schedules: m.schedules},
		AI:           aihttp.Handler{Svc: m.ai},
		FileSync:     filesynchttp.Handler{Svc: m.fileSync},
		ExtInt:       extinthttp.Handler{Svc: m.extInt},
		AdminConfig:  adminconfighttp.Handler{Svc: m.adminConfig},
		Rooming:      roominghttp.Handler{Svc: m.rooming},
		Search:       searchhttp.Handler{Svc: m.search},
		Setup:        setuphttp.Handler{Svc: m.tenancy.setup},
		Company:      companyhttp.Handler{Svc: m.tenancy.companies},
		Privacy:      privacyhttp.Handler{Svc: m.privacy},
		Webhook:      newWebhookHandler(cfg, m.webhooks, m.limiter),
		Stream:       streamhttp.Handler{Hub: hub, Heartbeat: streamHeartbeat, Closing: streamsClosing},
	}

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      httpadapter.NewRouter(cfg, m.tokens, m.sessionCheck, m.tenancy.workspaces, handlers),
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}
	var closeStreams sync.Once
	srv.RegisterOnShutdown(func() { closeStreams.Do(func() { close(streamsClosing) }) })
	return &Application{Cfg: cfg, Log: log, Pool: m.pool, Server: srv, Queue: m.queue, mods: m, stop: stop}, nil
}

func (a *Application) Close() {
	if a.stop != nil {
		a.stop()
	}
	if a.mods != nil {
		a.mods.close()
	}
}

// leadBookingBridge adapts booking.Service to lead.BookingDraftCreator (DIP).
type leadBookingBridge struct {
	svc *appbooking.Service
}

func (b leadBookingBridge) CreateDraftFromLead(ctx context.Context, in applead.ConvertBookingInput) (uuid.UUID, error) {
	bk, err := b.svc.CreateDraft(ctx, appbooking.CreateInput{
		BranchID: in.BranchID, CustomerID: in.CustomerID, DepartureID: in.DepartureID,
		LeadID: &in.LeadID, PaxCount: in.PaxCount, TotalAmount: in.TotalAmount,
		Currency: in.Currency, OwnerID: in.OwnerID,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return bk.ID, nil
}

// bookingDocsBridge adapts booking repo for document checklists (ISP).
type bookingDocsBridge struct {
	repo *pgbooking.Repository
}

func (b bookingDocsBridge) BookingSubjects(ctx context.Context, bookingID uuid.UUID) (branchID, customerID, departureID uuid.UUID, participantIDs []uuid.UUID, err error) {
	bk, err := b.repo.FindByID(ctx, bookingID)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, nil, err
	}
	parts, err := b.repo.ListParticipants(ctx, bookingID)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, nil, err
	}
	ids := make([]uuid.UUID, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.ID)
	}
	return bk.BranchID, bk.CustomerID, bk.DepartureID, ids, nil
}

func (b bookingDocsBridge) BookingsOnDeparture(ctx context.Context, departureID uuid.UUID) ([]uuid.UUID, error) {
	items, err := b.repo.ListByDeparture(ctx, departureID)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(items))
	for _, bk := range items {
		out = append(out, bk.ID)
	}
	return out, nil
}

type inboxCustomerBridge struct {
	repo *pgcustomer.Repository
}

func (b inboxCustomerBridge) FindByPhone(ctx context.Context, phone string, branchID uuid.UUID) (*appinbox.MatchedCustomer, error) {
	c, err := b.repo.FindByPhone(ctx, phone, branchID)
	if err != nil || c == nil {
		return nil, nil
	}
	return &appinbox.MatchedCustomer{ID: c.ID, FullName: c.FullName}, nil
}

func (b inboxCustomerBridge) FindByEmail(ctx context.Context, email string, branchID uuid.UUID) (*appinbox.MatchedCustomer, error) {
	c, err := b.repo.FindByEmail(ctx, email, branchID)
	if err != nil || c == nil {
		return nil, nil
	}
	return &appinbox.MatchedCustomer{ID: c.ID, FullName: c.FullName}, nil
}

type inboxLeadBridge struct {
	svc *applead.Service
}

func (b inboxLeadBridge) CreateShell(ctx context.Context, in appinbox.LeadShellInput) (uuid.UUID, error) {
	owner := in.OwnerID
	if owner == uuid.Nil {
		owner = uuid.MustParse("22222222-2222-2222-2222-222222222203") // sales demo
	}
	actor := in.ActorID
	if actor == uuid.Nil {
		actor = owner
	}
	l, err := b.svc.Create(ctx, applead.CreateInput{
		BranchID: in.BranchID,
		FullName: in.FullName,
		Phone:    in.Phone,
		Source:   in.Source,
		OwnerID:  owner,
		ActorID:  actor,
		Notes:    "Created from inbox (no customer match)",
	})
	if err != nil {
		return uuid.Nil, err
	}
	return l.ID, nil
}

// taskConvBridge adapts inbox repo to task.ConversationReader (DIP).
type taskConvBridge struct {
	repo *pginbox.Repository
}

func (b taskConvBridge) GetConversation(ctx context.Context, id uuid.UUID) (*apptask.ConversationRef, error) {
	c, err := b.repo.GetConversation(ctx, id)
	if err != nil || c == nil {
		return nil, err
	}
	return &apptask.ConversationRef{
		ID: c.ID, BranchID: c.BranchID, OwnerID: c.OwnerID,
		LeadID: c.LeadID, CustomerID: c.CustomerID,
	}, nil
}

// identityDirectory adapts identity repo to notification.UserDirectory (DIP).
type identityDirectory struct {
	repo *pgidentity.Repository
}

func (d identityDirectory) ListActiveByRoles(ctx context.Context, branchID uuid.UUID, roles []string) ([]appnotification.UserRef, error) {
	active := true
	seen := map[uuid.UUID]struct{}{}
	var out []appnotification.UserRef
	for _, role := range roles {
		r := platformauth.Role(role)
		users, _, err := d.repo.ListUsers(ctx, identity.UserFilter{
			BranchID: &branchID, Role: &r, Active: &active, Limit: 100,
		})
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if _, ok := seen[u.ID]; ok {
				continue
			}
			seen[u.ID] = struct{}{}
			out = append(out, appnotification.UserRef{ID: u.ID, Email: u.Email, Role: string(u.Role)})
		}
	}
	return out, nil
}

func (d identityDirectory) FindByID(ctx context.Context, id uuid.UUID) (*appnotification.UserRef, error) {
	u, err := d.repo.FindUserByID(ctx, id)
	if err != nil || u == nil {
		return nil, err
	}
	return &appnotification.UserRef{ID: u.ID, Email: u.Email, Role: string(u.Role)}, nil
}

// --- Epic 15 AI bridges (DIP / ISP) ---

type aiDashBridge struct {
	dash *appdashboard.Service
}

func (b aiDashBridge) Facts(ctx context.Context, branchID uuid.UUID) (*appai.DashboardFacts, error) {
	bid := branchID
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -30)
	kpi, err := b.dash.KPIs(ctx, &bid, from, to)
	if err != nil {
		return nil, err
	}
	att, _ := b.dash.Attention(ctx, &bid, 8)
	titles := make([]string, 0, len(att))
	for _, a := range att {
		titles = append(titles, a.Title)
	}
	tp, _ := b.dash.MyTarget(ctx, branchID, nil)
	f := &appai.DashboardFacts{
		LeadsOpen: kpi.LeadsOpen, TasksOverdue: kpi.TasksOverdue,
		BookingsUnpaid: kpi.BookingsUnpaid, MissingDocs: kpi.MissingDocs,
		Attention: titles,
	}
	if tp != nil {
		f.TargetStatus = tp.Status
		f.TargetLabel = tp.Label
		f.CollectedAmt = tp.ActualAmount
	}
	return f, nil
}

type aiInboxBridge struct {
	repo *pginbox.Repository
}

func (b aiInboxBridge) Messages(ctx context.Context, conversationID, branchID uuid.UUID, limit int) (string, []string, error) {
	c, err := b.repo.GetConversation(ctx, conversationID)
	if err != nil || c == nil {
		return "", nil, shared.NewNotFound("conversation")
	}
	if c.BranchID != branchID {
		return "", nil, shared.NewForbidden("conversation branch mismatch")
	}
	msgs, err := b.repo.ListMessages(ctx, conversationID, limit)
	if err != nil {
		return "", nil, err
	}
	lines := make([]string, 0, len(msgs))
	for _, m := range msgs {
		lines = append(lines, string(m.Direction)+": "+m.Body)
	}
	return c.Subject, lines, nil
}

type aiLeadBridge struct {
	repo  *pglead.Repository
	tasks *pgtask.Repository
}

func (b aiLeadBridge) LoadSignals(ctx context.Context, leadID, branchID uuid.UUID) (domainai.LeadSignals, string, error) {
	l, err := b.repo.FindByID(ctx, leadID)
	if err != nil || l == nil {
		return domainai.LeadSignals{}, "", shared.NewNotFound("lead")
	}
	if l.BranchID != branchID {
		return domainai.LeadSignals{}, "", shared.NewForbidden("lead branch mismatch")
	}
	now := time.Now().UTC()
	sig := domainai.LeadSignals{
		Stage: string(l.Stage), NoFollowUp: l.NoFollowUp,
		HoursSinceTouch: domainai.HoursSince(&l.UpdatedAt, now),
		Source:          l.Source,
	}
	if b.tasks != nil {
		items, _, _ := b.tasks.List(ctx, taskdomain.ListFilter{
			BranchID: &branchID, RelatedType: "lead", RelatedID: &leadID, Limit: 20,
		})
		for _, t := range items {
			if t.Status == taskdomain.StatusOpen || t.Status == taskdomain.StatusInProgress {
				sig.HasOpenTask = true
				if t.DueAt != nil && t.DueAt.Before(now) {
					sig.OverdueTask = true
				}
			}
		}
	}
	return sig, l.FullName, nil
}

type aiTargetBridge struct {
	svc *apprevenuetarget.Service
}

func (b aiTargetBridge) LoadProgress(ctx context.Context, targetID, branchID uuid.UUID) (string, int64, int64, int64, string, error) {
	t, err := b.svc.Get(ctx, targetID)
	if err != nil || t == nil {
		return "", 0, 0, 0, "", shared.NewNotFound("revenue_target")
	}
	if t.BranchID != branchID {
		return "", 0, 0, 0, "", shared.NewForbidden("target branch mismatch")
	}
	p, err := b.svc.Progress(ctx, targetID)
	if err != nil || p == nil {
		return t.Label, t.TargetAmount, 0, 0, "unknown", nil
	}
	return p.Label, p.TargetAmount, p.ActualAmount, p.ExpectedToDate, string(p.Status), nil
}
