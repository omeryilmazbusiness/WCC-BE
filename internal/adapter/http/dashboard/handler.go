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
	Now func() time.Time
}

func (h Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
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

	from, to := appsvc.DefaultPeriod(h.now())
	q := r.URL.Query()
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			response.Error(w, shared.NewValidation("from must be RFC3339"))
			return
		}
		from = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			response.Error(w, shared.NewValidation("to must be RFC3339"))
			return
		}
		to = t
	}

	branchID := middleware.ScopeBranch(claims, &claims.BranchID)
	kpi, err := h.Svc.KPIs(r.Context(), branchID, from, to)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, kpi)
}
