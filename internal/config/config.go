package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds process-wide settings loaded from environment.
type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Database DatabaseConfig
	Auth     AuthConfig
	Redis    RedisConfig
	Webhook  WebhookConfig
	Storage  StorageConfig
	Log      LogConfig
	FX       FXConfig
	Flights  FlightsConfig
	Payments PaymentsConfig
}

// PaymentsConfig configures customer payment links. LinkURLTemplate is the
// hosted (3-D Secure) checkout URL of the payment provider with {ref},
// {amount}, {currency} and {booking_id} placeholders; empty disables links.
type PaymentsConfig struct {
	LinkURLTemplate string
}

// FlightsConfig configures the flight finder (Travelpayouts Data API). An
// empty token keeps place suggestions but disables fare search.
type FlightsConfig struct {
	TravelpayoutsToken  string
	TravelpayoutsMarker string
	APIURL              string
	AutocompleteURL     string
	AviasalesURL        string
	Market              string
	Timeout             time.Duration
}

// FXConfig configures the optional generic rate provider (an empty
// ProviderURL disables it) and the live rate board.
type FXConfig struct {
	ProviderURL     string
	ProviderBase    string
	ProviderTimeout time.Duration

	LiveEnabled bool
	// LocalCurrency is the currency live quotes are expressed in.
	LocalCurrency  string
	LivePinned     []string
	LiveCurrencies []string
	// LiveMarketMaxAge is how old a direct market quote may be before the
	// board derives it from the market USD rate instead.
	LiveMarketMaxAge   time.Duration
	LiveTimeout        time.Duration
	LiraScopeBaseURL   string
	LiraScopeAPIKey    string
	LiraScopeAPISecret string
	ERAPIBaseURL       string
	// AccountingSource (off | official | market) makes fx.rates_sync also
	// store today's live mid of every pinned currency.
	AccountingSource string
}

// AccountingEnabled reports whether fx.rates_sync writes live rates.
func (f FXConfig) AccountingEnabled() bool {
	return f.AccountingSource == "official" || f.AccountingSource == "market"
}

type AppConfig struct {
	Name    string
	Env     string // local | development | staging | production
	Version string
}

type HTTPConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	CORSOrigins     []string
	TrustedProxies  []string
	MaxBodyBytes    int64
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

type AuthConfig struct {
	JWTAccessSecret string
	// JWTAccessKeyID is the kid of JWTAccessSecret, stamped on new tokens.
	JWTAccessKeyID string
	// JWTPreviousAccessSecrets are "kid:secret" entries that only verify,
	// kept until tokens signed with them have expired (AccessTTL).
	JWTPreviousAccessSecrets []string
	AccessTTL                time.Duration
	// RefreshTTL is the idle session lifetime: each refresh extends it.
	RefreshTTL time.Duration
	// SessionAbsoluteTTL caps a session's age regardless of activity.
	SessionAbsoluteTTL time.Duration
	// SessionCheckCacheTTL bounds how long an instance may keep accepting
	// access tokens of a session revoked on another instance.
	SessionCheckCacheTTL time.Duration
	// RefreshReuseGrace tolerates a just-rotated refresh token from a
	// concurrent request; 0 treats every replay as theft.
	RefreshReuseGrace time.Duration
	Issuer            string
	Audience          string
	// EncryptionKey is a base64 32-byte AES-256 key for secrets/PII at rest.
	EncryptionKey string
	// EncryptionKeyID versions the key so ciphertexts survive rotation.
	EncryptionKeyID string
	// PreviousEncryptionKeys are "id:base64key" entries kept for decryption.
	PreviousEncryptionKeys []string
	// BlindIndexKey (base64 32 bytes) pins the passport/verify-token blind
	// index key; unset derives it from EncryptionKey, which changes the index
	// on key rotation (then rerun the encrypt backfill with rehash).
	BlindIndexKey    string
	LoginMaxAttempts int
	LoginLockout     time.Duration
	LoginWindow      time.Duration
	// LoginLockoutMax caps the doubling lockout duration.
	LoginLockoutMax time.Duration
	// LoginIPMaxAttempts throttles failed logins per client IP within LoginWindow.
	LoginIPMaxAttempts int
	// MFAEnforce requires gm/admin to enroll TOTP; always on in production.
	MFAEnforce bool
	MFAIssuer  string
}

