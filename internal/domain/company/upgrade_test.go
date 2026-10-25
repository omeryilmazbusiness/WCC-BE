package company

import (
	"strings"
	"testing"
)

func TestComposeUpgrade(t *testing.T) {
	en := ComposeUpgrade("en", "Sara Ahmad", "sara@alnoor.test", "gm", "Al Noor", PlanStandard, PlanPremium, "Finance")
	if en.Title != "Upgrade request: standard → premium" || en.Page != "/upgrade/premium" || en.Locale != "en" {
		t.Fatalf("%+v", en)
	}
	if !containsAll(en.Description, "Sara Ahmad", "Al Noor", "Finance") {
		t.Fatal(en.Description)
	}
	ar := ComposeUpgrade("ar-SA", "سارة", "sara@alnoor.test", "gm", "النور", PlanStandard, PlanUltimate, "")
	if ar.Locale != "ar" || ar.Page != "/upgrade/ultimate" {
		t.Fatalf("%+v", ar)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
