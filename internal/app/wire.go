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
	leadhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/lead"
	opshttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/ops"
	paymenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/payment"
	taskhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/task"
	pkghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/tourpackage"
	usershttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/users"
	pgaudit "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/audit"
	pgbooking "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/booking"
	pgcustomer "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/customer"
	pgdocument "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/document"
	pgidentity "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/identity"
	pglead "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/lead"
	pgpayment "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/payment"
	pgtask "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/task"
	pkgpg "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/tourpackage"
	pgdash "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/queue"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/storage"
	appaudit "github.com/wodi-crm/wodi-crm-be/internal/app/audit"
	appauth "github.com/wodi-crm/wodi-crm-be/internal/app/auth"
	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	appcustomer "github.com/wodi-crm/wodi-crm-be/internal/app/customer"
	appdashboard "github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	appdocument "github.com/wodi-crm/wodi-crm-be/internal/app/document"
	applead "github.com/wodi-crm/wodi-crm-be/internal/app/lead"
	apppayment "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	apppkg "github.com/wodi-crm/wodi-crm-be/internal/app/tourpackage"
	appuser "github.com/wodi-crm/wodi-crm-be/internal/app/useradmin"
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
	dashAgg := pgdash.NewDashboardAggregator(pool)

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
	taskSvc := apptask.NewService(taskRepo, txm, bus)
	pkgSvc := apppkg.NewService(pkgRepo, txm)
	pkgSvc.SetBookingReader(bookingRepo)
	dashSvc := appdashboard.NewService(dashAgg)
	docSvc := appdocument.NewService(docRepo, store)

	taskSeeder := apptask.NewSeeder(taskRepo, txm)
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
		Task:      taskhttp.Handler{Svc: taskSvc},
		Dashboard: dashboardhttp.Handler{Svc: dashSvc},
		Document:  documenthttp.Handler{Svc: docSvc},
		Package:   pkghttp.Handler{Svc: pkgSvc},
		Ops:       opshttp.Handler{Queue: q},
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
