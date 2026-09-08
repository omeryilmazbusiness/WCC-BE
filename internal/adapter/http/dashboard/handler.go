package dashboard

import (
	"net/http"
	"time"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type Handler struct {
	Svc *appsvc.Service
}

func (h Handler) KPIs(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	if claims.Role == platformauth.RoleEmployee {
		response.Error(w, shared.NewForbidden("manager dashboard only"))
		return
	}
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -30)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}
	branchID := middleware.ScopeBranch(claims, &claims.BranchID)
	kpi, err := h.Svc.KPIs(r.Context(), branchID, from, to)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, kpi)
}
