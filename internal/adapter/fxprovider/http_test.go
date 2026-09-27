package fxprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

func TestHTTPFetchParsesDecimalText(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"base":"USD","date":"2026-09-26","rates":{"SAR":"3.75","EUR":0.92,"BAD":"1.123456789","NEG":"-1"}}`))
	}))
	defer srv.Close()

	p := NewHTTP(srv.URL+"/latest?key=k", "usd", time.Second)
	rates, err := p.Fetch(context.Background(), time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "base=USD&date=2026-09-27&key=k" {
		t.Errorf("query: %s", gotQuery)
	}
	sort.Slice(rates, func(i, j int) bool { return rates[i].Quote < rates[j].Quote })
	if len(rates) != 2 || rates[0].Quote != "EUR" || rates[0].Scaled != 92_000_000 ||
		rates[1].Quote != "SAR" || rates[1].Scaled != 375_000_000 {
		t.Fatalf("rates: %+v", rates)
	}
	if !rates[1].EffectiveDate.Equal(time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)) || rates[1].Base != "USD" {
		t.Errorf("provider date/base must be kept: %+v", rates[1])
	}
}

func TestHTTPFetchFailsOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	if _, err := NewHTTP(srv.URL, "USD", time.Second).Fetch(context.Background(), time.Now()); err == nil {
		t.Fatal("non-200 must fail")
	}
}
