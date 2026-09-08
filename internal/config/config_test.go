package config_test

import (
	"os"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("DATABASE_URL", "postgres://wodi:wodi@localhost:5432/wodi_crm?sslmode=disable")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App.Name != "wodi-crm-be" && os.Getenv("APP_NAME") == "" {
		// default name
		if cfg.App.Name == "" {
			t.Fatal("empty app name")
		}
	}
	if cfg.HTTP.Addr == "" {
		t.Fatal("empty http addr")
	}
}

func TestProductionRejectsWeakSecrets(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=require")
	t.Setenv("JWT_ACCESS_SECRET", "change-me")
	t.Setenv("JWT_REFRESH_SECRET", "change-me")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected production validation failure")
	}
}
