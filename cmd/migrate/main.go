package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/logger"
)

func main() {
	dir := flag.String("dir", "migrations", "migrations directory")
	command := flag.String("command", "up", "goose command: up|down|status|reset")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)

	db, err := sql.Open("pgx", cfg.Database.URL)
	if err != nil {
		log.Error("open db", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Error("ping db", "error", err)
		os.Exit(1)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		log.Error("goose dialect", "error", err)
		os.Exit(1)
	}

	var runErr error
	switch *command {
	case "up":
		runErr = goose.Up(db, *dir)
	case "down":
		runErr = goose.Down(db, *dir)
	case "status":
		runErr = goose.Status(db, *dir)
	case "reset":
		runErr = goose.Reset(db, *dir)
	default:
		runErr = fmt.Errorf("unknown command %q", *command)
	}
	if runErr != nil {
		log.Error("migrate failed", "error", runErr)
		os.Exit(1)
	}
	log.Info("migrate ok", "command", *command)
}
