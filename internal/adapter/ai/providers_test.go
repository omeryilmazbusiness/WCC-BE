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
