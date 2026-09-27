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

func validFX() config.FXConfig {
	return config.FXConfig{
		LiveEnabled: true, LocalCurrency: "SYP", LivePinned: []string{"USD", "EUR", "SAR"},
		LiveCurrencies: []string{"USD", "TRY"}, LiveMarketMaxAge: 48 * time.Hour, LiveTimeout: 10 * time.Second,
		LiraScopeBaseURL: "https://lirascope.syria-cloud.sy/api/v1", ERAPIBaseURL: "https://open.er-api.com/v6",
		AccountingSource: "off",
	}
}

func TestFXLiveValidation(t *testing.T) {
	cases := map[string]struct {
		mutate func(*config.FXConfig)
		ok     bool
	}{
		"defaults":                   {func(*config.FXConfig) {}, true},
		"accounting official":        {func(f *config.FXConfig) { f.AccountingSource = "official" }, true},
		"accounting unknown":         {func(f *config.FXConfig) { f.AccountingSource = "street" }, false},
		"accounting without live":    {func(f *config.FXConfig) { f.LiveEnabled, f.AccountingSource = false, "market" }, false},
		"disabled skips live checks": {func(f *config.FXConfig) { f.LiveEnabled, f.LocalCurrency, f.LiraScopeBaseURL = false, "", "" }, true},
		"bad local currency":         {func(f *config.FXConfig) { f.LocalCurrency = "LIRA" }, false},
		"bad pinned code":            {func(f *config.FXConfig) { f.LivePinned = []string{"US$"} }, false},
		"no currencies":              {func(f *config.FXConfig) { f.LivePinned, f.LiveCurrencies = nil, nil }, false},
		"zero timeout":               {func(f *config.FXConfig) { f.LiveTimeout = 0 }, false},
		"bad lirascope url":          {func(f *config.FXConfig) { f.LiraScopeBaseURL = "lirascope.syria-cloud.sy" }, false},
		"bad erapi url":              {func(f *config.FXConfig) { f.ERAPIBaseURL = "ftp://open.er-api.com" }, false},
	}
	for name, c := range cases {
		fx := validFX()
		c.mutate(&fx)
		cfg := config.Config{App: config.AppConfig{Env: "local"}, Database: config.DatabaseConfig{URL: "postgres://x"}, Auth: validAuth(), FX: fx}
		if err := cfg.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: ok=%v, got err %v", name, c.ok, err)
		}
	}
}

func TestFXLiveDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	f := cfg.FX
	if os.Getenv("FX_LIVE_ENABLED") == "" && (!f.LiveEnabled || f.LocalCurrency != "SYP" || f.LiveMarketMaxAge != 48*time.Hour ||
		len(f.LivePinned) != 3 || f.LivePinned[0] != "USD" || f.AccountingEnabled()) {
		t.Errorf("live defaults: %+v", f)
	}
}
