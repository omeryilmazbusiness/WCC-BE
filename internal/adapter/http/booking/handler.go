package booking

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type createRequest struct {
	CustomerID  uuid.UUID  `json:"customer_id"`
	DepartureID uuid.UUID  `json:"departure_id"`
	LeadID      *uuid.UUID `json:"lead_id"`
	PaxCount    int        `json:"pax_count"`
	TotalAmount int64      `json:"total_amount"`
	DiscountAmt int64      `json:"discount_amt"`
	Currency    string     `json:"currency"`
	Notes       string     `json:"notes"`
}

type updateRequest struct {
	PaxCount    int     `json:"pax_count"`
	TotalAmount int64   `json:"total_amount"`
	Currency    string  `json:"currency"`
	DiscountAmt *int64  `json:"discount_amt"`
	Notes       *string `json:"notes"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type participantRequest struct {
	FullName    string  `json:"full_name"`
	PassportNo  string  `json:"passport_no"`
	Nationality string  `json:"nationality"`
	DateOfBirth *string `json:"date_of_birth"`
}

type lineItemsRequest struct {
	Items []struct {
		Kind      string `json:"kind"`
		Label     string `json:"label"`
		Quantity  int    `json:"quantity"`
		UnitPrice int64  `json:"unit_price"`
		UnitCost  int64  `json:"unit_cost"`
	} `json:"items"`
}

type checklistRequest struct {
	Completed bool `json:"completed"`
}

func mapBooking(b *domain.Booking) map[string]any {
	if b == nil {
		return nil
	}
	return map[string]any{
		"id":            b.ID,
		"branch_id":     b.BranchID,
		"customer_id":   b.CustomerID,
		"departure_id":  b.DepartureID,
		"lead_id":       b.LeadID,
		"status":        b.Status,
		"pax_count":     b.PaxCount,
		"total_amount":  b.TotalAmount,
		"discount_amt":  b.DiscountAmt,
		"cost_amt":      b.CostAmt,
		"margin":        b.Margin(),
		"collected_amt": b.CollectedAmt,
		"balance_amt":   b.BalanceAmt,
		"currency":      b.Currency,
		"notes":         b.Notes,
		"owner_id":      b.OwnerID,
		"created_at":    b.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":    b.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapParticipant(p *domain.Participant) map[string]any {
	if p == nil {
		return nil
	}
	var dob any
	if p.DateOfBirth != nil {
		dob = p.DateOfBirth.Format("2006-01-02")
	}
	return map[string]any{
		"id":            p.ID,
		"booking_id":    p.BookingID,
		"full_name":     p.FullName,
		"passport_no":   p.PassportNo,
		"nationality":   p.Nationality,
		"date_of_birth": dob,
		"created_at":    p.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapLine(l domain.LineItem) map[string]any {
	return map[string]any{
		"id":          l.ID,
		"booking_id":  l.BookingID,
		"kind":        l.Kind,
		"label":       l.Label,
		"quantity":    l.Quantity,
		"unit_price":  l.UnitPrice,
		"unit_cost":   l.UnitCost,
		"line_total":  l.LineTotal(),
		"line_cost":   l.LineCost(),
		"sort_order":  l.SortOrder,
		"created_at":  l.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":  l.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapChecklist(c domain.ChecklistItem) map[string]any {
	var completedAt any
	if c.CompletedAt != nil {
		completedAt = c.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"id":           c.ID,
		"booking_id":   c.BookingID,
		"code":         c.Code,
		"label":        c.Label,
		"required":     c.Required,
		"completed":    c.Completed,
		"completed_at": completedAt,
		"sort_order":   c.SortOrder,
		"created_at":   c.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func parseDOB(raw *string) (*time.Time, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", *raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
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
	b, err := h.Svc.CreateDraft(r.Context(), appsvc.CreateInput{
		BranchID: claims.BranchID, CustomerID: req.CustomerID, DepartureID: req.DepartureID,
		LeadID: req.LeadID, PaxCount: req.PaxCount, TotalAmount: req.TotalAmount,
		DiscountAmt: req.DiscountAmt, Currency: req.Currency, Notes: req.Notes, OwnerID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapBooking(b))
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	q := r.URL.Query()
	in := appsvc.ListInput{BranchID: claims.BranchID, Status: domain.Status(q.Get("status")), Query: q.Get("q")}
	if v := q.Get("customer_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid customer_id"))
			return
		}
		in.CustomerID = &id
	}
	if v := q.Get("departure_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid departure_id"))
			return
		}
		in.DepartureID = &id
	}
	if v := q.Get("owner_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid owner_id"))
			return
		}
		in.OwnerID = &id
	}
	if v := q.Get("lead_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid lead_id"))
			return
		}
		in.LeadID = &id
	}
	if v := q.Get("limit"); v != "" {
		n, _ := strconv.Atoi(v)
		in.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, _ := strconv.Atoi(v)
		in.Offset = n
	}
	items, total, err := h.Svc.List(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapBooking(&items[i]))
	}
	response.JSONMeta(w, http.StatusOK, out, map[string]any{"total": total})
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	b, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBooking(b))
}

func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
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
	b, err := h.Svc.Update(r.Context(), id, appsvc.UpdateInput{
		PaxCount: req.PaxCount, TotalAmount: req.TotalAmount, Currency: req.Currency,
		DiscountAmt: req.DiscountAmt, Notes: req.Notes,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBooking(b))
}

func (h Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	b, err := h.Svc.Confirm(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBooking(b))
}

func (h Handler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req statusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	b, err := h.Svc.Transition(r.Context(), id, domain.Status(strings.TrimSpace(req.Status)))
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBooking(b))
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

func (h Handler) OverrideReadiness(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	o, err := h.Svc.OverrideReadiness(r.Context(), id, claims.UserID, body.Reason)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, o)
}

func (h Handler) AddParticipant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req participantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	dob, err := parseDOB(req.DateOfBirth)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid date_of_birth"))
		return
	}
	p, err := h.Svc.AddParticipant(r.Context(), id, appsvc.AddParticipantInput{
		FullName: req.FullName, PassportNo: req.PassportNo, Nationality: req.Nationality, DateOfBirth: dob,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapParticipant(p))
}

func (h Handler) UpdateParticipant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	pid, err := uuid.Parse(chi.URLParam(r, "participantId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid participant id"))
		return
	}
	var req participantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	dob, err := parseDOB(req.DateOfBirth)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid date_of_birth"))
		return
	}
	p, err := h.Svc.UpdateParticipant(r.Context(), id, pid, appsvc.UpdateParticipantInput{
		FullName: req.FullName, PassportNo: req.PassportNo, Nationality: req.Nationality, DateOfBirth: dob,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapParticipant(p))
}

func (h Handler) DeleteParticipant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	pid, err := uuid.Parse(chi.URLParam(r, "participantId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid participant id"))
		return
	}
	if err := h.Svc.DeleteParticipant(r.Context(), id, pid); err != nil {
		response.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) ListParticipants(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListParticipants(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapParticipant(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) SetLineItems(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req lineItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	inputs := make([]appsvc.LineItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		inputs = append(inputs, appsvc.LineItemInput{
			Kind: it.Kind, Label: it.Label, Quantity: it.Quantity, UnitPrice: it.UnitPrice, UnitCost: it.UnitCost,
		})
	}
	b, lines, err := h.Svc.SetLineItems(r.Context(), id, inputs)
	if err != nil {
		response.Error(w, err)
		return
	}
	mapped := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		mapped = append(mapped, mapLine(l))
	}
	response.JSONMeta(w, http.StatusOK, mapped, map[string]any{"booking": mapBooking(b)})
}

func (h Handler) ListLineItems(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListLineItems(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, l := range items {
		out = append(out, mapLine(l))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) ListChecklist(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListChecklist(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, c := range items {
		out = append(out, mapChecklist(c))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) UpdateChecklist(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid item id"))
		return
	}
	var req checklistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	item, err := h.Svc.UpdateChecklistItem(r.Context(), id, itemID, appsvc.ChecklistUpdateInput{Completed: req.Completed})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapChecklist(*item))
}
