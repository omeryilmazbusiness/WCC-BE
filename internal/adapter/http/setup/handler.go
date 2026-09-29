package setup

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	companyhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/company"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/setup"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/setup"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type companyRequest struct {
	Slug      string `json:"slug"`
	NameEN    string `json:"name_en"`
	NameAR    string `json:"name_ar"`
	LegalName string `json:"legal_name"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	Website   string `json:"website"`
	Country   string `json:"country"`
	City      string `json:"city"`
	Address   string `json:"address"`
	Currency  string `json:"currency"`
	Timezone  string `json:"timezone"`
}

func actor(r *http.Request) (appsvc.Actor, error) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return appsvc.Actor{}, shared.NewUnauthorized("unauthenticated")
	}
	s := access.From(r.Context())
	if s.CompanyID == uuid.Nil {
		return appsvc.Actor{}, shared.NewForbidden("account is not linked to a company")
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		return appsvc.Actor{}, err
	}
	return appsvc.Actor{
		CompanyID: s.CompanyID, BranchID: branchID, UserID: claims.UserID,
		IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	}, nil
}

func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func mapOverview(o *appsvc.Overview) map[string]any {
	steps := make([]map[string]any, 0, len(o.Progress.Steps))
	for _, st := range o.Progress.Steps {
		steps = append(steps, map[string]any{"key": st.Key, "status": st.Status, "count": st.Count})
	}
	var next any
	if o.Progress.Next != "" {
		next = o.Progress.Next
	}
	return map[string]any{
		"company_id":   o.Company.ID,
		"company":      companyhttp.MapCompany(&o.Company),
		"branches":     companyhttp.MapBranches(o.Branches),
		"steps":        steps,
		"next_step":    next,
		"done_count":   o.Progress.DoneCount,
		"total_steps":  len(o.Progress.Steps),
		"completed":    o.Progress.Completed,
		"fully_done":   o.Progress.FullyDone,
		"required":     o.Progress.Required,
		"completed_at": timeOrNil(o.State.CompletedAt),
		"dismissed_at": timeOrNil(o.State.DismissedAt),
	}
}

func (h Handler) write(w http.ResponseWriter, o *appsvc.Overview, err error) {
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapOverview(o))
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	o, err := h.Svc.Overview(r.Context(), a)
	h.write(w, o, err)
}

func (h Handler) SaveCompany(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var body companyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	o, err := h.Svc.SaveCompany(r.Context(), a, company.Company{
		Slug: body.Slug, NameEN: body.NameEN, NameAR: body.NameAR, LegalName: body.LegalName,
		Phone: body.Phone, Email: body.Email, Website: body.Website, Country: body.Country,
		City: body.City, Address: body.Address, Currency: body.Currency, Timezone: body.Timezone,
	})
	h.write(w, o, err)
}

func (h Handler) AdvanceStep(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	step, err := domain.ParseStep(chi.URLParam(r, "step"))
	if err != nil {
		response.Error(w, err)
		return
	}
	var body struct {
		Skip bool `json:"skip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	o, err := h.Svc.Advance(r.Context(), a, step, body.Skip)
	h.write(w, o, err)
}

func (h Handler) Complete(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	o, err := h.Svc.Complete(r.Context(), a)
	h.write(w, o, err)
}

func (h Handler) Dismiss(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	o, err := h.Svc.Dismiss(r.Context(), a)
	h.write(w, o, err)
}