// WebhookConfig holds env fallbacks used when an integration account has no
// secret of its own. Keys are lowercase provider names.
type WebhookConfig struct {
	Secrets      map[string]string
	VerifyTokens map[string]string
	RateLimit    int
	RateWindow   time.Duration
}

type RedisConfig struct {
	URL string
	// SchedulerTZ is the IANA zone cron specs are evaluated in.
	SchedulerTZ string
	// ScheduleOverrides maps SCHEDULE_<JOB> (job name upper-cased, "." → "_")
	// to a cron spec, or "off" to disable that job.
	ScheduleOverrides map[string]string
}

// ScheduleKey is the SCHEDULE_ suffix for a job name (inbox.sla_sweep → INBOX_SLA_SWEEP).
func ScheduleKey(job string) string {
	return strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(job))
}

// RequiresRedis reports whether queues must be backed by Redis: staging and
// production never fall back to the in-memory queue.
func (c Config) RequiresRedis() bool {
	return c.App.Env == "production" || c.App.Env == "staging"
}

type StorageConfig struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	PublicURL string
}

type LogConfig struct {
	Level  string // debug | info | warn | error
	Format string // json | text
}

// Load reads optional .env then environment variables.
func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		App: AppConfig{
			Name:    getEnv("APP_NAME", "wodi-crm-be"),
			Env:     getEnv("APP_ENV", "local"),
			Version: getEnv("APP_VERSION", "0.1.0"),
		},
		HTTP: HTTPConfig{
			Addr:            getEnv("HTTP_ADDR", ":8080"),
			ReadTimeout:     getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: getDuration("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
			CORSOrigins:     splitCSV(getEnv("CORS_ORIGINS", "http://localhost:3000")),
			TrustedProxies:  splitCSV(getEnv("TRUSTED_PROXIES", "127.0.0.1,::1")),
			MaxBodyBytes:    int64(getInt("HTTP_MAX_BODY_BYTES", 2<<20)),
		},
		Database: DatabaseConfig{
			URL:             getEnv("DATABASE_URL", "postgres://wodi:wodi@localhost:5432/wodi_crm?sslmode=disable"),
			MaxConns:        int32(getInt("DB_MAX_CONNS", 20)),
			MinConns:        int32(getInt("DB_MIN_CONNS", 2)),
			MaxConnLifetime: getDuration("DB_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime: getDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
		},
		Auth: AuthConfig{
			JWTAccessSecret:          getEnv("JWT_ACCESS_SECRET", "dev-access-secret-change-me"),
			JWTAccessKeyID:           getEnv("JWT_ACCESS_KEY_ID", "a1"),
			JWTPreviousAccessSecrets: splitCSV(getEnv("JWT_PREVIOUS_ACCESS_SECRETS", "")),
			AccessTTL:                getDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTTL:               getDuration("JWT_REFRESH_TTL", 7*24*time.Hour),
			SessionAbsoluteTTL:       getDuration("SESSION_ABSOLUTE_TTL", 30*24*time.Hour),
			SessionCheckCacheTTL:     getDuration("SESSION_CHECK_CACHE_TTL", 10*time.Second),
			RefreshReuseGrace:        getDuration("REFRESH_REUSE_GRACE", 10*time.Second),
			Issuer:                   getEnv("JWT_ISSUER", "wodi-crm-be"),
			Audience:                 getEnv("JWT_AUDIENCE", "wodi-crm-api"),
			// Dev-only default key; Validate rejects it in production.
			EncryptionKey:          getEnv("ENCRYPTION_KEY", devEncryptionKey),
			EncryptionKeyID:        getEnv("ENCRYPTION_KEY_ID", "k1"),
			PreviousEncryptionKeys: splitCSV(getEnv("ENCRYPTION_PREVIOUS_KEYS", "")),
			BlindIndexKey:          getEnv("ENCRYPTION_BLIND_INDEX_KEY", ""),
			LoginMaxAttempts:       getInt("LOGIN_MAX_ATTEMPTS", 5),
			LoginLockout:           getDuration("LOGIN_LOCKOUT", 15*time.Minute),
			LoginWindow:            getDuration("LOGIN_WINDOW", 15*time.Minute),
			LoginLockoutMax:        getDuration("LOGIN_LOCKOUT_MAX", 24*time.Hour),
			LoginIPMaxAttempts:     getInt("LOGIN_IP_MAX_ATTEMPTS", 20),
			MFAIssuer:              getEnv("MFA_ISSUER", "Wodi CRM"),
		},
		Webhook: WebhookConfig{
			Secrets:      envByProvider("WEBHOOK_SECRET_"),
			VerifyTokens: envByProvider("WEBHOOK_VERIFY_TOKEN_"),
			RateLimit:    getInt("WEBHOOK_RATE_LIMIT", 300),
			RateWindow:   getDuration("WEBHOOK_RATE_WINDOW", time.Minute),
		},
		Redis: RedisConfig{
			URL:               getEnv("REDIS_URL", "redis://localhost:6379/0"),
			SchedulerTZ:       getEnv("SCHEDULER_TZ", "Asia/Riyadh"),
			ScheduleOverrides: envWithPrefix("SCHEDULE_"),
		},
		Storage: StorageConfig{
			Endpoint:  getEnv("S3_ENDPOINT", "http://localhost:9000"),
			Region:    getEnv("S3_REGION", "us-east-1"),
			Bucket:    getEnv("S3_BUCKET", "wodi-crm"),
			AccessKey: getEnv("S3_ACCESS_KEY", "minioadmin"),
			SecretKey: getEnv("S3_SECRET_KEY", "minioadmin"),
			UseSSL:    getBool("S3_USE_SSL", false),
			PublicURL: getEnv("S3_PUBLIC_URL", "http://localhost:9000"),
		},
		Payments: PaymentsConfig{
			LinkURLTemplate: getEnv("PAYMENT_LINK_URL_TEMPLATE", ""),
		},
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		FX: FXConfig{
			ProviderURL:     strings.TrimSpace(getEnv("FX_PROVIDER_URL", "")),
			ProviderBase:    strings.ToUpper(getEnv("FX_PROVIDER_BASE", "USD")),
			ProviderTimeout: getDuration("FX_PROVIDER_TIMEOUT", 10*time.Second),

			LiveEnabled:        getBool("FX_LIVE_ENABLED", true),
			LocalCurrency:      strings.ToUpper(strings.TrimSpace(getEnv("FX_LOCAL_CURRENCY", "SYP"))),
			LivePinned:         splitCSV(strings.ToUpper(getEnv("FX_LIVE_PINNED", "USD,EUR,SAR"))),
			LiveCurrencies:     splitCSV(strings.ToUpper(getEnv("FX_LIVE_CURRENCIES", "USD,EUR,SAR,TRY,AED,GBP,JOD,EGP,KWD,QAR,LBP,IQD"))),
			LiveMarketMaxAge:   getDuration("FX_LIVE_MARKET_MAX_AGE", 48*time.Hour),
			LiveTimeout:        getDuration("FX_LIVE_TIMEOUT", 10*time.Second),
			LiraScopeBaseURL:   strings.TrimSpace(getEnv("LIRASCOPE_BASE_URL", "https://lirascope.syria-cloud.sy/api/v1")),
			LiraScopeAPIKey:    strings.TrimSpace(os.Getenv("LIRASCOPE_API_KEY")),
			LiraScopeAPISecret: strings.TrimSpace(os.Getenv("LIRASCOPE_API_SECRET")),
			ERAPIBaseURL:       strings.TrimSpace(getEnv("ERAPI_BASE_URL", "https://open.er-api.com/v6")),
			AccountingSource:   strings.ToLower(strings.TrimSpace(getEnv("FX_ACCOUNTING_SOURCE", "off"))),
		},
		Flights: FlightsConfig{
			TravelpayoutsToken:  strings.TrimSpace(os.Getenv("TRAVELPAYOUTS_TOKEN")),
			TravelpayoutsMarker: strings.TrimSpace(getEnv("TRAVELPAYOUTS_MARKER", "784605")),
			APIURL:              strings.TrimSpace(getEnv("TRAVELPAYOUTS_API_URL", "https://api.travelpayouts.com")),
			AutocompleteURL:     strings.TrimSpace(getEnv("TRAVELPAYOUTS_AUTOCOMPLETE_URL", "https://autocomplete.travelpayouts.com")),
			AviasalesURL:        strings.TrimSpace(getEnv("AVIASALES_URL", "https://www.aviasales.com")),
			Market:              strings.ToLower(strings.TrimSpace(os.Getenv("TRAVELPAYOUTS_MARKET"))),
			Timeout:             getDuration("TRAVELPAYOUTS_TIMEOUT", 10*time.Second),
		},
	}

	cfg.Auth.MFAEnforce = getBool("MFA_ENFORCE", !cfg.IsLocal())

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// webhookProviders are the channels that may receive signed webhooks.
var webhookProviders = []string{"whatsapp", "instagram", "facebook", "gmail", "email", "stub"}

// envByProvider reads <prefix><PROVIDER> for each webhook provider.
func envByProvider(prefix string) map[string]string {
	out := map[string]string{}
	for _, p := range webhookProviders {
		if v := strings.TrimSpace(os.Getenv(prefix + strings.ToUpper(p))); v != "" {
			out[p] = v
		}
	}
	return out
}

// envWithPrefix collects KEY=value pairs whose key starts with prefix, keyed
// by the remainder.
func envWithPrefix(prefix string) map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, prefix) || strings.TrimSpace(v) == "" {
			continue
		}
		out[strings.TrimPrefix(k, prefix)] = strings.TrimSpace(v)
	}
	return out
}

