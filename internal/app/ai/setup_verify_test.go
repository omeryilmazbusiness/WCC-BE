package ai_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/ai"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestCompleteSetupRejectsCredentialsTheProviderRefuses(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		code  string
		field string
	}{
		{"unknown model", &domain.ProviderError{Status: 404, Body: `{"error":{"message":"models/test is not found for API version v1beta"}}`}, ai.CodeModelNotFound, "model"},
		{"bad key", &domain.ProviderError{Status: 401, Body: `{"error":"invalid x-api-key"}`}, ai.CodeKeyInvalid, "api_key"},
		{"gemini bad key", &domain.ProviderError{Status: 400, Body: `{"error":{"message":"API key not valid. Please pass a valid API key."}}`}, ai.CodeKeyInvalid, "api_key"},
		{"server error", &domain.ProviderError{Status: 500, Body: `internal`}, ai.CodeProviderError, ""},
		{"network", errors.New("dial tcp: timeout"), ai.CodeProviderError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingsRepo{rows: map[uuid.UUID]domain.Settings{}}
			p := &fakeProvider{err: tc.err}
			svc := ai.NewService(repo, registry{p: p}, testBox(t))
			branch := uuid.New()

			_, err := svc.CompleteSetup(context.Background(), ai.SetupInput{
				BranchID: branch, Provider: domain.ProviderGemini, APIKey: "AIza-test-0000", Model: "test", Enabled: true,
			})
			var app *shared.AppError
			if !errors.As(err, &app) || !strings.HasPrefix(app.Message, tc.code+":") {
				t.Fatalf("want %s, got %v", tc.code, err)
			}
			if tc.field != "" && app.Details[tc.field] != tc.code {
				t.Fatalf("details: %v", app.Details)
			}
			if _, saved := repo.rows[branch]; saved {
				t.Fatal("refused credentials must not be saved")
			}
			if p.req.Model != "test" {
				t.Fatalf("verified model %q", p.req.Model)
			}
		})
	}
}

func TestCompleteSetupUsesProviderDefaultModel(t *testing.T) {
	repo := &settingsRepo{rows: map[uuid.UUID]domain.Settings{}}
	p := &fakeProvider{reply: "OK"}
	svc := ai.NewService(repo, registry{p: p}, testBox(t))
	branch := uuid.New()

	out, err := svc.CompleteSetup(context.Background(), ai.SetupInput{
		BranchID: branch, Provider: domain.ProviderGemini, APIKey: "AIza-test-0000", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.DefaultModel(domain.ProviderGemini)
	if out["model"] != want || p.req.Model != want {
		t.Fatalf("model: view=%v verified=%q want %q", out["model"], p.req.Model, want)
	}
}

func TestDisabledSetupSkipsVerification(t *testing.T) {
	repo := &settingsRepo{rows: map[uuid.UUID]domain.Settings{}}
	p := &fakeProvider{err: &domain.ProviderError{Status: 401}}
	svc := ai.NewService(repo, registry{p: p}, testBox(t))

	if _, err := svc.CompleteSetup(context.Background(), ai.SetupInput{
		BranchID: uuid.New(), Provider: domain.ProviderOpenAI, APIKey: "sk-test-0000", Enabled: false,
	}); err != nil {
		t.Fatalf("disabled setup should save without a provider call: %v", err)
	}
}

func TestRuntimeProviderFailureIsClassified(t *testing.T) {
	p := &fakeProvider{err: &domain.ProviderError{Status: 404, Body: "model not found"}}
	svc, _, branch := lostFixture(t, true, p, lostSample)

	_, err := svc.LostLeadsAnalysis(context.Background(), branch, uuid.New(), weekFrom, weekTo)
	if err == nil || !strings.HasPrefix(err.Error(), ai.CodeModelNotFound) {
		t.Fatalf("want %s, got %v", ai.CodeModelNotFound, err)
	}
}

func TestThrottledProviderStillSavesTheKey(t *testing.T) {
	for _, status := range []int{429, 503, 529} {
		t.Run(fmt.Sprint(status), func(t *testing.T) { throttledSaves(t, status) })
	}
}

func TestSlowProviderStillSavesTheKey(t *testing.T) {
	repo := &settingsRepo{rows: map[uuid.UUID]domain.Settings{}}
	p := &fakeProvider{err: fmt.Errorf("post: %w", context.DeadlineExceeded)}
	svc := ai.NewService(repo, registry{p: p}, testBox(t))
	out, err := svc.CompleteSetup(context.Background(), ai.SetupInput{
		BranchID: uuid.New(), Provider: domain.ProviderGemini, APIKey: "AIza-test-0000", Enabled: true,
	})
	if err != nil || out["verification"] != ai.VerificationThrottled {
		t.Fatalf("timeout: out=%v err=%v", out, err)
	}
}

func throttledSaves(t *testing.T, status int) {
	repo := &settingsRepo{rows: map[uuid.UUID]domain.Settings{}}
	p := &fakeProvider{err: &domain.ProviderError{Status: status, Body: `busy`}}
	svc := ai.NewService(repo, registry{p: p}, testBox(t))
	branch := uuid.New()

	out, err := svc.CompleteSetup(context.Background(), ai.SetupInput{
		BranchID: branch, Provider: domain.ProviderGemini, APIKey: "AIza-test-0000", Enabled: true,
	})
	if err != nil {
		t.Fatalf("a 429 means the key was accepted; setup must save: %v", err)
	}
	if out["verification"] != ai.VerificationThrottled || out["enabled"] != true {
		t.Fatalf("out: %v", out)
	}
	if _, saved := repo.rows[branch]; !saved {
		t.Fatal("throttled key not saved")
	}
}

func TestKeepingTheStoredKey(t *testing.T) {
	repo := &settingsRepo{rows: map[uuid.UUID]domain.Settings{}}
	p := &fakeProvider{reply: "OK"}
	svc := ai.NewService(repo, registry{p: p}, testBox(t))
	branch := uuid.New()
	ctx := context.Background()
	if _, err := svc.CompleteSetup(ctx, ai.SetupInput{BranchID: branch, Provider: domain.ProviderGemini, APIKey: "AIza-test-0000", Model: "gemini-a", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	// Same provider and model, blank key: nothing new to verify, even while the provider throttles.
	p.err, p.req = &domain.ProviderError{Status: 429}, domain.CompletionRequest{}
	out, err := svc.CompleteSetup(ctx, ai.SetupInput{BranchID: branch, Provider: domain.ProviderGemini, Model: "gemini-a", Enabled: true})
	if err != nil || out["verification"] != ai.VerificationSkipped || p.req.Model != "" {
		t.Fatalf("keep: out=%v err=%v called=%q", out, err, p.req.Model)
	}
	if out["key_hint"] == "" {
		t.Fatal("stored key lost")
	}

	// A new model with the kept key is checked.
	p.err = nil
	out, err = svc.CompleteSetup(ctx, ai.SetupInput{BranchID: branch, Provider: domain.ProviderGemini, Model: "gemini-b", Enabled: true})
	if err != nil || out["verification"] != ai.VerificationPassed || p.req.Model != "gemini-b" {
		t.Fatalf("model change: out=%v err=%v called=%q", out, err, p.req.Model)
	}

	// Another provider never inherits this key.
	_, err = svc.CompleteSetup(ctx, ai.SetupInput{BranchID: branch, Provider: domain.ProviderOpenAI, Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "api_key is required") {
		t.Fatalf("provider switch without a key: %v", err)
	}
}
