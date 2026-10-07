package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
)

func TestGeminiSendsKeyInHeaderAndReturnsTypedErrors(t *testing.T) {
	var gotPath, gotKey, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotQuery = r.URL.Path, r.Header.Get("x-goog-api-key"), r.URL.RawQuery
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"models/test is not found"}}`))
	}))
	defer srv.Close()

	g := &Gemini{Client: srv.Client(), Base: srv.URL}
	_, err := g.Complete(context.Background(), "AIza-secret", domain.CompletionRequest{Model: "test", User: "hi"})

	var pe *domain.ProviderError
	if !errors.As(err, &pe) || pe.Status != http.StatusNotFound {
		t.Fatalf("want ProviderError 404, got %v", err)
	}
	if gotPath != "/models/test:generateContent" || gotKey != "AIza-secret" || strings.Contains(gotQuery, "AIza") {
		t.Fatalf("path=%q key=%q query=%q", gotPath, gotKey, gotQuery)
	}
}

func TestTransientFailuresAreRetriedButClientErrorsAreNot(t *testing.T) {
	saved := retryBackoff
	retryBackoff = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { retryBackoff = saved }()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case r.URL.Path == "/models/busy:generateContent" && calls == 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case r.URL.Path == "/models/busy:generateContent":
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"OK"}]}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	g := &Gemini{Client: srv.Client(), Base: srv.URL}

	out, err := g.Complete(context.Background(), "k", domain.CompletionRequest{Model: "busy", User: "hi"})
	if err != nil || out.Text != "OK" || calls != 2 {
		t.Fatalf("503 then OK: out=%v err=%v calls=%d", out, err, calls)
	}

	calls = 0
	if _, err := g.Complete(context.Background(), "k", domain.CompletionRequest{Model: "missing", User: "hi"}); err == nil || calls != 1 {
		t.Fatalf("404 must not retry: err=%v calls=%d", err, calls)
	}
}

func TestOpenAIUsesCompletionTokenLimit(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer srv.Close()

	o := &OpenAI{Client: srv.Client(), Base: srv.URL}
	out, err := o.Complete(context.Background(), "sk", domain.CompletionRequest{Model: "m", User: "hi", MaxTokens: 8})
	if err != nil || out.Text != "OK" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if _, legacy := body["max_tokens"]; legacy || body["max_completion_tokens"] != float64(8+reasoningHeadroom) {
		t.Fatalf("token limit fields: %v", body)
	}
}

func TestLowReasoningTuning(t *testing.T) {
	for model, want := range map[string]bool{"o3-mini": true, "gpt-5-mini": true, "gpt-6-luna": true, "gpt-4o": false, "omni": false} {
		if openAIReasons(model) != want {
			t.Fatalf("openAIReasons(%s) = %v", model, !want)
		}
	}
	if geminiThinking("gemini-2.5-flash")["thinkingBudget"] != 0 || geminiThinking("gemini-2.5-pro")["thinkingBudget"] != 128 {
		t.Fatal("gemini 2.5 budgets")
	}
	if geminiThinking("gemini-3.5-flash")["thinkingLevel"] != "low" || geminiThinking("gemini-2.0-flash") != nil {
		t.Fatal("gemini 3 levels, 2.0 none")
	}

	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		if cfg, _ := b["generationConfig"].(map[string]any); cfg["thinkingConfig"] != nil && strings.Contains(r.URL.Path, "strict") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"thinking not supported"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"OK"}]}}]}`))
	}))
	defer srv.Close()
	g := &Gemini{Client: srv.Client(), Base: srv.URL}

	out, err := g.Complete(context.Background(), "k", domain.CompletionRequest{Model: "gemini-3.5-flash", User: "hi", MaxTokens: 200, LowReasoning: true})
	cfg := bodies[0]["generationConfig"].(map[string]any)
	if err != nil || out.Text != "OK" || cfg["thinkingConfig"] == nil || cfg["maxOutputTokens"] == nil {
		t.Fatalf("thinking config sent with the output cap: %v %+v", err, bodies[0])
	}

	bodies = nil
	out, err = g.Complete(context.Background(), "k", domain.CompletionRequest{Model: "gemini-9-strict", User: "hi", LowReasoning: true})
	if err != nil || out.Text != "OK" || len(bodies) != 2 {
		t.Fatalf("a rejected tuning retries once without it: %v calls=%d", err, len(bodies))
	}
	if cfg := bodies[1]["generationConfig"].(map[string]any); cfg["thinkingConfig"] != nil {
		t.Fatal("retry is the plain body")
	}

	bodies = nil
	_, _ = g.Complete(context.Background(), "k", domain.CompletionRequest{Model: "gemini-3.5-flash", User: "hi"})
	if cfg := bodies[0]["generationConfig"].(map[string]any); cfg["thinkingConfig"] != nil {
		t.Fatal("other features keep the provider default")
	}
}
