package task

import (
	"net/http"
	"strconv"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func (h Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 20
	}
	items, total, err := h.Svc.ListMine(r.Context(), claims.UserID, nil, limit, offset)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSONMeta(w, http.StatusOK, items, map[string]any{"total": total})
}
