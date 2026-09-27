package customer

import (
	"net/http/httptest"
	"testing"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
)

func TestMapCustomerPassportMasking(t *testing.T) {
	c := &domain.Customer{PassportNo: "A12345678"}
	if got := mapCustomer(c, true)["passport_no"]; got != "A1*****78" {
		t.Fatalf("masked = %v", got)
	}
	if got := mapCustomer(c, false)["passport_no"]; got != "A12345678" {
		t.Fatalf("full = %v", got)
	}
}

func TestCanReadPIIWithoutClaims(t *testing.T) {
	if canReadPII(httptest.NewRequest("GET", "/", nil)) {
		t.Fatal("unauthenticated request must not read PII")
	}
}
