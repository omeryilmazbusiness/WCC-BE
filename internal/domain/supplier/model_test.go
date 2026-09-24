package supplier_test

import (
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
)

func TestIsOversold(t *testing.T) {
	l := &supplier.Link{Allotment: 10, Sold: 10}
	if l.IsOversold() {
		t.Fatal("equal not oversold")
	}
	l.Sold = 11
	if !l.IsOversold() {
		t.Fatal("expected oversold")
	}
	l.Allotment = 0
	l.Sold = 100
	if l.IsOversold() {
		t.Fatal("unlimited allotment never oversold")
	}
}

func TestConfirm(t *testing.T) {
	l := &supplier.Link{ConfirmationStatus: supplier.ConfirmPending}
	if err := l.Confirm("REF-1"); err != nil {
		t.Fatal(err)
	}
	if l.ConfirmationStatus != supplier.ConfirmConfirmed || l.ConfirmedAt == nil {
		t.Fatal(l.ConfirmationStatus)
	}
	_ = l.Cancel()
	if err := l.Confirm("x"); err == nil {
		t.Fatal("cancelled cannot confirm")
	}
}
