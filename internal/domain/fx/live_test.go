package fx

import (
	"errors"
	"testing"
	"time"
)

func sc(s string) int64 {
	v, err := ParseDecimal(s)
	if err != nil {
		panic(err)
	}
	return v
}

var (
	liveNow   = time.Date(2026, 9, 27, 10, 30, 0, 0, time.UTC)
	freshTS   = time.Date(2026, 9, 27, 7, 58, 31, 0, time.UTC)
	staleTS   = time.Date(2026, 3, 2, 15, 29, 17, 0, time.UTC)
	refTS     = time.Date(2026, 9, 27, 0, 2, 31, 0, time.UTC)
	fetchedTS = time.Date(2026, 9, 27, 10, 25, 3, 0, time.UTC)
)

func q(cur, buy, sell, mid string, at time.Time) Quote {
	out := Quote{Currency: cur, Mid: sc(mid), ObservedAt: at}
	if buy != "" {
		out.Buy, out.Sell = sc(buy), sc(sell)
	}
	return out
}

// sampleSnapshots are the live values of 2026-09-27.
func sampleSnapshots() []SourceSnapshot {
	return []SourceSnapshot{
		{Source: "lirascope", Kind: KindOfficial, FetchedAt: fetchedTS, OK: true, Quotes: []Quote{
			q("USD", "121.5", "122.5", "122", freshTS),
		}},
		{Source: "lirascope", Kind: KindMarket, FetchedAt: fetchedTS, OK: true, Quotes: []Quote{
			q("USD", "137", "137.75", "137.375", freshTS),
			q("EUR", "154.8", "156.9", "155.85", freshTS),
			q("SAR", "36.14", "36.71", "36.425", freshTS),
			q("JOD", "164.07", "166.43", "165.25", staleTS),
			q("GBP", "155.84", "158.09", "156.965", staleTS),
		}},
		{Source: "exchangerate-api", Kind: KindReference, FetchedAt: fetchedTS, OK: true, Quotes: []Quote{
			q("USD", "", "", "1", refTS),
			q("SYP", "", "", "121.843708", refTS),
			q("EUR", "", "", "0.877506", refTS),
			q("SAR", "", "", "3.75", refTS),
			q("JOD", "", "", "0.709", refTS),
		}},
	}
}

func boardCfg() BoardConfig {
	return BoardConfig{
		Local: "SYP", Pinned: []string{"USD", "EUR", "SAR"},
		Currencies:   []string{"USD", "SYP", "EUR", "SAR", "GBP", "JOD", "KWD"},
		MarketMaxAge: 48 * time.Hour, StaleAfter: time.Hour,
	}
}

func findQuote(t *testing.T, b Board, cur string) BoardQuote {
	t.Helper()
	for _, q := range b.Quotes {
		if q.Currency == cur {
			return q
		}
	}
	t.Fatalf("no board quote for %s", cur)
	return BoardQuote{}
}

type sideWant struct {
	buy, sell, mid string
	derived, stale bool
}

func checkSide(t *testing.T, name string, got *BoardSide, want *sideWant) {
	t.Helper()
	switch {
	case want == nil && got == nil:
		return
	case want == nil:
		t.Errorf("%s: want null, got %+v", name, got)
		return
	case got == nil:
		t.Errorf("%s: want %+v, got null", name, want)
		return
	}
	if FormatRate(got.Buy) != want.buy || FormatRate(got.Sell) != want.sell || FormatRate(got.Mid) != want.mid ||
		got.Derived != want.derived || got.Stale != want.stale {
		t.Errorf("%s: got buy=%s sell=%s mid=%s derived=%v stale=%v, want %+v", name,
			FormatRate(got.Buy), FormatRate(got.Sell), FormatRate(got.Mid), got.Derived, got.Stale, *want)
	}
}

func TestComposeBoardSample(t *testing.T) {
	b := ComposeBoard(sampleSnapshots(), liveNow, boardCfg())

	var order []string
	for _, q := range b.Quotes {
		order = append(order, q.Currency)
	}
	if got := len(order); got != 6 || order[0] != "USD" || order[1] != "EUR" || order[2] != "SAR" ||
		order[3] != "GBP" || order[4] != "JOD" || order[5] != "KWD" {
		t.Fatalf("order (pinned first, local skipped, no duplicates): %v", order)
	}
	if b.Stale || !b.UpdatedAt.Equal(fetchedTS) || b.Local != "SYP" {
		t.Errorf("board header: %+v", b)
	}

	cases := []struct {
		cur      string
		pinned   bool
		official *sideWant
		market   *sideWant
		cross    string
	}{
		{"USD", true, &sideWant{"121.50000000", "122.50000000", "122.00000000", false, false},
			&sideWant{"137.00000000", "137.75000000", "137.37500000", false, false}, "1.00000000"},
		{"EUR", true, &sideWant{"138.46059172", "139.60018507", "139.03038840", true, false},
			&sideWant{"154.80000000", "156.90000000", "155.85000000", false, false}, "0.87750600"},
		{"SAR", true, &sideWant{"32.40000000", "32.66666667", "32.53333333", true, false},
			&sideWant{"36.14000000", "36.71000000", "36.42500000", false, false}, "3.75000000"},
		// Stale market quote with a reference cross → derived from market USD.
		{"JOD", false, &sideWant{"171.36812412", "172.77856135", "172.07334274", true, false},
			&sideWant{"193.22990127", "194.28772920", "193.75881523", true, false}, "0.70900000"},
		// Stale market quote without a reference cross → kept, flagged stale.
		{"GBP", false, nil, &sideWant{"155.84000000", "158.09000000", "156.96500000", false, true}, ""},
		{"KWD", false, nil, nil, ""},
	}
	for _, c := range cases {
		got := findQuote(t, b, c.cur)
		if got.Pinned != c.pinned {
			t.Errorf("%s pinned=%v", c.cur, got.Pinned)
		}
		checkSide(t, c.cur+" official", got.Official, c.official)
		checkSide(t, c.cur+" market", got.Market, c.market)
		cross := ""
		if got.USDCross > 0 {
			cross = FormatRate(got.USDCross)
		}
		if cross != c.cross {
			t.Errorf("%s usd_cross=%q want %q", c.cur, cross, c.cross)
		}
	}
	if eur := findQuote(t, b, "EUR"); !eur.Official.ObservedAt.Equal(refTS) {
		t.Errorf("derived observed_at must be the older input: %v", eur.Official.ObservedAt)
	}
}

