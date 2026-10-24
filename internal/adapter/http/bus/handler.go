// Package bus exposes the operations coach-operator catalogue over HTTP.
package bus

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/bus"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/bus"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid "+name))
		return uuid.Nil, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return false
	}
	return true
}

type companyRequest struct {
	Name         string `json:"name"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	Website      string `json:"website"`
	Country      string `json:"country"`
	City         string `json:"city"`
	Address      string `json:"address"`
	VehicleClass string `json:"vehicle_class"`
	LicenseNo    string `json:"license_no"`
	ContactName  string `json:"contact_name"`
	ContactPhone string `json:"contact_phone"`
	Notes        string `json:"notes"`
	IsActive     *bool  `json:"is_active"`
}

func (req companyRequest) input() appsvc.CompanyInput {
	return appsvc.CompanyInput{
		Name: req.Name, Phone: req.Phone, Email: req.Email, Website: req.Website,
		Country: req.Country, City: req.City, Address: req.Address,
		VehicleClass: req.VehicleClass, LicenseNo: req.LicenseNo,
		ContactName: req.ContactName, ContactPhone: req.ContactPhone,
		Notes: req.Notes, IsActive: req.IsActive,
	}
}

func mapCompany(c *domain.Company) map[string]any {
	return map[string]any{
		"id": c.ID, "branch_id": c.BranchID, "name": c.Name, "phone": c.Phone, "email": c.Email,
		"website": c.Website, "country": c.Country, "city": c.City, "address": c.Address,
		"vehicle_class": c.VehicleClass, "license_no": c.LicenseNo,
		"contact_name": c.ContactName, "contact_phone": c.ContactPhone,
		"notes": c.Notes, "is_active": c.IsActive,
		"created_at": c.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// List: GET /buses?q=&active=true
func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	items, err := h.Svc.List(r.Context(), domain.ListFilter{
		BranchID: branchID, Query: request.FilterString(r, "q"),
		ActiveOnly: request.FilterString(r, "active") == "true",
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapCompany(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var req companyRequest
	if !decode(w, r, &req) {
		return
	}
	c, err := h.Svc.Create(r.Context(), branchID, req.input())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapCompany(c))
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	c, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapCompany(c))
}

func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req companyRequest
	if !decode(w, r, &req) {
		return
	}
	c, err := h.Svc.Update(r.Context(), id, req.input())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapCompany(c))
}
