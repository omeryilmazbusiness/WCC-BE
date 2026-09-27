package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	webhookhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/webhook"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/signature"
	pgaudit "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/audit"
	pgidentity "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/identity"
	pginbox "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/inbox"
	pgwebhook "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/webhook"
	appaudit "github.com/wodi-crm/wodi-crm-be/internal/app/audit"
	appauth "github.com/wodi-crm/wodi-crm-be/internal/app/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/app/retention"
	appwebhook "github.com/wodi-crm/wodi-crm-be/internal/app/webhook"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	domaininbox "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/ratelimit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// newRateLimiter shares counters through Redis and degrades to per-instance
// memory counters when Redis is unset or unreachable. The returned client
// may be nil.
func newRateLimiter(cfg config.RedisConfig, log *slog.Logger) (ratelimit.Window, *redis.Client) {
	mem := ratelimit.NewMemory()
	if cfg.URL == "" || cfg.URL == "memory://" {
		return mem, nil
	}
	opt, err := redis.ParseURL(cfg.URL)
	if err != nil {
		log.Error("rate limiter: invalid REDIS_URL; using in-memory counters", "error", err)
		return mem, nil
	}
	rdb := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Warn("rate limiter: redis not reachable at startup; falling back per request", "error", err)
	}
	return ratelimit.NewResilient(ratelimit.NewRedis(rdb, "wodi:rl:"), mem, log), rdb
}

func newAuthService(
	cfg config.Config,
	identityRepo *pgidentity.Repository,
	sec *pgidentity.SecurityRepository,
	auditor audit.Recorder,
	tokens appauth.AccessTokenIssuer,
	txm tx.Runner,
	cipher crypto.Cipher,
	limiter ratelimit.Window,
	sessionCache appauth.SessionInvalidator,
) *appauth.Service {
	mfaPolicy := platformauth.NewMFAPolicy()
	if cfg.Auth.MFAEnforce {
		mfaPolicy = platformauth.NewMFAPolicy(platformauth.RoleGM, platformauth.RoleAdmin)
	}
	return appauth.NewService(appauth.Deps{
		Users: identityRepo, Audit: auditor, Tokens: tokens, Tx: txm,
		Lockouts: sec, MFA: sec, Challenges: sec, Refresh: sec, Sessions: sec, SessionCache: sessionCache,
		Cipher: cipher, Limiter: limiter,
		Options: appauth.Options{
			Lockout: authsec.LockoutPolicy{
				MaxAttempts: cfg.Auth.LoginMaxAttempts,
				Window:      cfg.Auth.LoginWindow,
				Base:        cfg.Auth.LoginLockout,
				Max:         cfg.Auth.LoginLockoutMax,
			},
			IPMaxAttempts: cfg.Auth.LoginIPMaxAttempts,
			MFA:           mfaPolicy,
			Issuer:        cfg.Auth.MFAIssuer,
			Session:       authsec.SessionPolicy{Idle: cfg.Auth.RefreshTTL, Absolute: cfg.Auth.SessionAbsoluteTTL},
			ReuseGrace:    cfg.Auth.RefreshReuseGrace,
		},
	})
}

// NewSecurityRetention builds the retention purge for processes outside the
// API (worker); the API wires the same service in New.
func NewSecurityRetention(pool *pgxpool.Pool) *retention.Service {
	auditRepo := pgaudit.NewRepository(pool)
	auditor := systemAuditor{svc: appaudit.NewService(auditRepo), repo: auditRepo}
	sec := pgidentity.NewSecurityRepository(pgidentity.NewRepository(pool))
	return retention.NewService(sec, pgwebhook.NewRepository(pool), auditor, retention.DefaultPolicy)
}

func newWebhookHandler(
	cfg config.Config,
	log *slog.Logger,
	pool *pgxpool.Pool,
	inboxRepo *pginbox.Repository,
	ingestor appwebhook.Ingestor,
	auditor audit.Recorder,
	limiter ratelimit.Window,
) webhookhttp.Handler {
	meta, shared := signature.Meta(), signature.Shared()
	svc := appwebhook.NewService(appwebhook.Deps{
		Accounts: inboxRepo,
		Events:   pgwebhook.NewRepository(pool),
		Ingestor: ingestor,
		Verifiers: map[domaininbox.Channel]appwebhook.SignatureVerifier{
			domaininbox.ChannelWhatsApp:  meta,
			domaininbox.ChannelInstagram: meta,
			domaininbox.ChannelFacebook:  meta,
			domaininbox.ChannelGmail:     shared,
			domaininbox.ChannelEmail:     shared,
			domaininbox.ChannelStub:      shared,
		},
		Audit: auditor,
		Log:   log,
		Options: appwebhook.Options{
			AllowUnsigned:   cfg.IsLocal(),
			EnvSecrets:      byChannel(cfg.Webhook.Secrets),
			EnvVerifyTokens: byChannel(cfg.Webhook.VerifyTokens),
		},
	})
	return webhookhttp.Handler{
		Svc: svc, Limiter: limiter, RateLimit: cfg.Webhook.RateLimit, RateWindow: cfg.Webhook.RateWindow,
	}
}

func byChannel(m map[string]string) map[domaininbox.Channel]string {
	out := make(map[domaininbox.Channel]string, len(m))
	for k, v := range m {
		out[domaininbox.Channel(k)] = v
	}
	return out
}

// systemAuditor records events without a user actor (e.g. rejected webhooks)
// with a NULL actor_id; user-attributed events go through the audit service.
type systemAuditor struct {
	svc  *appaudit.Service
	repo audit.Repository
}

func (a systemAuditor) Record(ctx context.Context, in audit.RecordInput) error {
	if in.ActorID != uuid.Nil {
		return a.svc.Record(ctx, in)
	}
	meta := map[string]any{}
	for k, v := range in.Extra {
		meta[k] = v
	}
	raw, _ := json.Marshal(meta)
	return a.repo.Insert(ctx, &audit.Event{
		ID: uuid.New(), Action: in.Action, EntityType: in.EntityType, EntityID: in.EntityID,
		BranchID: in.BranchID, Metadata: raw, IP: in.IP, UserAgent: in.UserAgent, CreatedAt: time.Now().UTC(),
	})
}
