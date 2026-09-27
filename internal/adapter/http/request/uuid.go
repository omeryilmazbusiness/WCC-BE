package request

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// OptionalUUID parses an optional UUID query param; nil when absent.
func OptionalUUID(r *http.Request, key string) (*uuid.UUID, error) {
	v := FilterString(r, key)
	if v == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, shared.NewValidation("invalid " + key)
	}
	return &id, nil
}
