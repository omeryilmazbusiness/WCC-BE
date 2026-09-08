package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/app"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	application, err := app.New(ctx, cfg, log)
	cancel()
	if err != nil {
		log.Error("failed to bootstrap application", "error", err)
		os.Exit(1)
	}
	defer application.Close()

	go func() {
		log.Info("http server listening",
			"addr", cfg.HTTP.Addr,
			"env", cfg.App.Env,
			"version", cfg.App.Version,
		)
		if err := application.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer shutdownCancel()
	log.Info("shutting down")
	if err := application.Server.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}
