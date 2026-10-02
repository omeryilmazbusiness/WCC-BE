package travelpayouts

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	airlinesTTL   = 24 * time.Hour
	airlinesRetry = 5 * time.Minute
)

// Airlines names carriers from the provider's static catalog, loaded on
// first use and refreshed daily; a failed load is retried after a pause and
// never fails a search (unknown names are simply empty).
type Airlines struct {
	cfg Config
	t   transport
	log *slog.Logger
	now func() time.Time

	mu       sync.Mutex
	names    map[string]string
	loadedAt time.Time
	failedAt time.Time
}

func NewAirlines(cfg Config, log *slog.Logger) *Airlines {
	cfg = cfg.withDefaults()
	if log == nil {
		log = slog.Default()
	}
	return &Airlines{cfg: cfg, t: newTransport(cfg), log: log, now: time.Now}
}

type airlineRow struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (a *Airlines) AirlineName(ctx context.Context, code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	stale := a.names == nil || now.Sub(a.loadedAt) > airlinesTTL
	if stale && now.Sub(a.failedAt) > airlinesRetry {
		a.load(ctx, now)
	}
	return a.names[code]
}

func (a *Airlines) load(ctx context.Context, now time.Time) {
	// One search giving up must not leave everyone without names until the retry.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.Timeout)
	defer cancel()
	var rows []airlineRow
	if err := a.t.getJSON(ctx, a.cfg.APIURL+"/data/en/airlines.json", false, &rows); err != nil {
		a.failedAt = now
		a.log.Warn("flight airline catalog unavailable", "err", err)
		return
	}
	names := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.Code != "" && r.Name != "" {
			names[strings.ToUpper(r.Code)] = r.Name
		}
	}
	a.names, a.loadedAt = names, now
}
