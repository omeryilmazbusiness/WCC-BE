package healthprobe

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeReportsStatusAndLatency(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://169.254.169.254/", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	tlsCfg := srv.Client().Transport.(*http.Transport).TLSClientConfig
	p := newProber(2*time.Second, true, tlsCfg)

	latency, code, err := p.Probe(context.Background(), srv.URL)
	if err != nil || code != http.StatusServiceUnavailable || latency <= 0 {
		t.Fatalf("probe = %v %d %v", latency, code, err)
	}
	_, code, err = p.Probe(context.Background(), srv.URL+"/redirect")
	if err != nil || code != http.StatusFound {
		t.Fatalf("redirects must not be followed: %d %v", code, err)
	}
}

func TestProbeBlocksInternalAddresses(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	tlsCfg := srv.Client().Transport.(*http.Transport).TLSClientConfig
	p := newProber(2*time.Second, false, tlsCfg)
	if _, _, err := p.Probe(context.Background(), srv.URL); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("loopback must be blocked, got %v", err)
	}
	for _, raw := range []string{"http://example.com", "ftp://example.com", "https://user:pw@example.com", "::"} {
		if _, _, err := p.Probe(context.Background(), raw); err == nil {
			t.Fatalf("%q must be rejected", raw)
		}
	}
}

func TestBlocked(t *testing.T) {
	for ip, want := range map[string]bool{
		"127.0.0.1": true, "10.1.2.3": true, "192.168.1.1": true, "172.16.0.1": true, "169.254.169.254": true,
		"100.64.0.1": true, "0.0.0.0": true, "::1": true, "fe80::1": true, "fd00::1": true,
		"8.8.8.8": false, "1.1.1.1": false, "2606:4700:4700::1111": false,
	} {
		if got := Blocked(net.ParseIP(ip)); got != want {
			t.Errorf("Blocked(%s) = %v, want %v", ip, got, want)
		}
	}
}
