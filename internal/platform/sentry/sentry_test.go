package sentry

import (
	"testing"

	sdk "github.com/getsentry/sentry-go"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
)

func TestInitEmptyDSNIsNoop(t *testing.T) {
	if err := Init(config.Config{}); err != nil {
		t.Fatal(err)
	}
}

func TestScrubEventStripsSecrets(t *testing.T) {
	event := &sdk.Event{Request: &sdk.Request{
		Cookies:     "session=abc",
		Data:        `{"passport":"X123"}`,
		QueryString: "q=dubai&token=secret&passport=A1",
		Headers: map[string]string{
			"Authorization": "Bearer abc",
			"Content-Type":  "application/json",
			"Cookie":        "sid=1",
		},
	}}
	got := scrubEvent(event, nil)
	if got.Request.Cookies != "" || got.Request.Data != "" {
		t.Fatalf("body/cookies leaked: %+v", got.Request)
	}
	if got.Request.Headers["Authorization"] != "[Filtered]" || got.Request.Headers["Cookie"] != "[Filtered]" {
		t.Fatalf("headers: %+v", got.Request.Headers)
	}
	if got.Request.Headers["Content-Type"] != "application/json" {
		t.Fatal("safe header was stripped")
	}
	if got.Request.QueryString != "q=dubai&token=[Filtered]&passport=[Filtered]" {
		t.Fatalf("query=%q", got.Request.QueryString)
	}
}
