package customer

import (
	"testing"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
)

func TestMapCustomerAlwaysMasksPassport(t *testing.T) {
	c := &domain.Customer{PassportNo: "A12345678"}
	m := mapCustomer(c)
	if got := m["passport_no"]; got != "••••5678" {
		t.Fatalf("passport_no = %v", got)
	}
	if got := m["passport_last4"]; got != "5678" {
		t.Fatalf("passport_last4 = %v", got)
	}
}

func TestMapCustomerWithoutPassport(t *testing.T) {
	m := mapCustomer(&domain.Customer{})
	if m["passport_no"] != "" || m["passport_last4"] != "" {
		t.Fatalf("empty passport leaked mask: %v", m)
	}
}
