package dashboard

import (
	"net/http"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// Revenue serves GET /dashboard/revenue: finance-ledger revenue in the branch reporting currency.
func (h Handler) Revenue(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	if claims.Role == platformauth.RoleEmployee {
		response.Error(w, shared.NewForbidden("manager dashboard only"))
		return
	}
	from, to, err := h.parsePeriod(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	branchID, err := request.OptionalUUID(r, "branch_id")
	if err != nil {
		response.Error(w, err)
		return
	}
	out, err := h.Svc.Revenue(r.Context(), branchID, from, to)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}
