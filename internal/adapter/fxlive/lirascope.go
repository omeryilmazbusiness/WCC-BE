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

// LiraScopeID is the source id stored with LiraScope snapshots.
const LiraScopeID = "lirascope"

type LiraScopeConfig struct {
	// BaseURL is the API root, e.g. https://lirascope.syria-cloud.sy/api/v1.
	BaseURL string
	// APIKey/APISecret are optional; headers are sent only when set.
	APIKey    string
	APISecret string
	Timeout   time.Duration
}

// LiraScope reads Central Bank of Syria (official) and Damascus parallel
// market rates in new SYP per unit:
//
//	GET {base}/rates/latest?lang=en
//	{"timestampUtc":"...","cbsRates":[{"currency":"USD","buy":121.5,"sell":122.5,"mid":122,"timestampUtc":"..."}],
//	 "marketRates":[...]}
//
// Numbers are read as decimal text; entries that do not parse are skipped.
type LiraScope struct {
	cfg    LiraScopeConfig
	client *http.Client
	now    func() time.Time
}

func NewLiraScope(cfg LiraScopeConfig) *LiraScope {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	return &LiraScope{cfg: cfg, client: newClient(cfg.Timeout), now: time.Now}
}

// The public API allows 60 requests per minute and 1200 per hour per IP.
func (l *LiraScope) Info() domain.LiveSourceInfo {
	return domain.LiveSourceInfo{
		ID: LiraScopeID, Name: "LiraScope", URL: siteURL(l.cfg.BaseURL),
		Kinds:       []domain.SourceKind{domain.KindOfficial, domain.KindMarket},
		MinInterval: 5 * time.Minute,
	}
}

type liraRate struct {
	Currency     string          `json:"currency"`
	Buy          json.RawMessage `json:"buy"`
	Sell         json.RawMessage `json:"sell"`
	Mid          json.RawMessage `json:"mid"`
	TimestampUTC string          `json:"timestampUtc"`
}

type liraPayload struct {
	TimestampUTC string     `json:"timestampUtc"`
	CBSRates     []liraRate `json:"cbsRates"`
	MarketRates  []liraRate `json:"marketRates"`
}

func (l *LiraScope) Fetch(ctx context.Context) ([]domain.SourceSnapshot, error) {
	headers := map[string]string{}
	if l.cfg.APIKey != "" {
		headers["X-Api-Key"] = l.cfg.APIKey
	}
	if l.cfg.APISecret != "" {
		headers["X-Api-Secret"] = l.cfg.APISecret
	}
	var body liraPayload
	if err := getJSON(ctx, l.client, l.cfg.BaseURL+"/rates/latest?lang=en", headers, &body); err != nil {
		return nil, fmt.Errorf("lirascope: %w", err)
	}
	now := l.now().UTC()
	fallback, ok := parseTime(body.TimestampUTC)
	if !ok {
		fallback = now
	}
	return []domain.SourceSnapshot{
		{Source: LiraScopeID, Kind: domain.KindOfficial, Quotes: liraQuotes(body.CBSRates, fallback), FetchedAt: now, OK: true},
		{Source: LiraScopeID, Kind: domain.KindMarket, Quotes: liraQuotes(body.MarketRates, fallback), FetchedAt: now, OK: true},
	}, nil
}

func liraQuotes(rates []liraRate, fallback time.Time) []domain.Quote {
	out := make([]domain.Quote, 0, len(rates))
	seen := map[string]bool{}
	for _, r := range rates {
		cur, err := domain.NormalizeCurrency(r.Currency)
		if err != nil || seen[cur] {
			continue
		}
		buy, errB := parseNumber(r.Buy)
		sell, errS := parseNumber(r.Sell)
		mid, errM := parseNumber(r.Mid)
		if errB != nil || errS != nil || errM != nil {
			continue
		}
		observed, ok := parseTime(r.TimestampUTC)
		if !ok {
			observed = fallback
		}
		seen[cur] = true
		out = append(out, domain.Quote{Currency: cur, Buy: buy, Sell: sell, Mid: mid, ObservedAt: observed})
	}
	return out
}
