package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	pgcustomer "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/customer"
	pgimportexport "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/importexport"
	appimportexport "github.com/wodi-crm/wodi-crm-be/internal/app/importexport"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
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

	txm := tx.NewManager(pool)
	importRepo := pgimportexport.NewRepository(pool)
	customerRepo := pgcustomer.NewRepository(pool)
	importSvc := appimportexport.NewService(importRepo, customerRepo, txm)

	srv, err := worker.NewServer(cfg.Redis, log)
	if err != nil {
		log.Error("failed to start worker", "error", err)
		os.Exit(1)
	}
	srv.SetImportProcessor(importSvc.ProcessByStringID)

	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		slog.Info("worker shutting down")
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
