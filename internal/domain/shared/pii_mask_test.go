package shared

import "testing"

func TestMaskedPassport(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"AB1":        "••••",
		"a12 345678": "••••5678",
		"••••5678":   "••••5678",
	}
	for in, want := range cases {
		if got := MaskedPassport(in); got != want {
			t.Fatalf("MaskedPassport(%q) = %q, want %q", in, got, want)
		}
	}
	if PassportLast4("AB1") != "" || PassportLast4("x1234567") != "4567" {
		t.Fatal("PassportLast4")
	}
	if MaskPassportLast4("") != "" || MaskPassportLast4("4567") != "••••4567" {
		t.Fatal("MaskPassportLast4")
	}
	if !IsMaskedPassport("••••5678") || IsMaskedPassport("A12345678") {
		t.Fatal("IsMaskedPassport")
	}
}
