package tourpackage

import (
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// CategoryPersonal is a one-customer product sheet (Umrah, Hajj or other).
const CategoryPersonal = "personal"

// IsPersonal reports a sheet built for one assigned customer, not the catalogue.
func IsPersonal(category string) bool {
	return category == CategoryPersonal
}

// NormalizeAssignment ties a personal sheet to exactly one customer and
// clears the owner on catalogue tiers.
func NormalizeAssignment(category string, customerID *uuid.UUID) (*uuid.UUID, error) {
	if !IsPersonal(category) {
		return nil, nil
	}
	if customerID == nil || *customerID == uuid.Nil {
		return nil, shared.NewValidation("personal packages need a customer")
	}
	id := *customerID
	return &id, nil
}
