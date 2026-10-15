package lead

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseSearch(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"Ayşe  KAYA", []string{"ayşe", "kaya"}},
		{"kaya kaya, Kaya", []string{"kaya"}},
		{"+90 (555) 123-45", []string{"9055512345"}},
		{"555", []string{"555"}},
		{"a b c d e f g", []string{"a", "b", "c", "d", "e"}},
		{"acme 2024", []string{"acme", "2024"}},
	}
	for _, c := range cases {
		got, err := ParseSearch(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if !reflect.DeepEqual(got.Tokens, c.want) {
			t.Errorf("%q: tokens %v, want %v", c.in, got.Tokens, c.want)
		}
		if got.Empty() != (len(c.want) == 0) {
			t.Errorf("%q: Empty mismatch", c.in)
		}
	}
}

func TestParseSearchRejectsLongQuery(t *testing.T) {
	if _, err := ParseSearch(strings.Repeat("ş", MaxQueryLength)); err != nil {
		t.Fatalf("limit is inclusive in runes: %v", err)
	}
	if _, err := ParseSearch(strings.Repeat("a", MaxQueryLength+1)); err == nil {
		t.Fatal("over-long query must be rejected")
	}
}

func TestIsPhoneToken(t *testing.T) {
	for tok, want := range map[string]bool{"555": true, "5": true, "": false, "55a": false, "+90": false} {
		if IsPhoneToken(tok) != want {
			t.Errorf("IsPhoneToken(%q) != %v", tok, want)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	if got := EscapeLike(`50%_off\`); got != `50\%\_off\\` {
		t.Fatalf("got %q", got)
	}
}

func TestRetentionStart(t *testing.T) {
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if got := RetentionStart(now); !got.Equal(time.Date(2025, 10, 15, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %v", got)
	}
}

func TestValidateFilter(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	ok := []ListFilter{
		{},
		{CreatedFrom: &from, CreatedTo: &to},
		{LostReason: LostReasonCodes()[0], Segment: SegmentB2B},
	}
	for i, f := range ok {
		if err := ValidateFilter(f); err != nil {
			t.Errorf("ok[%d]: %v", i, err)
		}
	}
	bad := []ListFilter{
		{CreatedFrom: &to, CreatedTo: &from},
		{CreatedFrom: &from, CreatedTo: &from},
		{LostReason: "nope"},
		{Segment: "b2x"},
	}
	for i, f := range bad {
		if err := ValidateFilter(f); err == nil {
			t.Errorf("bad[%d] accepted", i)
		}
	}
}
