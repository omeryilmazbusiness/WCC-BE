package shared

import "testing"

func TestFormatMinor(t *testing.T) {
	for in, want := range map[int64]string{
		0: "0.00 USD", 5: "0.05 USD", 150000: "1500.00 USD", 125050: "1250.50 USD", -3599: "-35.99 USD",
	} {
		if got := FormatMinor(in, "USD"); got != want {
			t.Fatalf("FormatMinor(%d) = %q, want %q", in, got, want)
		}
	}
}
