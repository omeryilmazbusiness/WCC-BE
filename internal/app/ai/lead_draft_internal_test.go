package ai

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSanitizeLeadDraft(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	umrah := CatalogPackage{ID: uuid.New(), Code: "UMR-10", Name: "Umrah 10 nights"}
	conv := &DraftConversation{ContactName: "Abu Ahmad", ContactPhone: "+966500000001"}
	raw := map[string]any{
		"full_name":        "  Ahmed Al-Harbi ",
		"phone":            "+10000000000",
		"travel_date":      "2027-03-10",
		"travel_window":    "Ramadan 2027",
		"pax_count":        float64(4),
		"budget_amount":    "12,500.50",
		"budget_currency":  "sar",
		"package_code":     "umr-10",
		"package_interest": "ignored when a package matches",
		"notes":            "Departing from Jeddah.",
	}
	d := sanitizeLeadDraft(raw, []CatalogPackage{umrah}, conv, now)
	if d.FullName != "Ahmed Al-Harbi" || d.Phone != conv.ContactPhone {
		t.Fatalf("identity: %+v", d)
	}
	if d.TravelDate != "2027-03-10" || d.TravelWindow != "Ramadan 2027" || *d.PaxCount != 4 {
		t.Fatalf("trip: %+v", d)
	}
	if *d.BudgetAmount != 1250050 || d.BudgetCurrency != "SAR" {
		t.Fatalf("budget: %v %s", *d.BudgetAmount, d.BudgetCurrency)
	}
	if d.PackageID == nil || *d.PackageID != umrah.ID || d.PackageInterest != "" {
		t.Fatalf("package: %+v", d)
	}
	want := []string{"full_name", "travel_date", "travel_window", "pax_count", "budget_amount", "package_id", "notes"}
	if !slices.Equal(d.AIFields, want) {
		t.Fatalf("ai fields = %v", d.AIFields)
	}
}

func TestSanitizeLeadDraftDropsWhatItCannotTrust(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	conv := &DraftConversation{ContactName: "Abu Ahmad", ContactPhone: "+966500000001"}
	raw := map[string]any{
		"full_name":        nil,
		"travel_date":      "2019-01-01",
		"pax_count":        2.5,
		"budget_amount":    float64(9000),
		"budget_currency":  "riyal",
		"package_code":     "NOPE",
		"package_interest": "5-star Umrah near the Haram",
		"notes":            "null",
	}
	d := sanitizeLeadDraft(raw, nil, conv, now)
	if d.FullName != "Abu Ahmad" {
		t.Fatalf("name must fall back to the contact: %q", d.FullName)
	}
	if d.TravelDate != "" || d.PaxCount != nil || d.BudgetAmount != nil || d.BudgetCurrency != "" || d.PackageID != nil || d.Notes != "" {
		t.Fatalf("untrusted values kept: %+v", d)
	}
	if d.PackageInterest != "5-star Umrah near the Haram" || !slices.Equal(d.AIFields, []string{"package_interest"}) {
		t.Fatalf("interest: %+v", d)
	}
}

func TestLeadDraftPromptCarriesDateAndCatalogue(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	got := leadDraftPrompt(now, []CatalogPackage{{Code: "HAJ-VIP", Name: "Hajj VIP"}}, "customer: hi")
	for _, want := range []string{"Today: 2026-09-29 (Tuesday)", "HAJ-VIP: Hajj VIP", "customer: hi"} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}
}
