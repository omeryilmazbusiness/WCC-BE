package tourpackage_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

func TestIsPersonal(t *testing.T) {
	if !domain.IsPersonal(domain.CategoryPersonal) {
		t.Fatal("personal category")
	}
	if domain.IsPersonal("luxury") || domain.IsPersonal("") {
		t.Fatal("catalogue tiers are not personal")
	}
}

func TestNormalizeAssignment(t *testing.T) {
	id := uuid.New()
	got, err := domain.NormalizeAssignment(domain.CategoryPersonal, &id)
	if err != nil || got == nil || *got != id {
		t.Fatalf("personal keeps the customer: %v %v", got, err)
	}
	cleared, err := domain.NormalizeAssignment("luxury", &id)
	if err != nil || cleared != nil {
		t.Fatalf("catalogue drops the owner: %v %v", cleared, err)
	}
	if _, err := domain.NormalizeAssignment(domain.CategoryPersonal, nil); err == nil {
		t.Fatal("personal without a customer")
	}
	nilID := uuid.Nil
	if _, err := domain.NormalizeAssignment(domain.CategoryPersonal, &nilID); err == nil {
		t.Fatal("nil uuid is not a customer")
	} else if _, ok := err.(*shared.AppError); !ok {
		t.Fatalf("want AppError, got %T", err)
	}
}

func TestNormalizeHeaderPersonal(t *testing.T) {
	h := domain.Header{Kind: "umrah", Category: "PERSONAL"}
	if err := domain.NormalizeHeader(&h); err != nil {
		t.Fatal(err)
	}
	if h.Category != domain.CategoryPersonal {
		t.Fatalf("category=%s", h.Category)
	}
	for _, kind := range []string{domain.KindUmrah, domain.KindHajj, domain.KindOther} {
		h := domain.Header{Kind: kind, Category: domain.CategoryPersonal}
		if err := domain.NormalizeHeader(&h); err != nil {
			t.Fatalf("%s personal: %v", kind, err)
		}
	}
}
