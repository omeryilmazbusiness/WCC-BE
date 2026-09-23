package dashboard

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

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

func (h Handler) parsePeriod(r *http.Request) (from, to time.Time, err error) {
	from, to = appsvc.DefaultPeriod(h.now())
	q := r.URL.Query()
	if v := q.Get("from"); v != "" {
		t, e := time.Parse(time.RFC3339, v)
		if e != nil {
			return time.Time{}, time.Time{}, shared.NewValidation("from must be RFC3339")
		}
		from = t
	}
	if v := q.Get("to"); v != "" {
		t, e := time.Parse(time.RFC3339, v)
		if e != nil {
			return time.Time{}, time.Time{}, shared.NewValidation("to must be RFC3339")
		}
		to = t
	}
	return from, to, nil
}

func (h Handler) resolveBranch(claims *platformauth.Claims, r *http.Request) *uuid.UUID {
	requested := &claims.BranchID
	if v := r.URL.Query().Get("branch_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			requested = &id
		}
	}
	return middleware.ScopeBranch(claims, requested)
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
	from, to, err := h.parsePeriod(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	branchID := h.resolveBranch(claims, r)
	kpi, err := h.Svc.KPIs(r.Context(), branchID, from, to)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, kpi)
}

func (h Handler) Team(w http.ResponseWriter, r *http.Request) {
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
	branchID := h.resolveBranch(claims, r)
	items, err := h.Svc.Team(r.Context(), branchID, from, to)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) Attention(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	if claims.Role == platformauth.RoleEmployee {
		response.Error(w, shared.NewForbidden("manager dashboard only"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	branchID := h.resolveBranch(claims, r)
	items, err := h.Svc.Attention(r.Context(), branchID, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) MyWork(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.MyWork(r.Context(), claims.BranchID, claims.UserID, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) MyTarget(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	ownerID := &claims.UserID
	// Managers can request branch-level target with ?scope=branch
	if r.URL.Query().Get("scope") == "branch" &&
		(claims.Role == platformauth.RoleGM || claims.Role == platformauth.RoleManager) {
		ownerID = nil
	}
	prog, err := h.Svc.MyTarget(r.Context(), claims.BranchID, ownerID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, prog)
}
