package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	httpadapter "github.com/wodi-crm/wodi-crm-be/internal/adapter/http"
	authhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/auth"
	audithttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/audithttp"
	bookinghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/booking"
	customerhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/customer"
	dashboardhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/dashboard"
	documenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/document"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/health"
	inboxhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/inbox"
	importhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/importexport"
	leadhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/lead"
	opshttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/ops"
	paymenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/payment"
	targethttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/revenuetarget"
	supplierhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/supplier"
	taskhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/task"
	pkghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/tourpackage"
	usershttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/users"
	visahttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/visa"
	notificationhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/notification"
	webhookhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/webhook"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/email"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/instagram"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/stub"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/whatsapp"
	domaininbox "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	pgaudit "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/audit"
	pgbooking "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/booking"
	pgcustomer "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/customer"
	pgdocument "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/document"
	pgidentity "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/identity"
	pginbox "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/inbox"
	pgimportexport "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/importexport"
	pglead "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/lead"
	pgpayment "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/payment"
	pgrevenuetarget "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/revenuetarget"
	pgsupplier "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/supplier"
	pgtask "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/task"
	pkgpg "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/tourpackage"
	pgvisa "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/visa"
	pgnotification "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/notification"
	pgdash "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/queue"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/storage"
	appaudit "github.com/wodi-crm/wodi-crm-be/internal/app/audit"
	appauth "github.com/wodi-crm/wodi-crm-be/internal/app/auth"
	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	appcustomer "github.com/wodi-crm/wodi-crm-be/internal/app/customer"
	appdashboard "github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	appdocument "github.com/wodi-crm/wodi-crm-be/internal/app/document"
	appinbox "github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	appimportexport "github.com/wodi-crm/wodi-crm-be/internal/app/importexport"
	applead "github.com/wodi-crm/wodi-crm-be/internal/app/lead"
	apppayment "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	apprevenuetarget "github.com/wodi-crm/wodi-crm-be/internal/app/revenuetarget"
	appsupplier "github.com/wodi-crm/wodi-crm-be/internal/app/supplier"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	apppkg "github.com/wodi-crm/wodi-crm-be/internal/app/tourpackage"
	appuser "github.com/wodi-crm/wodi-crm-be/internal/app/useradmin"
	appvisa "github.com/wodi-crm/wodi-crm-be/internal/app/visa"
	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/database"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Application is the composition root (Dependency Injection).
type Application struct {
	Cfg    config.Config
	Log    *slog.Logger
	Pool   *pgxpool.Pool
	Server *http.Server
	Queue  *queue.AsynqClient
}

