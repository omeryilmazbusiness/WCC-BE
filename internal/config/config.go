package config

import (
	"fmt"
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
	LoginMaxAttempts       int
	LoginLockout           time.Duration
	LoginWindow            time.Duration
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
			URL: getEnv("REDIS_URL", "redis://localhost:6379/0"),
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
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
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

// Validate enforces production-safe minimums.
func (c Config) Validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if err := c.Auth.validateSessions(); err != nil {
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
