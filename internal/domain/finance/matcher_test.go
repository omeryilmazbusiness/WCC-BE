package finance

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestExtractRefs(t *testing.T) {
	refs, pnrs := ExtractRefs("EFT - Ahmet Kaya WCC-1042 umre odemesi PNR x7k2lp ve #77")
	if !slices.Equal(refs, []int64{1042, 77}) {
		t.Fatalf("refs %v", refs)
	}
	if !slices.Equal(pnrs, []string{"X7K2LP"}) {
		t.Fatalf("pnrs %v", pnrs)
	}
	refs, pnrs = ExtractRefs("HAVALE ODEMESI 123456 ABCDEF")
	if len(refs) != 0 || len(pnrs) != 0 {
		t.Fatalf("plain digits/letters must not match: %v %v", refs, pnrs)
	}
	if refs, _ := ExtractRefs("Havale BK-000123 bakiye"); len(refs) != 1 || refs[0] != 123 {
		t.Fatalf("BK ref = %v", refs)
	}
	refs, _ = ExtractRefs("rez:  15, REZ 15")
	if !slices.Equal(refs, []int64{15}) {
		t.Fatalf("dedupe %v", refs)
	}
}

func TestMatchCredit(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	cands := []BookingDue{
		{BookingID: a, Currency: "SAR", Balance: 5_000},
		{BookingID: a, Currency: "SAR", Balance: 5_000},
		{BookingID: b, Currency: "USD", Balance: 9_000},
	}
	got, ok := MatchCredit(4_000, "SAR", cands)
	if !ok || got.BookingID != a {
		t.Fatalf("single match: %v %v", got, ok)
	}
	if _, ok := MatchCredit(6_000, "SAR", cands); ok {
		t.Fatal("over-payment matched")
	}
	two := append(cands, BookingDue{BookingID: uuid.New(), Currency: "SAR", Balance: 4_000})
	if _, ok := MatchCredit(4_000, "SAR", two); ok {
		t.Fatal("ambiguous credit matched")
	}
}
