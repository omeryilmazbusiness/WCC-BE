package fxlive

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
)

const liraSample = `{
 "disclaimer":"as is",
 "timestampUtc":"2026-09-27T10:25:03.2928351Z",
 "cbsRates":[{"currency":"USD","buy":121.500000,"sell":122.500000,"mid":122.000000,"timestampUtc":"2026-09-27T10:09:53.566456Z","isManualOverride":false}],
 "marketRates":[
  {"currency":"EUR","buy":154.8,"sell":156.9,"mid":155.85,"timestampUtc":"2026-09-27T07:58:31.149034Z"},
  {"currency":"USD","buy":137.0,"sell":137.75,"mid":137.375,"timestampUtc":"2026-09-27T07:58:31.149Z"},
  {"currency":"GBP","buy":155.84,"sell":158.09,"mid":156.965,"timestampUtc":"2026-03-02T15:29:17.871778Z"},
  {"currency":"sar","buy":"36.14","sell":"36.71","mid":"36.425"},
  {"currency":"XXXX","buy":1,"sell":1,"mid":1},
  {"currency":"TRY","buy":"abc","sell":2.81,"mid":2.795},
  {"currency":"AED","buy":null,"sell":37.51,"mid":37.22},
  {"currency":"EGP","buy":-2.61,"sell":2.65,"mid":2.63}
 ],
 "effectiveRates":[]
}`

func liraServer(t *testing.T, status int, body string, seen *http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = *r.Clone(context.Background())
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func byCurrency(qs []domain.Quote) map[string]domain.Quote {
	m := map[string]domain.Quote{}
	for _, q := range qs {
		m[q.Currency] = q
	}
	return m
}

func TestLiraScopeParsesSample(t *testing.T) {
	var req http.Request
	srv := liraServer(t, http.StatusOK, liraSample, &req)
	src := NewLiraScope(LiraScopeConfig{BaseURL: srv.URL + "/api/v1/", Timeout: time.Second})
	fixed := time.Date(2026, 9, 27, 10, 30, 0, 0, time.UTC)
	src.now = func() time.Time { return fixed }

	snaps, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/api/v1/rates/latest" || req.URL.Query().Get("lang") != "en" {
		t.Errorf("request: %s", req.URL)
	}
	if req.Header.Get("User-Agent") != "wodi-crm/1.0" || req.Header.Get("X-Api-Key") != "" || req.Header.Get("X-Api-Secret") != "" {
		t.Errorf("headers: %v", req.Header)
	}
	if len(snaps) != 2 || snaps[0].Kind != domain.KindOfficial || snaps[1].Kind != domain.KindMarket {
		t.Fatalf("snapshots: %+v", snaps)
	}
	for _, s := range snaps {
		if s.Source != LiraScopeID || !s.FetchedAt.Equal(fixed) || !s.OK {
			t.Errorf("snapshot header: %+v", s)
		}
		if err := s.Validate("SYP"); err != nil {
			t.Errorf("%s: %v", s.Kind, err)
		}
	}
	usd := snaps[0].Quotes[0]
	if usd.Currency != "USD" || usd.Buy != 12_150_000_000 || usd.Sell != 12_250_000_000 || usd.Mid != 12_200_000_000 ||
		!usd.ObservedAt.Equal(time.Date(2026, 9, 27, 10, 9, 53, 566456000, time.UTC)) {
		t.Errorf("official USD: %+v", usd)
	}
	market := byCurrency(snaps[1].Quotes)
	if len(market) != 4 {
		t.Errorf("garbage entries must be skipped, got %v", market)
	}
	if gbp := market["GBP"]; gbp.Mid != 15_696_500_000 || gbp.ObservedAt.Year() != 2026 || gbp.ObservedAt.Month() != time.March {
		t.Errorf("stale GBP keeps its own observed_at: %+v", gbp)
	}
	if sar := market["SAR"]; sar.Mid != 3_642_500_000 || !sar.ObservedAt.Equal(time.Date(2026, 9, 27, 10, 25, 3, 292835100, time.UTC)) {
		t.Errorf("SAR (quoted numbers, payload timestamp fallback): %+v", sar)
	}
	if info := src.Info(); info.URL != srv.URL || len(info.Kinds) != 2 {
		t.Errorf("info: %+v", info)
	}
}

func TestLiraScopeSendsOptionalKeys(t *testing.T) {
	var req http.Request
	srv := liraServer(t, http.StatusOK, liraSample, &req)
	src := NewLiraScope(LiraScopeConfig{BaseURL: srv.URL, APIKey: "k", APISecret: "s"})
	if _, err := src.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-Api-Key") != "k" || req.Header.Get("X-Api-Secret") != "s" {
		t.Errorf("headers: %v", req.Header)
	}
}

