package tourpackage_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

func TestNormalizeVisaPackage(t *testing.T) {
	h := domain.Header{Kind: domain.KindVisa, Category: "standard", DurationDays: 14, TransportMode: "road", BaseCurrency: "SAR"}
	s := domain.Spec{
		VisaOffers: []domain.VisaOffer{
			{Type: "umrah_visa", Net: 40000, Sell: 65000, ProcessingDays: 7, HealthInsurance: true, Notes: "  Nusuk  "},
			{Type: "TOURIST_VISA", Net: 80000, Sell: 110000, ProcessingDays: 10},
		},
		Kit:        []string{"ihram"},
		Destination: domain.Destination{Country: "KSA", Districts: []string{"Makkah"}},
	}
	if err := domain.NormalizeSpec(&s, h); err != nil {
		t.Fatal(err)
	}
	if len(s.VisaOffers) != 2 || s.VisaOffers[0].Type != "UMRAH_VISA" || s.VisaOffers[0].Notes != "Nusuk" {
		t.Fatalf("offers=%+v", s.VisaOffers)
	}
	if s.Visa.Type != "UMRAH_VISA" || !s.Visa.HealthInsurance {
		t.Fatalf("header visa=%+v", s.Visa)
	}
	if s.Destination.Country != "" || len(s.Kit) != 0 {
		t.Fatalf("visa kind must drop tour fields: dest=%+v kit=%v", s.Destination, s.Kit)
	}
	if sell, ok := domain.CheapestVisaSell(s.VisaOffers); !ok || sell != 65000 {
		t.Fatalf("cheapest=%d ok=%v", sell, ok)
	}
	got, ok := domain.FindVisaOffer(s.VisaOffers, "tourist_visa")
	if !ok || got.Sell != 110000 {
		t.Fatalf("find=%+v ok=%v", got, ok)
	}
}

func TestNormalizeVisaPackageRejects(t *testing.T) {
	h := domain.Header{Kind: domain.KindVisa, Category: "luxury", DurationDays: 7, TransportMode: "road", BaseCurrency: "USD"}
	if err := domain.NormalizeSpec(&domain.Spec{}, h); err == nil {
		t.Fatal("empty visa sheet")
	}
	s := domain.Spec{VisaOffers: []domain.VisaOffer{{Type: "UMRAH_VISA", Sell: 0}}}
	if err := domain.NormalizeSpec(&s, h); err == nil {
		t.Fatal("unpriced visa sheet")
	}
	s = domain.Spec{VisaOffers: []domain.VisaOffer{
		{Type: "UMRAH_VISA", Sell: 100},
		{Type: "UMRAH_VISA", Sell: 200},
	}}
	if err := domain.NormalizeSpec(&s, h); err == nil {
		t.Fatal("duplicate type")
	}
	s = domain.Spec{VisaOffers: []domain.VisaOffer{{Type: "WORK", Sell: 100}}}
	if err := domain.NormalizeSpec(&s, h); err == nil {
		t.Fatal("unknown type")
	}
}

func TestNormalizeHeaderVisa(t *testing.T) {
	h := domain.Header{Kind: "VISA", Category: "personal"}
	if err := domain.NormalizeHeader(&h); err != nil {
		t.Fatal(err)
	}
	if h.Kind != domain.KindVisa || h.Category != domain.CategoryPersonal {
		t.Fatalf("got %+v", h)
	}
}

func TestRedactAndKeepVisaNets(t *testing.T) {
	s := domain.Spec{
		VisaOffers: []domain.VisaOffer{
			{Type: "UMRAH_VISA", Net: 40000, Sell: 65000},
			{Type: "TOURIST_VISA", Net: 80000, Sell: 110000},
		},
		Costs: domain.Costs{Currency: "SAR", Visa: 40000, MarkupPct: 20},
	}
	domain.RedactVisaCost(&s)
	if s.VisaOffers[0].Net != 0 || s.VisaOffers[0].Sell != 65000 || s.Costs.Visa != 0 || s.Costs.MarkupPct != 0 {
		t.Fatalf("redact=%+v costs=%+v", s.VisaOffers, s.Costs)
	}

	stored := domain.Spec{VisaOffers: []domain.VisaOffer{{Type: "UMRAH_VISA", Net: 40000, Sell: 65000}}, Costs: domain.Costs{Currency: "SAR", Visa: 40000}}
	incoming := domain.Spec{VisaOffers: []domain.VisaOffer{{Type: "UMRAH_VISA", Net: 0, Sell: 70000}}}
	domain.KeepVisaNets(&stored, &incoming)
	if incoming.VisaOffers[0].Net != 40000 || incoming.VisaOffers[0].Sell != 70000 || incoming.Costs.Visa != 40000 {
		t.Fatalf("keep=%+v costs=%+v", incoming.VisaOffers, incoming.Costs)
	}
}

func TestStandingVisaDeparture(t *testing.T) {
	id := uuid.New()
	now := time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)
	d := domain.StandingVisaDeparture(id, 0, "SAR", 21, now)
	if d.PackageID != id || d.Code != domain.VisaOpenCode || !d.AllowOversell || d.CapacityTotal != 9999 {
		t.Fatalf("slot=%+v", d)
	}
	if d.DepartDate.Day() != 25 || d.ReturnDate.Sub(d.DepartDate) != 21*24*time.Hour {
		t.Fatalf("dates %s %s", d.DepartDate, d.ReturnDate)
	}
}
