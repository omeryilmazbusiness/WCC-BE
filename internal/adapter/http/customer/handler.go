package customer

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/customer"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type createRequest struct {
	FullName            string          `json:"full_name"`
	FullNameAR          string          `json:"full_name_ar"`
	Phone               string          `json:"phone"`
	Email               string          `json:"email"`
	Nationality         string          `json:"nationality"`
	PassportNo          string          `json:"passport_no"`
	DateOfBirth         *string         `json:"date_of_birth"`
	Preferences         json.RawMessage `json:"preferences"`
	SpecialRequirements string          `json:"special_requirements"`
	Notes               string          `json:"notes"`
}

type updateRequest struct {
	FullName            *string         `json:"full_name"`
	FullNameAR          *string         `json:"full_name_ar"`
	Phone               *string         `json:"phone"`
	Email               *string         `json:"email"`
	Nationality         *string         `json:"nationality"`
	PassportNo          *string         `json:"passport_no"`
	DateOfBirth         *string         `json:"date_of_birth"`
	ClearDOB            bool            `json:"clear_dob"`
	Preferences         json.RawMessage `json:"preferences"`
	SpecialRequirements *string         `json:"special_requirements"`
	Notes               *string         `json:"notes"`
}

type mergeRequest struct {
	SourceID string `json:"source_id"`
}

type companionRequest struct {
	CompanionID string `json:"companion_id"`
	Relation    string `json:"relation"`
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
	dob, err := parseDOB(req.DateOfBirth)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid date_of_birth (YYYY-MM-DD)"))
		return
	}
	res, err := h.Svc.Create(r.Context(), appsvc.CreateInput{
		BranchID: claims.BranchID, FullName: req.FullName, FullNameAR: req.FullNameAR,
		Phone: req.Phone, Email: req.Email, Nationality: req.Nationality,
		PassportNo: req.PassportNo, DateOfBirth: dob, Preferences: req.Preferences,
		SpecialRequirements: req.SpecialRequirements, Notes: req.Notes, CreatedBy: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	meta := map[string]any{}
	if res.DuplicateWarn {
		meta["duplicate_warn"] = true
		meta["duplicates"] = res.Duplicates
		if len(res.Duplicates) > 0 {
			meta["duplicate_of"] = res.Duplicates[0].Customer.ID
		}
	}
	response.JSONMeta(w, http.StatusCreated, mapCustomer(res.Customer, false), meta)
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
	dob, err := parseDOB(req.DateOfBirth)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid date_of_birth (YYYY-MM-DD)"))
		return
	}
	c, err := h.Svc.Update(r.Context(), appsvc.UpdateInput{
		ID: id, FullName: req.FullName, FullNameAR: req.FullNameAR, Phone: req.Phone,
		Email: req.Email, Nationality: req.Nationality, PassportNo: req.PassportNo,
		DateOfBirth: dob, ClearDOB: req.ClearDOB, Preferences: req.Preferences,
		SpecialRequirements: req.SpecialRequirements, Notes: req.Notes,
		ActorID: claims.UserID, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapCustomer(c, false))
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
	response.JSON(w, http.StatusOK, mapCustomer(c, false))
}

func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.ClaimsFrom(r.Context())
	page := request.Page(r)
	f := domain.SearchFilter{
		Query: request.FilterString(r, "q"), Limit: page.Limit, Offset: page.Offset,
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
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapCustomer(&items[i], true))
	}
	meta := shared.NewPageMeta(int64(total), page)
	response.JSONMeta(w, http.StatusOK, out, map[string]any{
		"total": meta.Total, "limit": meta.Limit, "offset": meta.Offset,
		"page": meta.Page, "total_pages": meta.TotalPages, "sort": meta.Sort,
	})
}

func (h Handler) CheckDuplicates(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	name := request.FilterString(r, "full_name")
	phone := request.FilterString(r, "phone")
	email := request.FilterString(r, "email")
	passport := request.FilterString(r, "passport_no")
	dups, err := h.Svc.FindDuplicates(r.Context(), claims.BranchID, name, phone, email, passport)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, dups)
}

func (h Handler) Merge(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req mergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	sourceID, err := uuid.Parse(req.SourceID)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid source_id"))
		return
	}
	c, err := h.Svc.Merge(r.Context(), appsvc.MergeInput{
		SourceID: sourceID, TargetID: targetID, ActorID: claims.UserID,
		IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapCustomer(c, false))
}

func (h Handler) Timeline(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.Timeline(r.Context(), id, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) ListCompanions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListCompanions(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) LinkCompanion(w http.ResponseWriter, r *http.Request) {
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
	var req companionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	cid, err := uuid.Parse(req.CompanionID)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid companion_id"))
		return
	}
	link, err := h.Svc.LinkCompanion(r.Context(), appsvc.LinkCompanionInput{
		CustomerID: id, CompanionID: cid, Relation: req.Relation, Notes: req.Notes, ActorID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, link)
}

func (h Handler) UnlinkCompanion(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	cid, err := uuid.Parse(chi.URLParam(r, "companionId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid companion id"))
		return
	}
	if err := h.Svc.UnlinkCompanion(r.Context(), id, cid); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func mapCustomer(c *domain.Customer, maskPassport bool) map[string]any {
	if c == nil {
		return nil
	}
	passport := c.PassportNo
	if maskPassport {
		passport = shared.MaskPassport(passport)
	}
	var dob any
	if c.DateOfBirth != nil {
		dob = c.DateOfBirth.Format("2006-01-02")
	}
	prefs := c.Preferences
	if len(prefs) == 0 {
		prefs = json.RawMessage(`{}`)
	}
	return map[string]any{
		"id": c.ID, "branch_id": c.BranchID, "full_name": c.FullName, "full_name_ar": c.FullNameAR,
		"phone": c.Phone, "email": c.Email, "nationality": c.Nationality,
		"passport_no": passport, "date_of_birth": dob, "preferences": prefs,
		"special_requirements": c.SpecialRequirements, "notes": c.Notes,
		"merged_into_id": c.MergedIntoID, "is_active": c.IsActive,
		"created_by": c.CreatedBy, "created_at": c.CreatedAt, "updated_at": c.UpdatedAt,
	}
}

func parseDOB(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(*raw))
	if err != nil {
		return nil, err
	}
	return &t, nil
}
