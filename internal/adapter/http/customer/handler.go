package customer

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/customer"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type createRequest struct {
	FullName    string `json:"full_name"`
	FullNameAR  string `json:"full_name_ar"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Nationality string `json:"nationality"`
	Notes       string `json:"notes"`
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
	res, err := h.Svc.Create(r.Context(), appsvc.CreateInput{
		BranchID:    claims.BranchID,
		FullName:    req.FullName,
		FullNameAR:  req.FullNameAR,
		Phone:       req.Phone,
		Email:       req.Email,
		Nationality: req.Nationality,
		Notes:       req.Notes,
		CreatedBy:   claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	meta := map[string]any{}
	if res.DuplicateWarn {
		meta["duplicate_warn"] = true
		meta["duplicate_of"] = res.DuplicateOfID
	}
	response.JSONMeta(w, http.StatusCreated, res.Customer, meta)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	c, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, c)
}

func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.ClaimsFrom(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	f := domain.SearchFilter{
		Query:  r.URL.Query().Get("q"),
		Limit:  limit,
		Offset: offset,
	}
	if claims != nil {
		bid := claims.BranchID
		f.BranchID = middleware.ScopeBranch(claims, &bid)
	}
	items, total, err := h.Svc.Search(r.Context(), f)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSONMeta(w, http.StatusOK, items, map[string]any{"total": total})
}
