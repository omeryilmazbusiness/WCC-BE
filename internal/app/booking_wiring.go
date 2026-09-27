package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	pgaudit "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/audit"
	pgbooking "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/booking"
	pgdocument "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/document"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgpii"
	pgtask "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/task"
	pkgpg "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/storage"
	appaudit "github.com/wodi-crm/wodi-crm-be/internal/app/audit"
	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	appdocument "github.com/wodi-crm/wodi-crm-be/internal/app/document"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// NewBookingLifecycle builds the worker's booking lifecycle with the same
// readiness inputs as the API; without the document gate the sweeps could
// derive ready while required documents are missing.
func NewBookingLifecycle(pool *pgxpool.Pool, passports *pgpii.Passports, cfg config.Config, log *slog.Logger) *appbooking.Lifecycle {
	txm := tx.NewManager(pool)
	bookingRepo := pgbooking.NewRepository(pool, passports)
	svc := appbooking.NewService(bookingRepo, bookingRepo, pkgpg.NewRepository(pool), txm, events.NewBus(log))
	svc.SetAuditor(appaudit.NewService(pgaudit.NewRepository(pool)))
	svc.SetTimeZone(businessLocation(cfg))
	docs := appdocument.NewService(pgdocument.NewRepository(pool), storage.NewMinIO(cfg.Storage), txm)
	docs.SetBookingContext(bookingDocsBridge{repo: bookingRepo})
	svc.SetDocReadiness(docs)
	return appbooking.NewLifecycle(svc, apptask.NewSeeder(pgtask.NewRepository(pool), txm))
}
