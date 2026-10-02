// Package travelpayouts implements the flight finder ports (domain flight)
// on the Travelpayouts / Aviasales Data API.
package travelpayouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/flight"
)

const (
	maxBody        = 4 << 20
	userAgent      = "wodi-crm/1.0"
	defaultTimeout = 10 * time.Second
)

// ErrUnauthorized means the API token is missing or rejected.
var ErrUnauthorized = domain.ErrProviderRejected

// Config points the clients at the provider; empty URLs use the public hosts.
type Config struct {
	APIURL          string // https://api.travelpayouts.com
	AutocompleteURL string // https://autocomplete.travelpayouts.com
	Token           string
	// Market selects the provider's data market (empty: derived from origin).
	Market  string
	Timeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.APIURL == "" {
		c.APIURL = "https://api.travelpayouts.com"
	}
	if c.AutocompleteURL == "" {
		c.AutocompleteURL = "https://autocomplete.travelpayouts.com"
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	c.APIURL = strings.TrimRight(c.APIURL, "/")
	c.AutocompleteURL = strings.TrimRight(c.AutocompleteURL, "/")
	return c
}

type transport struct {
	http  *http.Client
	token string
}

func newTransport(c Config) transport {
	return transport{http: &http.Client{Timeout: c.Timeout}, token: c.Token}
}

// getJSON decodes a 200 response; the token travels in a header, never the URL.
func (t transport) getJSON(ctx context.Context, endpoint string, withToken bool, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if withToken {
		req.Header.Set("X-Access-Token", t.token)
	}
	resp, err := t.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("travelpayouts: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return fmt.Errorf("travelpayouts: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("travelpayouts: read: %w", err)
	}
	if len(raw) > maxBody {
		return fmt.Errorf("travelpayouts: response larger than %d bytes", maxBody)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("travelpayouts: decode: %w", err)
	}
	return nil
}