func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*Application, error) {
	pool, err := database.NewPool(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}

	txm := tx.NewManager(pool)
	bus := events.NewBus(log)
	tokens := platformauth.NewTokenService(cfg.Auth)
	store := storage.NewMinIO(cfg.Storage)
	q := queue.NewAsynqClient(cfg.Redis, log)
	mfa := platformauth.NewPolicyMFA() // enable per-user via mfa_enabled flag

	identityRepo := pgidentity.NewRepository(pool)
	auditRepo := pgaudit.NewRepository(pool)
	customerRepo := pgcustomer.NewRepository(pool)
	leadRepo := pglead.NewRepository(pool)
	bookingRepo := pgbooking.NewRepository(pool)
	paymentRepo := pgpayment.NewRepository(pool)
	taskRepo := pgtask.NewRepository(pool)
	pkgRepo := pkgpg.NewRepository(pool)
	docRepo := pgdocument.NewRepository(pool)
	visaRepo := pgvisa.NewRepository(pool)
	supplierRepo := pgsupplier.NewRepository(pool)
	dashAgg := pgdash.NewDashboardAggregator(pool)
	inboxRepo := pginbox.NewRepository(pool)
	providers := integration.NewRegistry(
		stub.New(),
		whatsapp.New(),
		instagram.New(),
		email.New(),
		stub.NewNamed(domaininbox.ChannelFacebook, "ok", ""),
		stub.NewNamed(domaininbox.ChannelGmail, "ok", ""),
	)

	auditSvc := appaudit.NewService(auditRepo)
	authSvc := appauth.NewService(identityRepo, auditSvc, tokens, txm, mfa)
	userSvc := appuser.NewService(identityRepo, auditSvc, txm)
	customerSvc := appcustomer.NewService(customerRepo, txm)
	customerSvc.SetAuditor(auditSvc)
	leadSvc := applead.NewService(leadRepo, txm, bus)
	leadSvc.SetAuditor(auditSvc)
	bookingSvc := appbooking.NewService(bookingRepo, pkgRepo, txm, bus)
	bookingSvc.SetAuditor(auditSvc)
	leadSvc.SetBookingCreator(leadBookingBridge{svc: bookingSvc})
	paymentSvc := apppayment.NewService(paymentRepo, bookingRepo, txm, bus)
	paymentSvc.SetAuditor(auditSvc)
	paymentSvc.SetFX(apppayment.SettingsFX{Repo: paymentRepo})
	taskSvc := apptask.NewService(taskRepo, txm, bus)
	taskSeeder := apptask.NewSeeder(taskRepo, txm)
	paymentSvc.SetTaskCreator(taskSeeder)
	targetRepo := pgrevenuetarget.NewRepository(pool)
	targetSvc := apprevenuetarget.NewService(targetRepo, txm)
	targetSvc.SetAuditor(auditSvc)
	targetSvc.SetTaskCreator(taskSeeder)
	targetReactor := apprevenuetarget.NewReactor(targetSvc, bookingRepo)
	targetReactor.Register(bus)
	pkgSvc := apppkg.NewService(pkgRepo, txm)
	pkgSvc.SetBookingReader(bookingRepo)
	dashSvc := appdashboard.NewService(dashAgg)
	docSvc := appdocument.NewService(docRepo, store, txm)
	docSvc.SetBookingContext(bookingDocsBridge{repo: bookingRepo})
	docSvc.SetEnqueuer(q)
	bookingSvc.SetDocReadiness(docSvc)
	visaSvc := appvisa.NewService(visaRepo, txm)
	supplierSvc := appsupplier.NewService(supplierRepo, txm)
	supplierSvc.SetEnqueuer(q)
	notifRepo := pgnotification.NewRepository(pool)
	notifSvc := appnotification.NewService(notifRepo, txm, log)
	notifSvc.SetUserDirectory(identityDirectory{repo: identityRepo})
	notifSvc.SetExternal(appnotification.LogExternal{Log: log})
	notifReactor := appnotification.NewReactor(notifSvc)
	notifReactor.Register(bus)
	inboxSvc := appinbox.NewService(inboxRepo, providers, txm, bus)
	inboxSvc.SetCustomerMatcher(inboxCustomerBridge{repo: customerRepo})
	inboxSvc.SetLeadShellCreator(inboxLeadBridge{svc: leadSvc})

	importRepo := pgimportexport.NewRepository(pool)
	importSvc := appimportexport.NewService(importRepo, customerRepo, txm)
	importSvc.SetEnqueuer(q)
	importSvc.SetQueueMode(q)

	taskSeeder.Register(bus)
	taskReactor := apptask.NewReactor(taskRepo, bookingRepo, txm)
	taskReactor.Register(bus)

	handlers := httpadapter.Handlers{
		Health: health.Handler{
			DB:      pool,
			Queue:   q,
			Version: cfg.App.Version,
			Env:     cfg.App.Env,
		},
		Auth:      authhttp.Handler{Svc: authSvc},
		Users:     usershttp.Handler{Svc: userSvc},
		Audit:     audithttp.Handler{Svc: auditSvc},
		Customer:  customerhttp.Handler{Svc: customerSvc},
		Lead:      leadhttp.Handler{Svc: leadSvc},
		Booking:   bookinghttp.Handler{Svc: bookingSvc},
		Payment:   paymenthttp.Handler{Svc: paymentSvc},
		Target:    targethttp.Handler{Svc: targetSvc},
		Task:      taskhttp.Handler{Svc: taskSvc},
		Dashboard: dashboardhttp.Handler{Svc: dashSvc},
		Document:  documenthttp.Handler{Svc: docSvc},
		Visa:      visahttp.Handler{Svc: visaSvc},
		Supplier:  supplierhttp.Handler{Svc: supplierSvc},
		Package:   pkghttp.Handler{Svc: pkgSvc},
		Ops:       opshttp.Handler{Queue: q},
		Inbox:     inboxhttp.Handler{Svc: inboxSvc},
		Import:    importhttp.Handler{Svc: importSvc},
		Notification: notificationhttp.Handler{Svc: notifSvc},
		Webhook: webhookhttp.Handler{
			Svc:             inboxSvc,
			DefaultBranchID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		},
	}

	router := httpadapter.NewRouter(cfg, tokens, handlers)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	return &Application{Cfg: cfg, Log: log, Pool: pool, Server: srv, Queue: q}, nil
}

func (a *Application) Close() {
	if a.Queue != nil {
		_ = a.Queue.Close()
	}
	if a.Pool != nil {
		a.Pool.Close()
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
