package booking

import (
	"net/http"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// fieldAccess decides which sensitive fields a response may carry (T-243).
// The zero value redacts everything.
type fieldAccess struct {
	financials bool // cost and margin
}

func fieldAccessFor(r *http.Request) fieldAccess {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return fieldAccess{}
	}
	return fieldAccessForRole(claims.Role)
}

func fieldAccessForRole(role platformauth.Role) fieldAccess {
	return fieldAccess{
		financials: platformauth.HasPermission(role, platformauth.PermPaymentsRead),
	}
}