func TestLiraScopeOldLiraIsRejectedByValidation(t *testing.T) {
	body := strings.Replace(liraSample, `"buy":121.500000,"sell":122.500000,"mid":122.000000`,
		`"buy":12150,"sell":12250,"mid":12200`, 1)
	srv := liraServer(t, http.StatusOK, body, nil)
	snaps, err := NewLiraScope(LiraScopeConfig{BaseURL: srv.URL}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := snaps[0].Validate("SYP"); !errors.Is(err, domain.ErrOldLira) {
		t.Fatalf("old-lira official must fail validation: %v", err)
	}
}

func TestLiraScopeFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"5xx", http.StatusBadGateway, `{"error":"upstream"}`},
		{"429", http.StatusTooManyRequests, ``},
		{"garbage", http.StatusOK, `<html>maintenance</html>`},
		{"wrong shape", http.StatusOK, `{"cbsRates":{"USD":1}}`},
		{"too large", http.StatusOK, `{"disclaimer":"` + strings.Repeat("x", maxBody) + `"}`},
	}
	for _, c := range cases {
		srv := liraServer(t, c.status, c.body, nil)
		if _, err := NewLiraScope(LiraScopeConfig{BaseURL: srv.URL}).Fetch(context.Background()); err == nil {
			t.Errorf("%s: want error", c.name)
		}
	}
}

func TestLiraScopeTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	_, err := NewLiraScope(LiraScopeConfig{BaseURL: srv.URL, Timeout: 50 * time.Millisecond}).Fetch(context.Background())
	if err == nil || strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("timeout must fail without echoing the URL: %v", err)
	}
}

const erSample = `{"result":"success","provider":"https://www.exchangerate-api.com",
 "time_last_update_unix":1790467351,"time_next_update_unix":1790554621,"base_code":"USD",
 "rates":{"USD":1,"SYP":121.843708,"SAR":3.75,"EUR":0.877506,"LBP":89500,"TINY":1e-12,"eur":2,"BAD":"x","NEG":-1}}`

func TestERAPIParsesSample(t *testing.T) {
	var req http.Request
	srv := liraServer(t, http.StatusOK, erSample, &req)
	src := NewERAPI(srv.URL+"/v6", time.Second)
	snaps, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/v6/latest/USD" || req.Header.Get("User-Agent") != "wodi-crm/1.0" {
		t.Errorf("request: %s %v", req.URL, req.Header)
	}
	if len(snaps) != 1 {
		t.Fatalf("snapshots: %+v", snaps)
	}
	s := snaps[0]
	if s.Kind != domain.KindReference || s.Source != ERAPIID || !s.NextUpdateAt.Equal(time.Unix(1790554621, 0)) {
		t.Errorf("snapshot: %+v", s)
	}
	if err := s.Validate("SYP"); err != nil {
		t.Fatal(err)
	}
	m := byCurrency(s.Quotes)
	if len(m) != 5 || m["USD"].Mid != domain.RateScale || m["SYP"].Mid != 12_184_370_800 ||
		m["EUR"].Mid != 87_750_600 || m["LBP"].Mid != 8_950_000_000_000 {
		t.Errorf("quotes: %+v", m)
	}
	if !m["EUR"].ObservedAt.Equal(time.Unix(1790467351, 0)) {
		t.Errorf("observed_at = time_last_update_unix: %v", m["EUR"].ObservedAt)
	}
	if info := src.Info(); info.Attribution != "Rates By Exchange Rate API" || info.MinInterval != 6*time.Hour {
		t.Errorf("info: %+v", info)
	}
}

func TestERAPIOldLiraIsRejectedByValidation(t *testing.T) {
	srv := liraServer(t, http.StatusOK, strings.Replace(erSample, "121.843708", "12184.3708", 1), nil)
	snaps, err := NewERAPI(srv.URL, time.Second).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := snaps[0].Validate("SYP"); !errors.Is(err, domain.ErrOldLira) {
		t.Fatalf("old-lira reference must fail validation: %v", err)
	}
}

func TestERAPIFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"5xx", http.StatusServiceUnavailable, ``},
		{"rate limited", http.StatusOK, `{"result":"error","error-type":"too-many-requests"}`},
		{"garbage", http.StatusOK, `not json`},
		{"other base", http.StatusOK, `{"result":"success","base_code":"EUR","time_last_update_unix":1,"rates":{}}`},
		{"no timestamp", http.StatusOK, `{"result":"success","base_code":"USD","rates":{"SYP":121.8}}`},
	}
	for _, c := range cases {
		srv := liraServer(t, c.status, c.body, nil)
		if _, err := NewERAPI(srv.URL, time.Second).Fetch(context.Background()); err == nil {
			t.Errorf("%s: want error", c.name)
		}
	}
}
