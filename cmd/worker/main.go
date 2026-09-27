package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	pgcustomer "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/customer"
	pgimportexport "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/importexport"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgpii"
	"github.com/wodi-crm/wodi-crm-be/internal/app"
	"github.com/wodi-crm/wodi-crm-be/internal/app/dataprotection"
	appimportexport "github.com/wodi-crm/wodi-crm-be/internal/app/importexport"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/database"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/logger"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
	"github.com/wodi-crm/wodi-crm-be/internal/worker"
)

// Worker process for Asynq jobs (reminders, webhook retry, import, reports, AI).
func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	pool, err := database.NewPool(ctx, cfg.Database)
	cancel()
	if err != nil {
		log.Error("database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	keyring, err := app.NewKeyring(cfg.Auth)
	if err != nil {
		log.Error("encryption keyring", "error", err)
		os.Exit(1)
	}
	passports := pgpii.NewPassports(keyring)

	txm := tx.NewManager(pool)
	importRepo := pgimportexport.NewRepository(pool, passports)
	customerRepo := pgcustomer.NewRepository(pool, passports)
	importSvc := appimportexport.NewService(importRepo, customerRepo, txm)

	srv, err := worker.NewServer(cfg.Redis, log)
	if err != nil {
		log.Error("failed to start worker", "error", err)
		os.Exit(1)
	}
	srv.SetImportProcessor(importSvc.ProcessByStringID)
	cleanup := app.NewSecurityRetention(pool)
	srv.SetSecurityCleanup(func(ctx context.Context) error {
		_, err := cleanup.Run(ctx, uuid.Nil)
		return err
	})
	backfill := app.NewEncryptBackfill(pool, keyring)
	srv.SetEncryptBackfill(func(ctx context.Context, rehash bool) error {
		_, err := backfill.Run(ctx, uuid.Nil, dataprotection.Options{Rehash: rehash})
		return err
	})

	finance := app.NewFinanceJobs(pool, passports, cfg, log)
	srv.Register(fxdomain.JobRatesSync, finance.SyncRates)
	srv.Register(fxdomain.JobLiveSync, finance.SyncLive)
	srv.Register(paymentdomain.JobPromisesCheck, finance.CheckPromises)
	srv.Register(paymentdomain.JobSchedulesOverdue, finance.MarkSchedulesOverdue)

	bookings := app.NewBookingLifecycle(pool, passports, cfg, log)
	srv.Register(bookingdomain.JobHoldExpiry, bookings.HandleHoldExpiry)
	srv.Register(bookingdomain.JobTravelledSweep, bookings.HandleTravelledSweep)
	srv.Register(bookingdomain.JobRecomputeSweep, bookings.HandleRecomputeSweep)
	srv.Register(bookingdomain.JobRecompute, bookings.HandleRecompute)

	// Periodic jobs; every job listed here must also be registered on srv.
	schedule := []worker.ScheduleEntry{}
	schedule = append(schedule,
		worker.ScheduleEntry{Spec: "0 8 * * *", Job: paymentdomain.JobPromisesCheck},
		worker.ScheduleEntry{Spec: "0 * * * *", Job: paymentdomain.JobSchedulesOverdue},
		worker.ScheduleEntry{Spec: "*/5 * * * *", Job: bookingdomain.JobHoldExpiry},
		worker.ScheduleEntry{Spec: "15 0 * * *", Job: bookingdomain.JobTravelledSweep},
		worker.ScheduleEntry{Spec: "45 * * * *", Job: bookingdomain.JobRecomputeSweep},
	)
	if cfg.FX.ProviderURL != "" || cfg.FX.AccountingEnabled() {
		schedule = append(schedule, worker.ScheduleEntry{Spec: "30 6 * * *", Job: fxdomain.JobRatesSync})
	}
	if cfg.FX.LiveEnabled {
		schedule = append(schedule, worker.ScheduleEntry{Spec: "*/10 * * * *", Job: fxdomain.JobLiveSync})
	}
	loc, err := time.LoadLocation(cfg.Redis.SchedulerTZ)
	if err != nil {
		log.Error("scheduler time zone", "error", err)
		os.Exit(1)
	}
	sched, err := worker.NewScheduler(cfg.Redis, loc, schedule, log)
	if err != nil {
		log.Error("failed to start scheduler", "error", err)
		os.Exit(1)
	}
	go func() {
		if err := sched.Run(); err != nil {
			log.Error("scheduler stopped with error", "error", err)
		}
	}()

	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		slog.Info("worker shutting down")
		sched.Shutdown()
		srv.Shutdown()
	}()

	log.Info("worker process started",
		"redis", cfg.Redis.URL,
		"env", cfg.App.Env,
	)
	if err := srv.Run(); err != nil {
		log.Error("worker stopped with error", "error", err)
		os.Exit(1)
	}
}
