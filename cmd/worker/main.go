package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/logger"
)

// Worker process skeleton for Asynq jobs (reminders, webhook retry, import).
// Handlers are registered in internal/worker as P0 progresses.
func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)
	log.Info("worker process started (stub)",
		"redis", cfg.Redis.URL,
		"env", cfg.App.Env,
	)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	slog.Info("worker shutting down")
}
