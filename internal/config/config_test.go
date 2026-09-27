package config_test

import (
	"os"
	"testing"
	"time"

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
	if _, err := config.Load(); err == nil {
		t.Fatal("expected production validation failure")
	}
}

func validAuth() config.AuthConfig {
	return config.AuthConfig{
		JWTAccessSecret: "0123456789abcdef0123456789abcdef", JWTAccessKeyID: "a1",
		AccessTTL: 15 * time.Minute, RefreshTTL: 7 * 24 * time.Hour, SessionAbsoluteTTL: 30 * 24 * time.Hour,
		SessionCheckCacheTTL: 10 * time.Second, RefreshReuseGrace: 10 * time.Second,
		Issuer: "wodi-crm-be", Audience: "wodi-crm-api",
		EncryptionKey: "cHJvZC1lbmNyeXB0aW9uLWtleS0zMmJ5dGVzLW9rISE=", MFAEnforce: true,
	}
}

func TestSessionSettingsValidation(t *testing.T) {
	cases := map[string]struct {
		env    string
		mutate func(*config.AuthConfig)
		ok     bool
	}{
		"defaults": {"production", func(*config.AuthConfig) {}, true},
		"previous keys": {"production", func(a *config.AuthConfig) {
			a.JWTPreviousAccessSecrets = []string{"a0:0123456789abcdef0123456789abcdef"}
		}, true},
		"previous key malformed":    {"local", func(a *config.AuthConfig) { a.JWTPreviousAccessSecrets = []string{"nosecret"} }, false},
		"previous key reuses kid":   {"local", func(a *config.AuthConfig) { a.JWTPreviousAccessSecrets = []string{"a1:whatever"} }, false},
		"previous key short (prod)": {"production", func(a *config.AuthConfig) { a.JWTPreviousAccessSecrets = []string{"a0:short"} }, false},
		"absolute above 90d (prod)": {"production", func(a *config.AuthConfig) { a.SessionAbsoluteTTL = 91 * 24 * time.Hour }, false},
		"absolute above 90d (dev)":  {"local", func(a *config.AuthConfig) { a.SessionAbsoluteTTL = 91 * 24 * time.Hour }, true},
		"idle above absolute":       {"local", func(a *config.AuthConfig) { a.RefreshTTL = 31 * 24 * time.Hour }, false},
		"cache ttl above 1m (prod)": {"production", func(a *config.AuthConfig) { a.SessionCheckCacheTTL = 2 * time.Minute }, false},
		"grace disabled":            {"local", func(a *config.AuthConfig) { a.RefreshReuseGrace = 0 }, true},
		"grace too long":            {"local", func(a *config.AuthConfig) { a.RefreshReuseGrace = 2 * time.Minute }, false},
		"missing audience":          {"local", func(a *config.AuthConfig) { a.Audience = "" }, false},
		"missing kid":               {"local", func(a *config.AuthConfig) { a.JWTAccessKeyID = "" }, false},
	}
	for name, c := range cases {
		auth := validAuth()
		c.mutate(&auth)
		cfg := config.Config{App: config.AppConfig{Env: c.env}, Database: config.DatabaseConfig{URL: "postgres://x"}, Auth: auth}
		if err := cfg.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: ok=%v, got err %v", name, c.ok, err)
		}
	}
}
