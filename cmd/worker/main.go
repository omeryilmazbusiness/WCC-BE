package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/app"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/logger"
)

// Worker process: scheduled jobs, queued jobs and the outbox dispatcher.
func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	w, err := app.NewWorker(ctx, cfg, log)
	cancel()
	if err != nil {
		log.Error("failed to start worker", "error", err)
		os.Exit(1)
	}
	defer w.Close()

	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		log.Info("worker shutting down")
		w.Shutdown()
	}()

	log.Info("worker process started", "env", cfg.App.Env)
	if err := w.Run(); err != nil {
		log.Error("worker stopped with error", "error", err)
		os.Exit(1)
	}
}
