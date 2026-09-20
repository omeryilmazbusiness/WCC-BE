package audithttp

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appaudit "github.com/wodi-crm/wodi-crm-be/internal/app/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type Handler struct {
	Svc *appaudit.Service
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	page := request.Page(r)
	f := domain.ListFilter{
		EntityType: request.FilterString(r, "entity_type"),
		Action:     request.FilterString(r, "action"),
		Limit:      page.Limit,
		Offset:     page.Offset,
	}
	if actor := request.FilterString(r, "actor_id"); actor != "" {
		id, err := uuid.Parse(actor)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid actor_id"))
			return
		}
		f.ActorID = &id
	}
	if eid := request.FilterString(r, "entity_id"); eid != "" {
		id, err := uuid.Parse(eid)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid entity_id"))
			return
		}
		f.EntityID = &id
	}
	if from := request.FilterString(r, "from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid from (RFC3339)"))
			return
		}
		f.From = &t
	}
	if to := request.FilterString(r, "to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid to (RFC3339)"))
			return
		}
		f.To = &t
	}
	if bid := request.FilterString(r, "branch_id"); bid != "" {
		id, err := uuid.Parse(bid)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid branch_id"))
			return
		}
		f.BranchID = middleware.ScopeBranch(claims, &id)
	} else if claims.Role != platformauth.RoleGM && claims.Role != platformauth.RoleAdmin {
		id := claims.BranchID
		f.BranchID = &id
	}

	items, total, err := h.Svc.List(r.Context(), f)
	if err != nil {
		response.Error(w, err)
		return
	}
	meta := shared.NewPageMeta(total, page)
	response.JSONMeta(w, http.StatusOK, items, map[string]any{
		"total": meta.Total, "limit": meta.Limit, "offset": meta.Offset, "page": meta.Page, "total_pages": meta.TotalPages,
	})
}
