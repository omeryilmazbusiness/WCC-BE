package tourpackage

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type createPackageRequest struct {
	Code        string `json:"code"`
	NameEN      string `json:"name_en"`
	NameAR      string `json:"name_ar"`
	Description string `json:"description"`
}

type createDepartureRequest struct {
	Code          string `json:"code"`
	DepartDate    string `json:"depart_date"` // YYYY-MM-DD
	ReturnDate    string `json:"return_date"`
	CapacityTotal int    `json:"capacity_total"`
	BasePrice     int64  `json:"base_price"`
	Currency      string `json:"currency"`
}

type cloneDepartureRequest struct {
	Code       string `json:"code"`
	DepartDate string `json:"depart_date"`
	ReturnDate string `json:"return_date"`
}

func (h Handler) ListPackages(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	activeOnly := r.URL.Query().Get("active") != "false"
	items, err := h.Svc.ListPackages(r.Context(), claims.BranchID, activeOnly)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) CreatePackage(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var req createPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	p, err := h.Svc.CreatePackage(r.Context(), appsvc.CreatePackageInput{
		BranchID:    claims.BranchID,
		Code:        req.Code,
		NameEN:      req.NameEN,
		NameAR:      req.NameAR,
		Description: req.Description,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, p)
}

func (h Handler) GetPackage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	p, err := h.Svc.GetPackage(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, p)
}

func (h Handler) ListDepartures(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListDepartures(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) CreateDeparture(w http.ResponseWriter, r *http.Request) {
	pkgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req createDepartureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	depart, err := parseDate(req.DepartDate)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid depart_date"))
		return
	}
	ret, err := parseDate(req.ReturnDate)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid return_date"))
		return
	}
	d, err := h.Svc.CreateDeparture(r.Context(), appsvc.CreateDepartureInput{
		PackageID:     pkgID,
		Code:          req.Code,
		DepartDate:    depart,
		ReturnDate:    ret,
		CapacityTotal: req.CapacityTotal,
		BasePrice:     req.BasePrice,
		Currency:      req.Currency,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, d)
}

func (h Handler) GetDeparture(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	d, err := h.Svc.GetDeparture(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"departure": d,
		"remaining": d.Remaining(),
	})
}

func (h Handler) CloneDeparture(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req cloneDepartureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	depart, err := parseDate(req.DepartDate)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid depart_date"))
		return
	}
	ret, err := parseDate(req.ReturnDate)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid return_date"))
		return
	}
	d, err := h.Svc.CloneDeparture(r.Context(), appsvc.CloneDepartureInput{
		SourceID:   id,
		Code:       req.Code,
		DepartDate: depart,
		ReturnDate: ret,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, d)
}

func parseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}
