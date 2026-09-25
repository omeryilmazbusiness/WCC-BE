package users

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appuser "github.com/wodi-crm/wodi-crm-be/internal/app/useradmin"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type Handler struct {
	Svc *appuser.Service
}

type createRequest struct {
	Email    string  `json:"email"`
	Password string  `json:"password"`
	FullName string  `json:"full_name"`
	Role     string  `json:"role"`
	BranchID string  `json:"branch_id"`
	TeamID   *string `json:"team_id"`
}

type updateRequest struct {
	FullName   *string `json:"full_name"`
	Role       *string `json:"role"`
	BranchID   *string `json:"branch_id"`
	TeamID     *string `json:"team_id"`
	ClearTeam  bool    `json:"clear_team"`
	IsActive   *bool   `json:"is_active"`
	Password   *string `json:"password"`
	MFAEnabled *bool   `json:"mfa_enabled"`
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	page := request.Page(r)
	f := identity.UserFilter{
		Query:  request.FilterString(r, "q"),
		Limit:  page.Limit,
		Offset: page.Offset,
	}
	if role := request.FilterString(r, "role"); role != "" {
		rr := platformauth.Role(role)
		f.Role = &rr
	}
	if bid := request.FilterString(r, "branch_id"); bid != "" {
		id, err := uuid.Parse(bid)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid branch_id"))
			return
		}
		f.BranchID = middleware.ScopeBranch(claims, &id)
	} else {
		f.BranchID = middleware.ScopeBranch(claims, nil)
	}
	items, total, err := h.Svc.List(r.Context(), f)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapUser(&items[i]))
	}
	meta := shared.NewPageMeta(int64(total), page)
	response.JSONMeta(w, http.StatusOK, out, map[string]any{
		"total": meta.Total, "limit": meta.Limit, "offset": meta.Offset, "page": meta.Page, "total_pages": meta.TotalPages,
	})
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	u, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapUser(u))
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	branchID, err := uuid.Parse(req.BranchID)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid branch_id"))
		return
	}
	var teamID *uuid.UUID
	if req.TeamID != nil && *req.TeamID != "" {
		id, err := uuid.Parse(*req.TeamID)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid team_id"))
			return
		}
		teamID = &id
	}
	u, err := h.Svc.Create(r.Context(), appuser.CreateInput{
		Email: req.Email, Password: req.Password, FullName: req.FullName,
		Role: platformauth.Role(req.Role), BranchID: branchID, TeamID: teamID,
		ActorID: claims.UserID, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapUser(u))
}

func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	in := appuser.UpdateInput{
		UserID: id, FullName: req.FullName, IsActive: req.IsActive, Password: req.Password,
		MFAEnabled: req.MFAEnabled, ActorID: claims.UserID, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	}
	if req.Role != nil {
		role := platformauth.Role(*req.Role)
		in.Role = &role
	}
	if req.BranchID != nil {
		bid, err := uuid.Parse(*req.BranchID)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid branch_id"))
			return
		}
		in.BranchID = &bid
	}
	if req.ClearTeam {
		in.SetTeam = true
		in.TeamID = nil
	} else if req.TeamID != nil {
		tid, err := uuid.Parse(*req.TeamID)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid team_id"))
			return
		}
		in.SetTeam = true
		in.TeamID = &tid
	}
	u, err := h.Svc.Update(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapUser(u))
}

func (h Handler) ListBranches(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListBranches(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapBranch(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) UpdateBranch(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	// GM may only update own branch
	if claims.Role != platformauth.RoleGM && claims.Role != platformauth.RoleAdmin {
		response.Error(w, shared.NewForbidden("branch update requires gm or admin"))
		return
	}
	if claims.Role == platformauth.RoleGM && claims.BranchID != id {
		response.Error(w, shared.NewForbidden("branch mismatch"))
		return
	}
	var body struct {
		Code   string `json:"code"`
		NameEN string `json:"name_en"`
		NameAR string `json:"name_ar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	actor := claims.UserID
	b, err := h.Svc.UpdateBranch(r.Context(), appuser.UpdateBranchInput{
		BranchID: id, Code: body.Code, NameEN: body.NameEN, NameAR: body.NameAR,
		ActorID: actor, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBranch(b))
}

func (h Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
	var branchID *uuid.UUID
	if bid := request.FilterString(r, "branch_id"); bid != "" {
		id, err := uuid.Parse(bid)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid branch_id"))
			return
		}
		branchID = &id
	}
	items, err := h.Svc.ListTeams(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapTeam(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) PermissionsMatrix(w http.ResponseWriter, _ *http.Request) {
	response.JSON(w, http.StatusOK, map[string]any{
		"roles":       platformauth.AllRoles(),
		"permissions": platformauth.RolePermissionMatrix(),
	})
}

func mapUser(u *identity.User) map[string]any {
	return map[string]any{
		"id": u.ID, "email": u.Email, "full_name": u.FullName, "role": u.Role,
		"branch_id": u.BranchID, "team_id": u.TeamID, "is_active": u.IsActive,
		"mfa_enabled": u.MFAEnabled, "created_at": u.CreatedAt, "updated_at": u.UpdatedAt,
	}
}

func mapBranch(b *identity.Branch) map[string]any {
	return map[string]any{
		"id": b.ID, "code": b.Code, "name_en": b.NameEN, "name_ar": b.NameAR,
		"is_active": b.IsActive, "created_at": b.CreatedAt,
	}
}

func mapTeam(t *identity.Team) map[string]any {
	return map[string]any{
		"id": t.ID, "branch_id": t.BranchID, "code": t.Code,
		"name_en": t.NameEN, "name_ar": t.NameAR, "is_active": t.IsActive,
		"created_at": t.CreatedAt,
	}
}
