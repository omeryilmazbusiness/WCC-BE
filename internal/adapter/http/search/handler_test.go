package search

import (
	"testing"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/search"
)

func TestMaskPassportsOnlyTouchesPassportHits(t *testing.T) {
	hits := []domain.Hit{
		{Kind: domain.KindPassport, Subtitle: "5678"},
		{Kind: domain.KindCustomer, Subtitle: "+966500000000"},
	}
	maskPassports(hits)
	if hits[0].Subtitle != "••••5678" {
		t.Fatalf("passport subtitle = %q", hits[0].Subtitle)
	}
	if hits[1].Subtitle != "+966500000000" {
		t.Fatalf("customer subtitle changed: %q", hits[1].Subtitle)
	}
}
