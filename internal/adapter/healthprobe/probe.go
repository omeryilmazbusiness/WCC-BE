// Package healthprobe measures supplier API endpoints over HTTPS without
// letting staff-entered URLs reach internal networks.
package healthprobe

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

const DefaultTimeout = 5 * time.Second

var ErrBlockedAddress = errors.New("address is not allowed")

type Prober struct {
	client *http.Client
}

// New builds a prober that refuses internal addresses.
func New(timeout time.Duration) *Prober { return newProber(timeout, false, nil) }

func newProber(timeout time.Duration, allowPrivate bool, tlsCfg *tls.Config) *Prober {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	dialer := &net.Dialer{Timeout: timeout, Control: guard(allowPrivate)}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		DisableKeepAlives:     true,
		TLSClientConfig:       tlsCfg,
	}
	return &Prober{client: &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Probe sends a GET and reports the time to the response headers. Any HTTP
// status is a successful probe; classification is up to the caller.
func (p *Prober) Probe(ctx context.Context, raw string) (time.Duration, int, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return 0, 0, errors.New("invalid URL")
	}
	if u.Scheme != "https" {
		return 0, 0, errors.New("only https URLs can be checked")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", "WCC-HealthCheck/1.0")
	start := time.Now()
	resp, err := p.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			return latency, 0, ErrBlockedAddress
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return latency, 0, errors.New("timed out")
		}
		return latency, 0, errors.New("connection failed")
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	_ = resp.Body.Close()
	return latency, resp.StatusCode, nil
}

// guard rejects connections to loopback, private, link-local, multicast and
// unspecified addresses after DNS resolution, so rebinding cannot bypass it.
func guard(allowPrivate bool) func(network, address string, _ syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		if allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || Blocked(ip) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
		}
		return nil
	}
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// Blocked reports whether ip must not be reached by a probe.
func Blocked(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip)
}
