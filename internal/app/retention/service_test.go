package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
)

type fakePurger struct {
	sessionCutoff, challengeNow, webhookCutoff time.Time
	err                                        error
}

func (f *fakePurger) PurgeSessions(_ context.Context, cutoff time.Time) (int64, int64, error) {
	f.sessionCutoff = cutoff
	return 2, 5, f.err
}

func (f *fakePurger) PurgeExpiredChallenges(_ context.Context, now time.Time) (int64, error) {
	f.challengeNow = now
	return 3, nil
}

func (f *fakePurger) PurgeWebhookEvents(_ context.Context, cutoff time.Time) (int64, error) {
	f.webhookCutoff = cutoff
	return 7, nil
}

type fakeAudit struct{ actions []string }

func (f *fakeAudit) Record(_ context.Context, in audit.RecordInput) error {
	f.actions = append(f.actions, in.Action)
	return nil
}

func TestRunAppliesRetentionWindows(t *testing.T) {
	p, au := &fakePurger{}, &fakeAudit{}
	svc := NewService(p, p, au, DefaultPolicy)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	res, err := svc.Run(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if res != (Result{RefreshTokens: 5, AuthSessions: 2, MFAChallenges: 3, WebhookEvents: 7}) {
		t.Fatalf("result: %+v", res)
	}
	if !p.sessionCutoff.Equal(now.AddDate(0, 0, -30)) || !p.webhookCutoff.Equal(now.AddDate(0, 0, -90)) || !p.challengeNow.Equal(now) {
		t.Fatalf("cutoffs: sessions=%s webhooks=%s challenges=%s", p.sessionCutoff, p.webhookCutoff, p.challengeNow)
	}
	if len(au.actions) != 1 || au.actions[0] != "ops.security_cleanup" {
		t.Fatalf("audit: %v", au.actions)
	}
}

func TestRunStopsOnError(t *testing.T) {
	p := &fakePurger{err: errors.New("db down")}
	if _, err := NewService(p, p, &fakeAudit{}, DefaultPolicy).Run(context.Background(), uuid.Nil); err == nil {
		t.Fatal("purge failure must surface")
	}
	if !p.webhookCutoff.IsZero() {
		t.Fatal("later purges must not run after a failure")
	}
}