func TestComposeBoardMissingSources(t *testing.T) {
	snaps := sampleSnapshots()

	t.Run("no reference: no derivation, no cross", func(t *testing.T) {
		b := ComposeBoard(snaps[:2], liveNow, boardCfg())
		eur := findQuote(t, b, "EUR")
		if eur.Official != nil || eur.USDCross != 0 || eur.Market == nil || eur.Market.Derived {
			t.Errorf("EUR: %+v", eur)
		}
		if usd := findQuote(t, b, "USD"); usd.USDCross != RateScale {
			t.Errorf("USD cross is always 1: %d", usd.USDCross)
		}
	})

	t.Run("no market USD: stale market quote cannot be derived", func(t *testing.T) {
		market := snaps[1]
		market.Quotes = market.Quotes[1:]
		b := ComposeBoard([]SourceSnapshot{snaps[0], market, snaps[2]}, liveNow, boardCfg())
		checkSide(t, "USD market", findQuote(t, b, "USD").Market, nil)
		checkSide(t, "JOD market", findQuote(t, b, "JOD").Market,
			&sideWant{"164.07000000", "166.43000000", "165.25000000", false, true})
	})

	t.Run("stale market USD marks derived quotes stale", func(t *testing.T) {
		b := ComposeBoard(snaps, liveNow.Add(72*time.Hour), boardCfg())
		if !b.Stale {
			t.Error("board must be stale when the last fetch is older than StaleAfter")
		}
		usd := findQuote(t, b, "USD")
		eur := findQuote(t, b, "EUR")
		if !usd.Market.Stale || usd.Market.Derived || !eur.Market.Derived || !eur.Market.Stale {
			t.Errorf("USD %+v EUR %+v", usd.Market, eur.Market)
		}
	})

	t.Run("never fetched: stale, everything null", func(t *testing.T) {
		b := ComposeBoard([]SourceSnapshot{{Source: "lirascope", Kind: KindOfficial, Error: "timeout"}}, liveNow, boardCfg())
		if !b.Stale || !b.UpdatedAt.IsZero() {
			t.Errorf("header: %+v", b)
		}
		if usd := findQuote(t, b, "USD"); usd.Official != nil || usd.Market != nil {
			t.Errorf("USD: %+v", usd)
		}
	})
}

func TestParseDecimal(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"121.843708", 12_184_370_800, true},
		{"122.000000", 12_200_000_000, true},
		{"0.123456785", 12_345_679, true},
		{"0.123456784", 12_345_678, true},
		{"1.2e-05", 1_200, true},
		{"89500", 8_950_000_000_000, true},
		{"0", 0, false},
		{"0.000000001", 0, false},
		{"-1", 0, false},
		{"1e99", 0, false},
		{"NaN", 0, false},
		{"0x10", 0, false},
		{"1/3", 0, false},
		{"", 0, false},
		{"100000000000000", 0, false},
	}
	for _, c := range cases {
		got, err := ParseDecimal(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ParseDecimal(%q) = %d, %v; want %d ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestSnapshotValidate(t *testing.T) {
	snaps := sampleSnapshots()
	for _, s := range snaps {
		if err := s.Validate("SYP"); err != nil {
			t.Errorf("%s/%s: %v", s.Source, s.Kind, err)
		}
	}

	oldOfficial := snaps[0]
	oldOfficial.Quotes = []Quote{q("USD", "12150", "12250", "12200", freshTS)}
	if err := oldOfficial.Validate("SYP"); !errors.Is(err, ErrOldLira) {
		t.Errorf("old-lira official USD: %v", err)
	}
	if err := oldOfficial.Validate("IQD"); err != nil {
		t.Errorf("the guard only applies to SYP: %v", err)
	}
	oldRef := snaps[2]
	oldRef.Quotes = []Quote{q("SYP", "", "", "12184.3708", refTS)}
	if err := oldRef.Validate("SYP"); !errors.Is(err, ErrOldLira) {
		t.Errorf("old-lira reference SYP: %v", err)
	}

	bad := []SourceSnapshot{
		{Source: "x", Kind: KindMarket},
		{Source: "x", Kind: "rumour", Quotes: snaps[0].Quotes},
		{Source: "x", Kind: KindMarket, Quotes: []Quote{q("usd", "1", "1", "1", freshTS)}},
		{Source: "x", Kind: KindMarket, Quotes: []Quote{q("USD", "", "", "1", freshTS)}},
		{Source: "x", Kind: KindMarket, Quotes: []Quote{q("USD", "1", "1", "1", freshTS), q("USD", "1", "1", "1", freshTS)}},
		{Source: "x", Kind: KindMarket, Quotes: []Quote{q("USD", "1", "1", "1", time.Time{})}},
	}
	for i, s := range bad {
		if err := s.Validate("SYP"); err == nil {
			t.Errorf("case %d must be rejected: %+v", i, s)
		}
	}
}
