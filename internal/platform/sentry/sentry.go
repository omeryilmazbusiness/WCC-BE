package sentry

import (
	"net/http"
	"strings"
	"time"

	sdk "github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
)

const flushTimeout = 2 * time.Second

var sensitiveHeaders = map[string]struct{}{
	"authorization":        {},
	"cookie":               {},
	"set-cookie":           {},
	"x-api-key":            {},
	"x-api-secret":         {},
	"x-amz-security-token": {},
	"proxy-authorization":  {},
}

var sensitiveQuery = map[string]struct{}{
	"token":         {},
	"access_token":  {},
	"refresh":       {},
	"refresh_token": {},
	"password":      {},
	"passport":      {},
	"secret":        {},
	"code":          {},
}

// Init starts the Sentry SDK. An empty DSN leaves reporting off.
func Init(cfg config.Config) error {
	if strings.TrimSpace(cfg.Sentry.DSN) == "" {
		return nil
	}
	rate := cfg.Sentry.TracesSampleRate
	return sdk.Init(sdk.ClientOptions{
		Dsn:              cfg.Sentry.DSN,
		Environment:      cfg.App.Env,
		Release:          cfg.App.Name + "@" + cfg.App.Version,
		ServerName:       cfg.App.Name,
		EnableTracing:    true,
		TracesSampleRate: rate,
		TracesSampler: func(ctx sdk.SamplingContext) float64 {
			if ctx.Span != nil {
				switch ctx.Span.Name {
				case "GET /healthz", "GET /readyz":
					return 0
				}
			}
			return rate
		},
		SendDefaultPII: false,
		BeforeSend:     scrubEvent,
	})
}

// Flush waits briefly for buffered events on shutdown.
func Flush() {
	sdk.Flush(flushTimeout)
}

// HTTP recovers panics into Sentry, then re-panics so Recoverer can answer 500.
func HTTP() func(http.Handler) http.Handler {
	return sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle
}

func scrubEvent(event *sdk.Event, _ *sdk.EventHint) *sdk.Event {
	if event == nil {
		return nil
	}
	if event.Request != nil {
		event.Request.Cookies = ""
		event.Request.Data = ""
		if event.Request.Headers != nil {
			for k := range event.Request.Headers {
				if _, sensitive := sensitiveHeaders[strings.ToLower(k)]; sensitive {
					event.Request.Headers[k] = "[Filtered]"
				}
			}
		}
		if q := event.Request.QueryString; q != "" {
			event.Request.QueryString = scrubQuery(q)
		}
	}
	return event
}

func scrubQuery(q string) string {
	parts := strings.Split(q, "&")
	for i, part := range parts {
		key, _, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if _, sensitive := sensitiveQuery[strings.ToLower(key)]; sensitive {
			parts[i] = key + "=[Filtered]"
		}
	}
	return strings.Join(parts, "&")
}