// Validate enforces production-safe minimums.
func (c Config) Validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.RequiresRedis() && (c.Redis.URL == "" || strings.HasPrefix(c.Redis.URL, "memory://")) {
		return fmt.Errorf("REDIS_URL must point to Redis in %s (no in-memory queue fallback)", c.App.Env)
	}
	if err := c.Auth.validateSessions(); err != nil {
		return err
	}
	if _, err := time.LoadLocation(c.Redis.SchedulerTZ); err != nil {
		return fmt.Errorf("SCHEDULER_TZ: %w", err)
	}
	if err := c.FX.validate(); err != nil {
		return err
	}
	if c.App.Env == "production" {
		if strings.Contains(c.Auth.JWTAccessSecret, "change-me") {
			return fmt.Errorf("JWT_ACCESS_SECRET must be set in production")
		}
		if len(c.Auth.JWTAccessSecret) < minProdSecretLen {
			return fmt.Errorf("JWT secrets must be at least %d characters in production", minProdSecretLen)
		}
		for _, e := range c.Auth.JWTPreviousAccessSecrets {
			if _, secret, _ := strings.Cut(e, ":"); len(secret) < minProdSecretLen {
				return fmt.Errorf("JWT secrets must be at least %d characters in production", minProdSecretLen)
			}
		}
		if c.Auth.SessionAbsoluteTTL > maxProdSessionAbsoluteTTL {
			return fmt.Errorf("SESSION_ABSOLUTE_TTL must not exceed %s in production", maxProdSessionAbsoluteTTL)
		}
		if c.Auth.SessionCheckCacheTTL > maxProdSessionCheckCacheTTL {
			return fmt.Errorf("SESSION_CHECK_CACHE_TTL must not exceed %s in production", maxProdSessionCheckCacheTTL)
		}
		if c.Auth.EncryptionKey == "" || c.Auth.EncryptionKey == devEncryptionKey {
			return fmt.Errorf("ENCRYPTION_KEY must be set in production")
		}
		if !c.Auth.MFAEnforce {
			return fmt.Errorf("MFA_ENFORCE cannot be disabled in production")
		}
	}
	return nil
}

