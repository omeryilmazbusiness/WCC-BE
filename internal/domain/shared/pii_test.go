package shared

import "testing"

func TestNormalizeAndMask(t *testing.T) {
	if NormalizePhone("+966 50-000-0001") != "+966500000001" {
		t.Fatal("phone")
	}
	if NormalizeEmail("  A@B.Com ") != "a@b.com" {
		t.Fatal("email")
	}
	if NormalizePassport("ab 123") != "AB123" {
		t.Fatal("passport")
	}
	m := MaskPassport("A12345678")
	if m == "A12345678" || !stringsHasStar(m) {
		t.Fatalf("mask=%s", m)
	}
}

func TestNameSimilarity(t *testing.T) {
	if NameSimilarity("Ahmed Al Rashid", "Ahmed Al-Rashid") < 50 {
		t.Fatal("expected fuzzy match")
	}
	if NameSimilarity("Ahmed", "Omar") > 40 {
		t.Fatal("expected low score")
	}
}

func stringsHasStar(s string) bool {
	for _, r := range s {
		if r == '*' {
			return true
		}
	}
	return false
}
