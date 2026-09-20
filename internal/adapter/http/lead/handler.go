package lead

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type createRequest struct {
	CustomerID *uuid.UUID `json:"customer_id"`
	FullName   string     `json:"full_name"`
	Phone      string     `json:"phone"`
	Source     string     `json:"source"`
	Notes      string     `json:"notes"`
	OwnerID    *uuid.UUID `json:"owner_id"`
}

type stageRequest struct {
	Stage          string `json:"stage"`
	Note           string `json:"note"`
	LostReasonCode string `json:"lost_reason_code"`
	LostReason     string `json:"lost_reason"`
}

type assignRequest struct {
	OwnerID string   `json:"owner_id"`
	LeadIDs []string `json:"lead_ids"`
}

type convertRequest struct {
	DepartureID string `json:"departure_id"`
	PaxCount    int    `json:"pax_count"`
	TotalAmount int64  `json:"total_amount"`
	Currency    string `json:"currency"`
}

type noFollowUpRequest struct {
	NoFollowUp bool `json:"no_follow_up"`
}

func mapLead(l *domain.Lead) map[string]any {
	if l == nil {
		return nil
	}
	return map[string]any{
		"id":                   l.ID,
		"branch_id":            l.BranchID,
		"customer_id":          l.CustomerID,
		"full_name":            l.FullName,
		"phone":                l.Phone,
		"source":               l.Source,
		"stage":                l.Stage,
		"owner_id":             l.OwnerID,
		"owner_name":           l.OwnerName,
		"lost_reason_code":     l.LostReasonCode,
		"lost_reason":          l.LostReason,
		"notes":                l.Notes,
		"no_follow_up":         l.NoFollowUp,
		"converted_booking_id": l.ConvertedBookingID,
		"created_at":           l.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":           l.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapHistory(h domain.StageHistory) map[string]any {
	var from any
	if h.FromStage != nil {
		from = string(*h.FromStage)
	}
	return map[string]any{
		"id":         h.ID,
		"lead_id":    h.LeadID,
		"from_stage": from,
		"to_stage":   h.ToStage,
		"changed_by": h.ChangedBy,
		"note":       h.Note,
		"created_at": h.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
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
	owner := claims.UserID
	if req.OwnerID != nil {
		owner = *req.OwnerID
	}
	l, err := h.Svc.Create(r.Context(), appsvc.CreateInput{
		BranchID: claims.BranchID, CustomerID: req.CustomerID, FullName: req.FullName,
		Phone: req.Phone, Source: req.Source, OwnerID: owner, Notes: req.Notes,
		ActorID: claims.UserID, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapLead(l))
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	page := request.Page(r)
	f := domain.ListFilter{
		Query: request.FilterString(r, "q"), Limit: page.Limit, Offset: page.Offset,
	}
	bid := claims.BranchID
	f.BranchID = middleware.ScopeBranch(claims, &bid)
	if oid := request.FilterString(r, "owner_id"); oid != "" {
		id, err := uuid.Parse(oid)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid owner_id"))
			return
		}
		f.OwnerID = &id
	}
	if st := request.FilterString(r, "stage"); st != "" {
		f.Stage = domain.Stage(st)
	}
	if nf := request.FilterString(r, "no_follow_up"); nf != "" {
		v := nf == "1" || strings.EqualFold(nf, "true")
		f.NoFollowUp = &v
	}
	items, total, err := h.Svc.List(r.Context(), f)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapLead(&items[i]))
	}
	meta := shared.NewPageMeta(int64(total), page)
	response.JSONMeta(w, http.StatusOK, out, map[string]any{
		"total": meta.Total, "limit": meta.Limit, "offset": meta.Offset,
		"page": meta.Page, "total_pages": meta.TotalPages,
	})
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	l, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapLead(l))
}

func (h Handler) ChangeStage(w http.ResponseWriter, r *http.Request) {
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
	var req stageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	l, err := h.Svc.ChangeStage(r.Context(), appsvc.ChangeStageInput{
		LeadID: id, To: domain.Stage(req.Stage), Note: req.Note,
		LostReasonCode: req.LostReasonCode, LostReasonNote: req.LostReason,
		ActorID: claims.UserID, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapLead(l))
}

func (h Handler) Assign(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	ownerID, err := uuid.Parse(req.OwnerID)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid owner_id"))
		return
	}
	ids := make([]uuid.UUID, 0, len(req.LeadIDs))
	// Single-assign path: /leads/{id}/assign
	if pathID := chi.URLParam(r, "id"); pathID != "" {
		id, err := uuid.Parse(pathID)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid id"))
			return
		}
		ids = append(ids, id)
	}
	for _, raw := range req.LeadIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid lead_ids"))
			return
		}
		ids = append(ids, id)
	}
	items, err := h.Svc.Assign(r.Context(), appsvc.AssignInput{
		LeadIDs: ids, OwnerID: ownerID, ActorID: claims.UserID,
		IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapLead(&items[i]))
	}
	if len(out) == 1 && chi.URLParam(r, "id") != "" {
		response.JSON(w, http.StatusOK, out[0])
		return
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) History(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.History(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, mapHistory(it))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Convert(w http.ResponseWriter, r *http.Request) {
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
	var req convertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	depID, err := uuid.Parse(req.DepartureID)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid departure_id"))
		return
	}
	res, err := h.Svc.Convert(r.Context(), appsvc.ConvertInput{
		LeadID: id, DepartureID: depID, PaxCount: req.PaxCount,
		TotalAmount: req.TotalAmount, Currency: req.Currency,
		ActorID: claims.UserID, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"lead":       mapLead(res.Lead),
		"booking_id": res.BookingID,
	})
}

func (h Handler) SetNoFollowUp(w http.ResponseWriter, r *http.Request) {
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
	var req noFollowUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	l, err := h.Svc.SetNoFollowUp(r.Context(), appsvc.SetNoFollowUpInput{
		LeadID: id, NoFollowUp: req.NoFollowUp, ActorID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapLead(l))
}

func (h Handler) Analytics(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	a, err := h.Svc.Analytics(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, a)
}

func (h Handler) LostReasons(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, domain.LostReasonCodes())
}
