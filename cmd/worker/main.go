package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/logger"
	"github.com/wodi-crm/wodi-crm-be/internal/worker"
)

// Worker process for Asynq jobs (reminders, webhook retry, import, reports, AI).
func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)

	srv, err := worker.NewServer(cfg.Redis, log)
	if err != nil {
		log.Error("failed to start worker", "error", err)
		os.Exit(1)
	}

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
