// Package retention purges security records that are past their use:
// ended sessions and refresh tokens, expired MFA challenges and the raw
// webhook journal. Every purge is idempotent, so the job may run at any
// cadence (scheduling arrives with the scheduler epic).
package retention

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
)

type SessionPurger interface {
	// PurgeSessions deletes sessions and refresh tokens revoked or expired before cutoff.
	PurgeSessions(ctx context.Context, cutoff time.Time) (sessions, tokens int64, err error)
	PurgeExpiredChallenges(ctx context.Context, now time.Time) (int64, error)
}

type WebhookPurger interface {
	PurgeWebhookEvents(ctx context.Context, cutoff time.Time) (int64, error)
}

// Policy is how long ended records are kept for investigation.
type Policy struct {
	Sessions      time.Duration
	WebhookEvents time.Duration
}

var DefaultPolicy = Policy{Sessions: 30 * 24 * time.Hour, WebhookEvents: 90 * 24 * time.Hour}

type Result struct {
	RefreshTokens int64 `json:"refresh_tokens"`
	AuthSessions  int64 `json:"auth_sessions"`
	MFAChallenges int64 `json:"mfa_challenges"`
	WebhookEvents int64 `json:"webhook_events"`
}

type Service struct {
	sessions SessionPurger
	webhooks WebhookPurger
	audit    audit.Recorder
	policy   Policy
	now      func() time.Time
}

func NewService(sessions SessionPurger, webhooks WebhookPurger, rec audit.Recorder, policy Policy) *Service {
	return &Service{
		sessions: sessions, webhooks: webhooks, audit: rec, policy: policy,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Run purges everything past retention; actorID is uuid.Nil for jobs.
func (s *Service) Run(ctx context.Context, actorID uuid.UUID) (Result, error) {
	now := s.now()
	var res Result
	var err error
	if res.AuthSessions, res.RefreshTokens, err = s.sessions.PurgeSessions(ctx, now.Add(-s.policy.Sessions)); err != nil {
		return res, err
	}
	if res.MFAChallenges, err = s.sessions.PurgeExpiredChallenges(ctx, now); err != nil {
		return res, err
	}
	if res.WebhookEvents, err = s.webhooks.PurgeWebhookEvents(ctx, now.Add(-s.policy.WebhookEvents)); err != nil {
		return res, err
	}
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: actorID, Action: "ops.security_cleanup", EntityType: "security_retention",
		Extra: map[string]any{
			"refresh_tokens": res.RefreshTokens, "auth_sessions": res.AuthSessions,
			"mfa_challenges": res.MFAChallenges, "webhook_events": res.WebhookEvents,
		},
	})
	return res, nil
}
