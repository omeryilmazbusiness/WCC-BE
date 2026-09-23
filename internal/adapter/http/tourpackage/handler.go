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
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
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

type updatePackageRequest struct {
	Code        *string `json:"code"`
	NameEN      *string `json:"name_en"`
	NameAR      *string `json:"name_ar"`
	Description *string `json:"description"`
	IsActive    *bool   `json:"is_active"`
}

type clonePackageRequest struct {
	Code   string `json:"code"`
	NameEN string `json:"name_en"`
	NameAR string `json:"name_ar"`
}

type createDepartureRequest struct {
	Code             string `json:"code"`
	DepartDate       string `json:"depart_date"`
	ReturnDate       string `json:"return_date"`
	CapacityTotal    int    `json:"capacity_total"`
	BasePrice        int64  `json:"base_price"`
	Currency         string `json:"currency"`
	SoftThresholdPct int    `json:"soft_threshold_pct"`
	AllowOversell    bool   `json:"allow_oversell"`
}

type updateDepartureRequest struct {
	Code             *string `json:"code"`
	DepartDate       *string `json:"depart_date"`
	ReturnDate       *string `json:"return_date"`
	CapacityTotal    *int    `json:"capacity_total"`
	BasePrice        *int64  `json:"base_price"`
	Currency         *string `json:"currency"`
	IsActive         *bool   `json:"is_active"`
	SoftThresholdPct *int    `json:"soft_threshold_pct"`
	AllowOversell    *bool   `json:"allow_oversell"`
}

type cloneDepartureRequest struct {
	Code       string `json:"code"`
	DepartDate string `json:"depart_date"`
	ReturnDate string `json:"return_date"`
}

type tiersRequest struct {
	Tiers []tierBody `json:"tiers"`
}

type tierBody struct {
	Code     string `json:"code"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	IsActive *bool  `json:"is_active"`
}

type closeSalesRequest struct {
	Closed bool `json:"closed"`
}

func mapPackage(p *domain.Package) map[string]any {
	return map[string]any{
		"id": p.ID, "branch_id": p.BranchID, "code": p.Code,
		"name_en": p.NameEN, "name_ar": p.NameAR, "description": p.Description,
		"is_active": p.IsActive,
		"created_at": p.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapDeparture(d *domain.Departure) map[string]any {
	return map[string]any{
		"id": d.ID, "package_id": d.PackageID, "code": d.Code,
		"depart_date": d.DepartDate.Format("2006-01-02"),
		"return_date": d.ReturnDate.Format("2006-01-02"),
		"capacity_total": d.CapacityTotal, "capacity_sold": d.CapacitySold,
		"remaining": d.Remaining(), "fill_pct": d.FillPct(), "alert": d.CapacityAlert(),
		"base_price": d.BasePrice, "currency": d.Currency, "is_active": d.IsActive,
		"sales_closed": d.SalesClosed, "soft_threshold_pct": d.SoftThresholdPct,
		"allow_oversell": d.AllowOversell, "pricing_locked": d.PricingLocked(),
		"created_at": d.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": d.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapTier(t domain.PricingTier) map[string]any {
	m := map[string]any{
		"id": t.ID, "code": t.Code, "label": t.Label, "kind": t.Kind,
		"amount": t.Amount, "currency": t.Currency, "sort_order": t.SortOrder,
		"is_active": t.IsActive,
	}
	if t.PackageID != uuid.Nil {
		m["package_id"] = t.PackageID
	}
	if t.DepartureID != nil {
		m["departure_id"] = t.DepartureID
	}
	return m
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
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapPackage(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
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
		BranchID: claims.BranchID, Code: req.Code, NameEN: req.NameEN,
		NameAR: req.NameAR, Description: req.Description,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapPackage(p))
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
	response.JSON(w, http.StatusOK, mapPackage(p))
}

func (h Handler) UpdatePackage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req updatePackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	p, err := h.Svc.UpdatePackage(r.Context(), appsvc.UpdatePackageInput{
		ID: id, Code: req.Code, NameEN: req.NameEN, NameAR: req.NameAR,
		Description: req.Description, IsActive: req.IsActive,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapPackage(p))
}

func (h Handler) ClonePackage(w http.ResponseWriter, r *http.Request) {
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
	var req clonePackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	p, err := h.Svc.ClonePackage(r.Context(), appsvc.ClonePackageInput{
		SourceID: id, BranchID: claims.BranchID, Code: req.Code, NameEN: req.NameEN, NameAR: req.NameAR,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapPackage(p))
}

func (h Handler) ListPackageTiers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListPackageTiers(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, t := range items {
		out = append(out, mapTier(t))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) SetPackageTiers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req tiersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	inputs := make([]appsvc.TierInput, 0, len(req.Tiers))
	for _, t := range req.Tiers {
		active := true
		if t.IsActive != nil {
			active = *t.IsActive
		}
		inputs = append(inputs, appsvc.TierInput{
			Code: t.Code, Label: t.Label, Kind: t.Kind, Amount: t.Amount,
			Currency: t.Currency, IsActive: active,
		})
	}
	items, err := h.Svc.SetPackageTiers(r.Context(), id, inputs)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, t := range items {
		out = append(out, mapTier(t))
	}
	response.JSON(w, http.StatusOK, out)
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
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapDeparture(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
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
		PackageID: pkgID, Code: req.Code, DepartDate: depart, ReturnDate: ret,
		CapacityTotal: req.CapacityTotal, BasePrice: req.BasePrice, Currency: req.Currency,
		SoftThresholdPct: req.SoftThresholdPct, AllowOversell: req.AllowOversell,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapDeparture(d))
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
	response.JSON(w, http.StatusOK, mapDeparture(d))
}

func (h Handler) UpdateDeparture(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req updateDepartureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	in := appsvc.UpdateDepartureInput{ID: id, Code: req.Code, CapacityTotal: req.CapacityTotal,
		BasePrice: req.BasePrice, Currency: req.Currency, IsActive: req.IsActive,
		SoftThresholdPct: req.SoftThresholdPct, AllowOversell: req.AllowOversell}
	if req.DepartDate != nil {
		t, err := parseDate(*req.DepartDate)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid depart_date"))
			return
		}
		in.DepartDate = &t
	}
	if req.ReturnDate != nil {
		t, err := parseDate(*req.ReturnDate)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid return_date"))
			return
		}
		in.ReturnDate = &t
	}
	d, err := h.Svc.UpdateDeparture(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapDeparture(d))
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
		SourceID: id, Code: req.Code, DepartDate: depart, ReturnDate: ret,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapDeparture(d))
}

func (h Handler) ListDepartureTiers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListDepartureTiers(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, t := range items {
		out = append(out, mapTier(t))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) CloseSales(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req closeSalesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	d, err := h.Svc.CloseSales(r.Context(), id, req.Closed)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapDeparture(d))
}

func (h Handler) MarkFull(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	d, err := h.Svc.MarkFull(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapDeparture(d))
}

func (h Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	ready, err := h.Svc.Readiness(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, ready)
}

func (h Handler) RecomputeCapacity(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	d, err := h.Svc.RecomputeCapacity(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapDeparture(d))
}

func parseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}
