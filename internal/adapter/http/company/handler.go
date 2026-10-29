package company

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appcompany "github.com/wodi-crm/wodi-crm-be/internal/app/company"
	"github.com/wodi-crm/wodi-crm-be/internal/app/planupgrade"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc      *appcompany.Service
	Upgrades *planupgrade.Service
}

// MapCompany renders a company profile.
func MapCompany(c *domain.Company) map[string]any {
	return map[string]any{
		"id": c.ID, "slug": c.Slug, "name_en": c.NameEN, "name_ar": c.NameAR, "legal_name": c.LegalName,
		"phone": c.Phone, "email": c.Email, "website": c.Website, "country": c.Country, "city": c.City,
		"address": c.Address, "currency": c.Currency, "timezone": c.Timezone,
		"plan": c.Plan, "idle_logout_minutes": c.IdleLogoutMinutes, "is_active": c.IsActive,
		"created_at": c.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": c.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"term":       MapTerm(c),
	}
}

func MapTerm(c *domain.Company) map[string]any {
	t := c.TermAt(time.Now())
	return map[string]any{
		"plan": t.Plan, "years": t.Years, "status": t.Status, "days_left": t.DaysLeft,
		"started_at": t.StartedAt.UTC().Format(time.RFC3339Nano),
		"ends_at":    t.EndsAt.UTC().Format(time.RFC3339Nano),
	}
}

// MapBranch renders a branch; the shape is a superset of the legacy
// /branches payload.
func MapBranch(b *domain.Branch) map[string]any {
	return map[string]any{
		"id": b.ID, "company_id": b.CompanyID, "code": b.Code, "slug": b.Slug,
		"name_en": b.NameEN, "name_ar": b.NameAR, "kind": b.Kind, "timezone": b.Timezone,
		"is_active": b.IsActive, "created_at": b.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func MapBranches(items []domain.Branch) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, MapBranch(&items[i]))
	}
	return out
}

func actor(r *http.Request) (appcompany.Actor, error) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return appcompany.Actor{}, shared.NewUnauthorized("unauthenticated")
	}
	return appcompany.Actor{UserID: claims.UserID, IP: middleware.ClientIP(r), UserAgent: r.UserAgent()}, nil
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return shared.NewValidation("invalid json")
	}
	return nil
}

func (h Handler) ListBranches(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.Branches(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, MapBranches(items))
}

type branchRequest struct {
	NameEN   *string `json:"name_en"`
	NameAR   *string `json:"name_ar"`
	Slug     *string `json:"slug"`
	Code     *string `json:"code"`
	Kind     *string `json:"kind"`
	Timezone *string `json:"timezone"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func parseKind(raw *string) (*domain.Kind, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	k, ok := domain.ParseKind(*raw)
	if !ok {
		err := shared.NewValidation("invalid branch")
		err.Details = map[string]any{"kind": "main_center or branch"}
		return nil, err
	}
	return &k, nil
}

func (h Handler) CreateBranch(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var body branchRequest
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	kind, err := parseKind(body.Kind)
	if err != nil {
		response.Error(w, err)
		return
	}
	in := appcompany.BranchInput{
		NameEN: str(body.NameEN), NameAR: str(body.NameAR), Slug: str(body.Slug), Code: str(body.Code),
		Timezone: str(body.Timezone), Actor: a,
	}
	if kind != nil {
		in.Kind = *kind
	}
	b, err := h.Svc.CreateBranch(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, MapBranch(b))
}

func (h Handler) UpdateBranch(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var body branchRequest
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	kind, err := parseKind(body.Kind)
	if err != nil {
		response.Error(w, err)
		return
	}
	b, err := h.Svc.UpdateBranch(r.Context(), id, appcompany.BranchPatch{
		NameEN: body.NameEN, NameAR: body.NameAR, Slug: body.Slug, Code: body.Code,
		Kind: kind, Timezone: body.Timezone, Actor: a,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, MapBranch(b))
}

type registerRequest struct {
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
	Plan      string `json:"plan"`
	GM        struct {
		FullName string `json:"full_name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	} `json:"gm"`
}

