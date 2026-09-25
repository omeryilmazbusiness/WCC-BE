package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
)

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func defaultClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}

// Registry maps provider → CompletionProvider (OCP).
type Registry struct {
	byName map[domain.Provider]domain.CompletionProvider
}

func NewRegistry(providers ...domain.CompletionProvider) *Registry {
	r := &Registry{byName: map[domain.Provider]domain.CompletionProvider{}}
	for _, p := range providers {
		r.byName[p.Name()] = p
	}
	return r
}

func (r *Registry) Get(p domain.Provider) (domain.CompletionProvider, bool) {
	v, ok := r.byName[p]
	return v, ok
}

// --- OpenAI ---

type OpenAI struct {
	Client httpDoer
	Base   string
}

func NewOpenAI() *OpenAI {
	return &OpenAI{Client: defaultClient(), Base: "https://api.openai.com/v1"}
}

func (o *OpenAI) Name() domain.Provider { return domain.ProviderOpenAI }

func (o *OpenAI) Complete(ctx context.Context, apiKey string, req domain.CompletionRequest) (*domain.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = domain.DefaultModel(domain.ProviderOpenAI)
	}
	messages := []map[string]any{}
	if req.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": req.System})
	}
	if req.ImageB64 != "" {
		mime := req.ImageMIME
		if mime == "" {
			mime = "image/jpeg"
		}
		messages = append(messages, map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": req.User},
				map[string]any{"type": "image_url", "image_url": map[string]any{
					"url": "data:" + mime + ";base64," + req.ImageB64,
				}},
			},
		})
	} else {
		messages = append(messages, map[string]any{"role": "user", "content": req.User})
	}
	body := map[string]any{
		"model": model, "messages": messages,
		"max_tokens": maxTok(req.MaxTokens),
	}
	if req.JSONMode {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	raw, err := postJSON(ctx, o.Client, o.Base+"/chat/completions", apiKey, body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	text := ""
	if len(parsed.Choices) > 0 {
		text = parsed.Choices[0].Message.Content
	}
	return &domain.CompletionResponse{Text: text, Model: parsed.Model, Raw: raw}, nil
}

// --- Anthropic (Claude) ---

type Anthropic struct {
	Client httpDoer
	Base   string
}

func NewAnthropic() *Anthropic {
	return &Anthropic{Client: defaultClient(), Base: "https://api.anthropic.com/v1"}
}

func (a *Anthropic) Name() domain.Provider { return domain.ProviderAnthropic }

func (a *Anthropic) Complete(ctx context.Context, apiKey string, req domain.CompletionRequest) (*domain.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = domain.DefaultModel(domain.ProviderAnthropic)
	}
	var content any
	if req.ImageB64 != "" {
		mime := req.ImageMIME
		if mime == "" {
			mime = "image/jpeg"
		}
		content = []any{
			map[string]any{"type": "text", "text": req.User},
			map[string]any{"type": "image", "source": map[string]any{
				"type": "base64", "media_type": mime, "data": req.ImageB64,
			}},
		}
	} else {
		content = req.User
	}
	body := map[string]any{
		"model": model, "max_tokens": maxTok(req.MaxTokens),
		"messages": []map[string]any{{"role": "user", "content": content}},
	}
	if req.System != "" {
		body["system"] = req.System
	}
	raw, err := postJSONHeaders(ctx, a.Client, a.Base+"/messages", map[string]string{
		"x-api-key": apiKey, "anthropic-version": "2023-06-01",
		"Content-Type": "application/json",
	}, body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Model    string `json:"model"`
		Content  []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	text := ""
	for _, c := range parsed.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return &domain.CompletionResponse{Text: text, Model: parsed.Model, Raw: raw}, nil
}

// --- Google Gemini ---

type Gemini struct {
	Client httpDoer
	Base   string
}

func NewGemini() *Gemini {
	return &Gemini{Client: defaultClient(), Base: "https://generativelanguage.googleapis.com/v1beta"}
}

func (g *Gemini) Name() domain.Provider { return domain.ProviderGemini }

func (g *Gemini) Complete(ctx context.Context, apiKey string, req domain.CompletionRequest) (*domain.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = domain.DefaultModel(domain.ProviderGemini)
	}
	parts := []map[string]any{}
	if req.System != "" {
		parts = append(parts, map[string]any{"text": req.System + "\n\n" + req.User})
	} else {
		parts = append(parts, map[string]any{"text": req.User})
	}
	if req.ImageB64 != "" {
		mime := req.ImageMIME
		if mime == "" {
			mime = "image/jpeg"
		}
		parts = append(parts, map[string]any{
			"inline_data": map[string]any{"mime_type": mime, "data": req.ImageB64},
		})
	}
	body := map[string]any{
		"contents": []map[string]any{{"parts": parts}},
		"generationConfig": map[string]any{
			"maxOutputTokens": maxTok(req.MaxTokens),
		},
	}
	if req.JSONMode {
		body["generationConfig"].(map[string]any)["responseMimeType"] = "application/json"
	}
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", g.Base, model, apiKey)
	raw, err := postJSONHeaders(ctx, g.Client, url, map[string]string{"Content-Type": "application/json"}, body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	text := ""
	if len(parsed.Candidates) > 0 {
		for _, p := range parsed.Candidates[0].Content.Parts {
			text += p.Text
		}
	}
	return &domain.CompletionResponse{Text: text, Model: model, Raw: raw}, nil
}

func maxTok(n int) int {
	if n <= 0 {
		return 1024
	}
	return n
}

func postJSON(ctx context.Context, client httpDoer, url, bearer string, body any) (json.RawMessage, error) {
	return postJSONHeaders(ctx, client, url, map[string]string{
		"Authorization": "Bearer " + bearer,
		"Content-Type":  "application/json",
	}, body)
}

func postJSONHeaders(ctx context.Context, client httpDoer, url string, headers map[string]string, body any) (json.RawMessage, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 400 {
			msg = msg[:400]
		}
		return nil, fmt.Errorf("provider http %d: %s", res.StatusCode, msg)
	}
	return json.RawMessage(raw), nil
}
