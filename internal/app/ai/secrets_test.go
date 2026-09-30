package ai_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/ai"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
)

type settingsRepo struct {
	domain.Repository
	rows map[uuid.UUID]domain.Settings
}

func (r *settingsRepo) GetSettings(_ context.Context, branchID uuid.UUID) (*domain.Settings, error) {
	s, ok := r.rows[branchID]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (r *settingsRepo) UpsertSettings(_ context.Context, s *domain.Settings) error {
	r.rows[s.BranchID] = *s
	return nil
}

func testBox(t *testing.T) *crypto.SecretBox {
	t.Helper()
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	k, err := crypto.NewKeyring("k1", base64.StdEncoding.EncodeToString(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	return crypto.NewSecretBox(k)
}

func TestCompleteSetupSealsAPIKey(t *testing.T) {
	repo := &settingsRepo{rows: map[uuid.UUID]domain.Settings{}}
	svc := ai.NewService(repo, registry{p: &fakeProvider{reply: "OK"}}, testBox(t))
	branch := uuid.New()
	ctx := context.Background()

	out, err := svc.CompleteSetup(ctx, ai.SetupInput{BranchID: branch, Provider: domain.ProviderOpenAI, APIKey: "sk-live-abcd9876", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if out["key_hint"] != "••••9876" || out["configured"] != true {
		t.Fatalf("setup view: %v", out)
	}
	stored := repo.rows[branch]
	if strings.Contains(string(stored.ConfigJSON), "sk-live") || strings.Contains(stored.SecretsEnc, "sk-live") || stored.SecretsEnc == "" {
		t.Fatalf("api key stored in clear: config=%s enc=%q", stored.ConfigJSON, stored.SecretsEnc)
	}

	// Re-running setup without a key keeps the sealed one.
	if _, err := svc.CompleteSetup(ctx, ai.SetupInput{BranchID: branch, Provider: domain.ProviderOpenAI, Model: "gpt-x", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	view, _ := svc.GetSetup(ctx, branch)
	if view["key_hint"] != "••••9876" || view["model"] != "gpt-x" {
		t.Fatalf("after model change: %v", view)
	}
}

func TestLegacyPlaintextKeyStillReadable(t *testing.T) {
	branch := uuid.New()
	cfg, _ := json.Marshal(map[string]string{"api_key": "sk-legacy-1111"})
	repo := &settingsRepo{rows: map[uuid.UUID]domain.Settings{
		branch: {BranchID: branch, Provider: domain.ProviderOpenAI, Enabled: true, ConfigJSON: cfg},
	}}
	svc := ai.NewService(repo, nil, testBox(t))
	view, err := svc.GetSetup(context.Background(), branch)
	if err != nil || view["key_hint"] != "••••1111" {
		t.Fatalf("legacy view: %v %v", view, err)
	}
}
