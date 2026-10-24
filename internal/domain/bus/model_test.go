package bus

import (
	"errors"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestCompanyNormalize(t *testing.T) {
	c := Company{
		Name: "  kamil   koç ", Phone: "+90 212 111 22 33", Email: "Ops@Kamil.example",
		Country: "  turkey  ", City: " istanbul ", VehicleClass: "", ContactName: "  ali  ",
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if c.Name != "kamil koç" || c.Country != "turkey" || c.City != "istanbul" || c.VehicleClass != ClassStandard ||
		c.Email != "ops@kamil.example" || c.ContactName != "ali" {
		t.Fatalf("normalized: %+v", c)
	}

	bad := Company{Name: "x", Email: "not-mail", VehicleClass: "limo"}
	if err := bad.Normalize(); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation, got %v", err)
	}
}
