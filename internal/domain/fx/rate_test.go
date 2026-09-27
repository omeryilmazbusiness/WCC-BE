package fx

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestParseRate(t *testing.T) {
	ok := map[string]int64{
		"3.75":        375_000_000,
		"1":           100_000_000,
		"0.00000001":  1,
		"0.26666667":  26_666_667,
		" 12.5 ":      1_250_000_000,
		"3.75000000":  375_000_000,
		"92233720367": 9_223_372_036_700_000_000,
	}
	for in, want := range ok {
		got, err := ParseRate(in)
		if err != nil || got != want {
			t.Errorf("ParseRate(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "0.0", "-1", "+1", "1e3", "3.", ".5", "3.123456789", "abc", "1,5", "92233720368", "3.7.5"} {
		if _, err := ParseRate(in); err == nil {
			t.Errorf("ParseRate(%q) must fail", in)
		}
	}
}

func TestFormatRateRoundTrip(t *testing.T) {
	for _, s := range []string{"3.75000000", "0.00000001", "1.00000000", "1234.56789012"} {
		scaled, err := ParseRate(s)
		if err != nil {
			t.Fatal(err)
		}
		if got := FormatRate(scaled); got != s {
			t.Errorf("FormatRate(ParseRate(%q)) = %q", s, got)
		}
	}
}

func TestApplyRoundsHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		amount, scaled, want int64
	}{
		{10000, 375_000_000, 37500},     // 100.00 USD @3.75 = 375.00 SAR
		{1, 50_000_000, 1},              // 0.5 -> 1
		{-1, 50_000_000, -1},            // -0.5 -> -1
		{1, 49_999_999, 0},              // 0.49999999 -> 0
		{3, 50_000_000, 2},              // 1.5 -> 2
		{-3, 50_000_000, -2},            // -1.5 -> -2
		{37500, 26_666_667, 10000},      // 375.00 SAR @0.26666667 = 100.0000 USD
		{0, 375_000_000, 0},             //
		{12345, RateScale, 12345},       // identity
		{99999, 133_333_333, 133_332},   // 1333.31999667 -> 1333.32
		{-99999, 133_333_333, -133_332}, // symmetric
	}
	for _, c := range cases {
		got, err := Apply(c.amount, c.scaled)
		if err != nil || got != c.want {
			t.Errorf("Apply(%d, %d) = %d, %v; want %d", c.amount, c.scaled, got, err, c.want)
		}
	}
	if _, err := Apply(math.MaxInt64, 2*RateScale); !errors.Is(err, ErrOverflow) {
		t.Errorf("overflow must be reported, got %v", err)
	}
	if _, err := Apply(1, 0); err == nil {
		t.Error("zero rate must be rejected")
	}
}

func TestInvert(t *testing.T) {
	cases := map[int64]int64{
		375_000_000:   26_666_667, // 1/3.75 = 0.266666666.. -> 0.26666667
		RateScale:     RateScale,
		200_000_000:   50_000_000,
		3 * RateScale: 33_333_333,
	}
	for in, want := range cases {
		got, err := Invert(in)
		if err != nil || got != want {
			t.Errorf("Invert(%d) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := Invert(math.MaxInt64); err == nil {
		t.Error("a rate whose inverse rounds to zero must be rejected")
	}
}

func TestPick(t *testing.T) {
	d1 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	d2 := d1.AddDate(0, 0, 1)
	direct := &Rate{Base: "SAR", Quote: "USD", Scaled: 26_000_000, EffectiveDate: d1, Source: "manual"}
	inverse := &Rate{Base: "USD", Quote: "SAR", Scaled: 375_000_000, EffectiveDate: d2, Source: "manual"}

	if _, err := Pick(nil, nil); !errors.Is(err, ErrRateNotFound) {
		t.Fatalf("no rates: %v", err)
	}
	got, _ := Pick(direct, nil)
	if got != *direct {
		t.Errorf("direct only: %+v", got)
	}
	got, _ = Pick(nil, inverse)
	if got.Base != "SAR" || got.Quote != "USD" || got.Scaled != 26_666_667 || !got.EffectiveDate.Equal(d2) || got.Source != "manual (inverse)" {
		t.Errorf("inverse only: %+v", got)
	}
	got, _ = Pick(direct, inverse)
	if got.Scaled != 26_666_667 {
		t.Errorf("newer inverse must win: %+v", got)
	}
	sameDay := *inverse
	sameDay.EffectiveDate = d1
	got, _ = Pick(direct, &sameDay)
	if got != *direct {
		t.Errorf("tie must prefer direct: %+v", got)
	}
}

func TestNormalizeCurrency(t *testing.T) {
	if c, err := NormalizeCurrency(" usd "); err != nil || c != "USD" {
		t.Fatalf("got %q %v", c, err)
	}
	for _, in := range []string{"", "US", "USDT", "U$D", "12A"} {
		if _, err := NormalizeCurrency(in); err == nil {
			t.Errorf("NormalizeCurrency(%q) must fail", in)
		}
	}
}

func TestDateOf(t *testing.T) {
	riyadh := time.FixedZone("AST", 3*3600)
	in := time.Date(2026, 9, 27, 1, 30, 0, 0, riyadh) // 2026-09-26 22:30 UTC
	if got := DateOf(in); !got.Equal(time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("DateOf keeps the local calendar date, got %s", got)
	}
}