const (
	minProdSecretLen            = 32
	maxProdSessionAbsoluteTTL   = 90 * 24 * time.Hour
	maxProdSessionCheckCacheTTL = time.Minute
	maxRefreshReuseGrace        = time.Minute
)

// validateSessions enforces token/session settings that are unsafe in any environment.
func (a AuthConfig) validateSessions() error {
	if a.JWTAccessSecret == "" || a.JWTAccessKeyID == "" {
		return fmt.Errorf("JWT_ACCESS_SECRET and JWT_ACCESS_KEY_ID are required")
	}
	if a.Issuer == "" || a.Audience == "" {
		return fmt.Errorf("JWT_ISSUER and JWT_AUDIENCE are required")
	}
	kids := map[string]bool{a.JWTAccessKeyID: true}
	for _, e := range a.JWTPreviousAccessSecrets {
		kid, secret, ok := strings.Cut(e, ":")
		kid = strings.TrimSpace(kid)
		if !ok || kid == "" || secret == "" {
			return fmt.Errorf("JWT_PREVIOUS_ACCESS_SECRETS entries must be kid:secret")
		}
		if kids[kid] {
			return fmt.Errorf("JWT_PREVIOUS_ACCESS_SECRETS: duplicate kid %q", kid)
		}
		kids[kid] = true
	}
	if a.AccessTTL <= 0 || a.RefreshTTL <= 0 || a.SessionAbsoluteTTL <= 0 {
		return fmt.Errorf("JWT_ACCESS_TTL, JWT_REFRESH_TTL and SESSION_ABSOLUTE_TTL must be positive")
	}
	if a.RefreshTTL > a.SessionAbsoluteTTL {
		return fmt.Errorf("JWT_REFRESH_TTL (idle) must not exceed SESSION_ABSOLUTE_TTL")
	}
	if a.SessionCheckCacheTTL < 0 {
		return fmt.Errorf("SESSION_CHECK_CACHE_TTL must not be negative")
	}
	if a.RefreshReuseGrace < 0 || a.RefreshReuseGrace > maxRefreshReuseGrace {
		return fmt.Errorf("REFRESH_REUSE_GRACE must be between 0 and %s", maxRefreshReuseGrace)
	}
	return nil
}

