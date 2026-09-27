// Package fxlive holds live exchange rate feeds (domain fx.LiveSource).
package fxlive

import (
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

const (
	maxBody        = 1 << 20
	userAgent      = "wodi-crm/1.0"
	defaultTimeout = 10 * time.Second
)

func newClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// getJSON decodes a 200 JSON response into out; headers are optional extras.
func getJSON(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", redact(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if len(raw) > maxBody {
		return fmt.Errorf("response larger than %d bytes", maxBody)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// redact drops the URL from transport errors; it is stored and shown to users.
func redact(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}

// siteURL is the scheme and host of base, the public link shown for a source.
func siteURL(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return strings.TrimRight(base, "/")
	}
	return u.Scheme + "://" + u.Host
}

// parseNumber reads a JSON number (or a quoted one) as exact decimal text.
func parseNumber(raw json.RawMessage) (int64, error) {
	return domain.ParseDecimal(strings.Trim(string(raw), `"`))
}

// parseTime accepts RFC 3339 with any fractional precision, or the same
// layout without an offset (read as UTC).
func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("2006-01-02T15:04:05.999999999", s); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}
