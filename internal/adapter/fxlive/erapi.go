package fxlive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
)

// ERAPIID is the source id stored with ExchangeRate-API snapshots.
const ERAPIID = "exchangerate-api"

// ERAPI reads international cross rates (units per 1 USD) from the
// ExchangeRate-API open access endpoint:
//
//	GET {base}/latest/USD
//	{"result":"success","time_last_update_unix":...,"time_next_update_unix":...,"rates":{"SYP":121.843708,"EUR":0.877506}}
//
// Open access data must be shown with the "Rates By Exchange Rate API"
// attribution linking to exchangerate-api.com.
type ERAPI struct {
	baseURL string
	client  *http.Client
	now     func() time.Time
}

func NewERAPI(baseURL string, timeout time.Duration) *ERAPI {
	return &ERAPI{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), client: newClient(timeout), now: time.Now}
}

// Data changes once a day and the open endpoint rate-limits frequent polling.
func (e *ERAPI) Info() domain.LiveSourceInfo {
	return domain.LiveSourceInfo{
		ID: ERAPIID, Name: "ExchangeRate-API", URL: "https://www.exchangerate-api.com",
		Attribution: "Rates By Exchange Rate API",
		Kinds:       []domain.SourceKind{domain.KindReference},
		MinInterval: 6 * time.Hour,
	}
}

type erapiPayload struct {
	Result     string                     `json:"result"`
	ErrorType  string                     `json:"error-type"`
	BaseCode   string                     `json:"base_code"`
	LastUpdate json.Number                `json:"time_last_update_unix"`
	NextUpdate json.Number                `json:"time_next_update_unix"`
	Rates      map[string]json.RawMessage `json:"rates"`
}

func (e *ERAPI) Fetch(ctx context.Context) ([]domain.SourceSnapshot, error) {
	var body erapiPayload
	if err := getJSON(ctx, e.client, e.baseURL+"/latest/USD", nil, &body); err != nil {
		return nil, fmt.Errorf("exchangerate-api: %w", err)
	}
	if body.Result != "success" {
		return nil, fmt.Errorf("exchangerate-api: result %q %s", body.Result, body.ErrorType)
	}
	if body.BaseCode != "" && !strings.EqualFold(body.BaseCode, "USD") {
		return nil, fmt.Errorf("exchangerate-api: unexpected base %q", body.BaseCode)
	}
	now := e.now().UTC()
	observed, ok := unixTime(body.LastUpdate)
	if !ok {
		return nil, fmt.Errorf("exchangerate-api: missing time_last_update_unix")
	}
	next, _ := unixTime(body.NextUpdate)

	quotes := make([]domain.Quote, 0, len(body.Rates)+1)
	quotes = append(quotes, domain.Quote{Currency: "USD", Mid: domain.RateScale, ObservedAt: observed})
	for code, raw := range body.Rates {
		cur, err := domain.NormalizeCurrency(code)
		if err != nil || cur != code || cur == "USD" {
			continue
		}
		mid, err := parseNumber(raw)
		if err != nil {
			continue
		}
		quotes = append(quotes, domain.Quote{Currency: cur, Mid: mid, ObservedAt: observed})
	}
	return []domain.SourceSnapshot{{
		Source: ERAPIID, Kind: domain.KindReference, Quotes: quotes, FetchedAt: now, NextUpdateAt: next, OK: true,
	}}, nil
}

func unixTime(n json.Number) (time.Time, bool) {
	sec, err := n.Int64()
	if err != nil || sec <= 0 {
		return time.Time{}, false
	}
	return time.Unix(sec, 0).UTC(), true
}
