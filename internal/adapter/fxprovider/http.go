// Package fxprovider holds optional FX rate provider adapters.
package fxprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
)

const maxBody = 1 << 20

// HTTP fetches rates from a JSON endpoint:
//
//	GET {url}?base=USD&date=2026-09-27
//	{"base":"USD","date":"2026-09-27","rates":{"SAR":"3.75","EUR":0.92}}
//
// Rates are parsed as decimal text (never floats); entries with more than
// eight decimals or non-positive values are skipped.
type HTTP struct {
	endpoint string
	base     string
	client   *http.Client
}

func NewHTTP(endpoint, base string, timeout time.Duration) *HTTP {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &HTTP{endpoint: endpoint, base: strings.ToUpper(strings.TrimSpace(base)), client: &http.Client{Timeout: timeout}}
}

func (p *HTTP) Name() string { return "provider" }

type payload struct {
	Base  string                     `json:"base"`
	Date  string                     `json:"date"`
	Rates map[string]json.RawMessage `json:"rates"`
}

func (p *HTTP) Fetch(ctx context.Context, on time.Time) ([]domain.Rate, error) {
	u, err := url.Parse(p.endpoint)
	if err != nil {
		return nil, fmt.Errorf("fx provider url: %w", err)
	}
	q := u.Query()
	q.Set("base", p.base)
	q.Set("date", on.Format(time.DateOnly))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fx provider: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fx provider: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("fx provider: %w", err)
	}
	var body payload
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("fx provider: decode: %w", err)
	}
	base := body.Base
	if base == "" {
		base = p.base
	}
	eff := on
	if body.Date != "" {
		if eff, err = domain.ParseDate(body.Date); err != nil {
			return nil, fmt.Errorf("fx provider: date %q", body.Date)
		}
	}
	out := make([]domain.Rate, 0, len(body.Rates))
	for quote, v := range body.Rates {
		scaled, err := domain.ParseRate(string(bytes.Trim(v, `"`)))
		if err != nil {
			continue
		}
		out = append(out, domain.Rate{Base: base, Quote: quote, Scaled: scaled, EffectiveDate: eff, Source: p.Name()})
	}
	return out, nil
}