// Register creates a company with its main center and GM (platform admin).
func (h Handler) Register(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var body registerRequest
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	plan, ok := domain.ParsePlan(body.Plan)
	if !ok {
		err := shared.NewValidation("invalid company registration")
		err.Details = map[string]any{"plan": "standard, premium or ultimate"}
		response.Error(w, err)
		return
	}
	out, err := h.Svc.Register(r.Context(), appcompany.RegisterInput{
		Company: domain.Company{
			Slug: body.Slug, NameEN: body.NameEN, NameAR: body.NameAR, LegalName: body.LegalName,
			Phone: body.Phone, Email: body.Email, Website: body.Website, Country: body.Country,
			City: body.City, Address: body.Address, Currency: body.Currency, Timezone: body.Timezone,
			Plan: plan,
		},
		GM:    appcompany.GMAccount{FullName: body.GM.FullName, Email: body.GM.Email, Password: body.GM.Password},
		Actor: a,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{
		"company":     MapCompany(&out.Company),
		"main_center": MapBranch(&out.MainCenter),
		"gm_user_id":  out.GMUserID,
	})
}

// ListCompanies is the platform console list.
func (h Handler) ListCompanies(w http.ResponseWriter, r *http.Request) {
	page := request.Page(r)
	res, err := h.Svc.ListCompanies(r.Context(), request.FilterString(r, "q"), page.Limit, page.Offset)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(res.Items))
	for i := range res.Items {
		item := MapCompany(&res.Items[i].Company)
		item["branch_count"] = res.Items[i].BranchCount
		item["user_count"] = res.Items[i].UserCount
		item["gm_email"] = res.Items[i].GMEmail
		item["has_logo"], item["logo_version"] = false, nil
		if at := res.Items[i].LogoUpdatedAt; at != nil {
			item["has_logo"], item["logo_version"] = true, domain.LogoVersion(*at)
		}
		out = append(out, item)
	}
	meta := shared.NewPageMeta(int64(res.Total), page)
	response.JSONMeta(w, http.StatusOK, out, map[string]any{
		"total": meta.Total, "limit": meta.Limit, "offset": meta.Offset, "page": meta.Page, "total_pages": meta.TotalPages,
	})
}

// ListPlans is the published WCC catalog for the platform console.
func (h Handler) ListPlans(w http.ResponseWriter, r *http.Request) {
	out := make([]map[string]any, 0, 3)
	for _, p := range domain.AllPlans() {
		spec := domain.Of(p)
		out = append(out, map[string]any{
			"plan":        spec.Plan,
			"permissions": spec.Permissions,
			"limits": map[string]int{
				"users": spec.Limits.Users, "branches": spec.Limits.Branches, "packages": spec.Limits.Packages,
				"hotels": spec.Limits.Hotels, "buses": spec.Limits.Buses, "inbox": spec.Limits.Inbox,
				"ai_per_day": spec.Limits.AIPerDay,
			},
		})
	}
	response.JSON(w, http.StatusOK, out)
}

type updatePlanRequest struct {
	Plan string `json:"plan"`
}

// UpdatePlan changes a tenant package (platform admin).
func (h Handler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid company id"))
		return
	}
	var body updatePlanRequest
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	plan, ok := domain.ParsePlan(body.Plan)
	if !ok {
		err := shared.NewValidation("invalid plan")
		err.Details = map[string]any{"plan": "standard, premium or ultimate"}
		response.Error(w, err)
		return
	}
	c, err := h.Svc.UpdatePlan(r.Context(), id, plan, a)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, MapCompany(c))
}

// GetCompany is the platform edit card.
func (h Handler) GetCompany(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid company id"))
		return
	}
	c, err := h.Svc.GetCompany(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, MapCompany(c))
}

type saveCompanyRequest struct {
	NameEN    string `json:"name_en"`
	NameAR    string `json:"name_ar"`
	Slug      string `json:"slug"`
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

type deleteCompanyRequest struct {
	Slug string `json:"slug"`
}

// DeleteCompany retires a tenant after the caller types its web address.
func (h Handler) DeleteCompany(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid company id"))
		return
	}
	var body deleteCompanyRequest
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	if err := h.Svc.Delete(r.Context(), id, body.Slug, a); err != nil {
		response.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SaveCompany updates the tenant profile from the platform console.
func (h Handler) SaveCompany(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid company id"))
		return
	}
	var body saveCompanyRequest
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	c, err := h.Svc.SaveCompany(r.Context(), id, domain.Company{
		NameEN: body.NameEN, NameAR: body.NameAR, Slug: body.Slug, LegalName: body.LegalName,
		Phone: body.Phone, Email: body.Email, Website: body.Website, Country: body.Country,
		City: body.City, Address: body.Address, Currency: body.Currency, Timezone: body.Timezone,
	}, a)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, MapCompany(c))
}

type upgradeRequestBody struct {
	Target  string `json:"target"`
	Feature string `json:"feature"`
	Locale  string `json:"locale"`
}

// RequestUpgrade asks the platform team to move the caller to a higher plan.
func (h Handler) RequestUpgrade(w http.ResponseWriter, r *http.Request) {
	if h.Upgrades == nil {
		response.Error(w, shared.NewUnavailable("upgrade requests are not configured", 30*time.Second))
		return
	}
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	claims, _ := middleware.ClaimsFrom(r.Context())
	var body upgradeRequestBody
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	target, ok := domain.ParsePlan(body.Target)
	if !ok {
		err := shared.NewValidation("invalid plan")
		err.Details = map[string]any{"target": "premium or ultimate"}
		response.Error(w, err)
		return
	}
	out, err := h.Upgrades.Request(r.Context(), planupgrade.Actor{
		UserID: a.UserID, BranchID: claims.BranchID, IP: a.IP, UserAgent: a.UserAgent,
	}, planupgrade.Input{Target: target, Feature: body.Feature, Locale: body.Locale})
	if err != nil {
		response.Error(w, err)
		return
	}
	status := http.StatusCreated
	if out.Duplicate {
		status = http.StatusOK
	}
	response.JSON(w, status, map[string]any{
		"id": out.RequestID, "number": out.Number, "target": out.Target, "duplicate": out.Duplicate,
	})
}
