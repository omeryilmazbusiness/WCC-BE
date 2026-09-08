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
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime     time.Duration
	MaxConnIdleTime time.Duration
}

type AuthConfig struct {
	JWTAccessSecret  string
	JWTRefreshSecret string
	AccessTTL        time.Duration
	RefreshTTL       time.Duration
	Issuer           string
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
		},
		Database: DatabaseConfig{
			URL:             getEnv("DATABASE_URL", "postgres://wodi:wodi@localhost:5432/wodi_crm?sslmode=disable"),
			MaxConns:        int32(getInt("DB_MAX_CONNS", 20)),
			MinConns:        int32(getInt("DB_MIN_CONNS", 2)),
			MaxConnLifetime: getDuration("DB_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime: getDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
		},
		Auth: AuthConfig{
			JWTAccessSecret:  getEnv("JWT_ACCESS_SECRET", "dev-access-secret-change-me"),
			JWTRefreshSecret: getEnv("JWT_REFRESH_SECRET", "dev-refresh-secret-change-me"),
			AccessTTL:        getDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTTL:       getDuration("JWT_REFRESH_TTL", 7*24*time.Hour),
			Issuer:           getEnv("JWT_ISSUER", "wodi-crm-be"),
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

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate enforces production-safe minimums.
func (c Config) Validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.App.Env == "production" {
		if strings.Contains(c.Auth.JWTAccessSecret, "change-me") ||
			strings.Contains(c.Auth.JWTRefreshSecret, "change-me") {
			return fmt.Errorf("JWT secrets must be set in production")
		}
		if len(c.Auth.JWTAccessSecret) < 32 || len(c.Auth.JWTRefreshSecret) < 32 {
			return fmt.Errorf("JWT secrets must be at least 32 characters in production")
		}
	}
	return nil
}

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