func (f FXConfig) validate() error {
	switch f.AccountingSource {
	case "", "off", "official", "market":
	default:
		return fmt.Errorf("FX_ACCOUNTING_SOURCE must be off, official or market")
	}
	if !f.LiveEnabled {
		if f.AccountingEnabled() {
			return fmt.Errorf("FX_ACCOUNTING_SOURCE requires FX_LIVE_ENABLED")
		}
		return nil
	}
	if !isCurrencyCode(f.LocalCurrency) {
		return fmt.Errorf("FX_LOCAL_CURRENCY must be an ISO-4217 code")
	}
	if len(f.LivePinned)+len(f.LiveCurrencies) == 0 {
		return fmt.Errorf("FX_LIVE_PINNED or FX_LIVE_CURRENCIES must list at least one currency")
	}
	for _, c := range append(append([]string{}, f.LivePinned...), f.LiveCurrencies...) {
		if !isCurrencyCode(c) {
			return fmt.Errorf("FX_LIVE_PINNED/FX_LIVE_CURRENCIES: %q is not an ISO-4217 code", c)
		}
	}
	if f.LiveMarketMaxAge <= 0 || f.LiveTimeout <= 0 {
		return fmt.Errorf("FX_LIVE_MARKET_MAX_AGE and FX_LIVE_TIMEOUT must be positive")
	}
	for key, v := range map[string]string{"LIRASCOPE_BASE_URL": f.LiraScopeBaseURL, "ERAPI_BASE_URL": f.ERAPIBaseURL} {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%s must be an http(s) URL", key)
		}
	}
	return nil
}

func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// devEncryptionKey is a public 32-byte key for local development only.
const devEncryptionKey = "ZGV2LW9ubHktZW5jcnlwdGlvbi1rZXktMzJieXRlcyE="

func (c Config) IsLocal() bool {
	return c.App.Env == "local" || c.App.Env == "development"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
