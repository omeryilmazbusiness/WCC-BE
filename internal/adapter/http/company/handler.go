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
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appcompany.Service
}

// MapCompany renders a company profile.
func MapCompany(c *domain.Company) map[string]any {
	return map[string]any{
		"id": c.ID, "slug": c.Slug, "name_en": c.NameEN, "name_ar": c.NameAR, "legal_name": c.LegalName,
		"phone": c.Phone, "email": c.Email, "website": c.Website, "country": c.Country, "city": c.City,
		"address": c.Address, "currency": c.Currency, "timezone": c.Timezone, "is_active": c.IsActive,
		"created_at": c.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": c.UpdatedAt.UTC().Format(time.RFC3339Nano),
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
	out, err := h.Svc.Register(r.Context(), appcompany.RegisterInput{
		Company: domain.Company{
			Slug: body.Slug, NameEN: body.NameEN, NameAR: body.NameAR, LegalName: body.LegalName,
			Phone: body.Phone, Email: body.Email, Website: body.Website, Country: body.Country,
			City: body.City, Address: body.Address, Currency: body.Currency, Timezone: body.Timezone,
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
		out = append(out, item)
	}
	meta := shared.NewPageMeta(int64(res.Total), page)
	response.JSONMeta(w, http.StatusOK, out, map[string]any{
		"total": meta.Total, "limit": meta.Limit, "offset": meta.Offset, "page": meta.Page, "total_pages": meta.TotalPages,
	})
}
