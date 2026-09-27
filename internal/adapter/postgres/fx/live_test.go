package fx

import (
	"strings"
	"testing"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
)

func TestLivePayloadRoundTripsAsDecimalText(t *testing.T) {
	at := time.Date(2026, 9, 27, 7, 58, 31, 149034000, time.UTC)
	in := []domain.Quote{
		{Currency: "USD", Buy: 13_700_000_000, Sell: 13_775_000_000, Mid: 13_737_500_000, ObservedAt: at},
		{Currency: "EUR", Mid: 87_750_600, ObservedAt: at},
	}
	raw, err := encodeQuotes(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"quotes":[{"currency":"USD","buy":"137.00000000","sell":"137.75000000","mid":"137.37500000","observed_at":"2026-09-27T07:58:31.149034Z"},` +
		`{"currency":"EUR","mid":"0.87750600","observed_at":"2026-09-27T07:58:31.149034Z"}]}`
	if string(raw) != want {
		t.Fatalf("payload:\n%s\nwant\n%s", raw, want)
	}
	out, err := decodeQuotes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0] != in[0] || out[1].Mid != in[1].Mid || out[1].Buy != 0 || !out[1].ObservedAt.Equal(at) {
		t.Fatalf("round trip: %+v", out)
	}
	if _, err := decodeQuotes([]byte(strings.Replace(want, `"0.87750600"`, `0.8775`, 1))); err == nil {
		t.Fatal("non-string rates must be rejected")
	}
}
