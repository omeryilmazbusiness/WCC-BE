package customer_test

import (
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestDuplicateScoringHelpers(t *testing.T) {
	if shared.NameSimilarity("Mohammed Ali", "Mohammed Ali Hassan") < 50 {
		t.Fatal("expected overlap")
	}
	if shared.NormalizePhone("050-123") == "" {
		t.Fatal("phone")
	}
}
