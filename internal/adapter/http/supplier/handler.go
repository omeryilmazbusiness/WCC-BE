package supplier

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func mapSupplier(s *domain.Supplier) map[string]any {
	return map[string]any{
		"id": s.ID, "branch_id": s.BranchID, "code": s.Code,
		"name_en": s.NameEn, "name_ar": s.NameAr,
		"contact_name": s.ContactName, "contact_phone": s.ContactPhone, "contact_email": s.ContactEmail,
		"terms": s.Terms, "is_active": s.IsActive,
		"created_at": s.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": s.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapLink(l *domain.Link) map[string]any {
	m := map[string]any{
		"id": l.ID, "supplier_id": l.SupplierID, "link_type": l.LinkType, "link_id": l.LinkID,
		"confirmation_status": l.ConfirmationStatus, "confirmation_ref": l.ConfirmationRef,
		"allotment": l.Allotment, "sold": l.Sold, "unit_cost": l.UnitCost, "currency": l.Currency,
		"notes": l.Notes, "oversold": l.IsOversold(),
		"created_at": l.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": l.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if l.ConfirmedAt != nil {
		m["confirmed_at"] = l.ConfirmedAt.UTC().Format(time.RFC3339Nano)
	}
	return m
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body appsvc.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	body.BranchID = claims.BranchID
	s, err := h.Svc.Create(r.Context(), body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapSupplier(s))
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	activeOnly := r.URL.Query().Get("active") == "1" || r.URL.Query().Get("active") == "true"
	items, err := h.Svc.List(r.Context(), claims.BranchID, activeOnly)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapSupplier(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	s, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapSupplier(s))
}

func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var body appsvc.UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	s, err := h.Svc.Update(r.Context(), id, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapSupplier(s))
}

func (h Handler) Link(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var body struct {
		LinkType  string    `json:"link_type"`
		LinkID    uuid.UUID `json:"link_id"`
		Allotment int       `json:"allotment"`
		Sold      int       `json:"sold"`
		UnitCost  int64     `json:"unit_cost"`
		Currency  string    `json:"currency"`
		Notes     string    `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	l, err := h.Svc.Link(r.Context(), appsvc.LinkInput{
		SupplierID: id, LinkType: domain.LinkType(body.LinkType), LinkID: body.LinkID,
		Allotment: body.Allotment, Sold: body.Sold, UnitCost: body.UnitCost,
		Currency: body.Currency, Notes: body.Notes,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapLink(l))
}

func (h Handler) ListLinks(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListLinks(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapLink(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Unlink(w http.ResponseWriter, r *http.Request) {
	linkID, err := uuid.Parse(chi.URLParam(r, "linkId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid link id"))
		return
	}
	if err := h.Svc.Unlink(r.Context(), linkID); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h Handler) ConfirmLink(w http.ResponseWriter, r *http.Request) {
	linkID, err := uuid.Parse(chi.URLParam(r, "linkId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid link id"))
		return
	}
	var body struct {
		Ref string `json:"confirmation_ref"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	l, err := h.Svc.Confirm(r.Context(), linkID, body.Ref)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapLink(l))
}

func (h Handler) ListUnconfirmed(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.ListUnconfirmed(r.Context(), claims.BranchID, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapLink(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) ListOversold(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.ListOversold(r.Context(), claims.BranchID, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapLink(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) ProcessUnconfirmedReminders(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	n, err := h.Svc.ProcessUnconfirmedReminders(r.Context(), claims.BranchID, 100)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"reminders_sent": n})
}

func mapInvoice(inv *domain.Invoice) map[string]any {
	m := map[string]any{
		"id": inv.ID, "branch_id": inv.BranchID, "supplier_id": inv.SupplierID,
		"invoice_number": inv.InvoiceNumber, "status": inv.Status, "currency": inv.Currency,
		"subtotal": inv.Subtotal, "tax_total": inv.TaxTotal, "grand_total": inv.GrandTotal,
		"notes": inv.Notes,
		"created_at": inv.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": inv.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if inv.IssuedOn != nil {
		m["issued_on"] = inv.IssuedOn.UTC().Format("2006-01-02")
	}
	if inv.DueOn != nil {
		m["due_on"] = inv.DueOn.UTC().Format("2006-01-02")
	}
	if inv.PaidAt != nil {
		m["paid_at"] = inv.PaidAt.UTC().Format(time.RFC3339Nano)
	}
	if inv.CreatedBy != nil {
		m["created_by"] = inv.CreatedBy
	}
	if inv.Lines != nil {
		lines := make([]map[string]any, 0, len(inv.Lines))
		for i := range inv.Lines {
			l := &inv.Lines[i]
			lm := map[string]any{
				"id": l.ID, "invoice_id": l.InvoiceID, "description": l.Description,
				"quantity": l.Quantity, "unit_cost": l.UnitCost, "line_total": l.LineTotal,
				"sort_order": l.SortOrder,
				"created_at": l.CreatedAt.UTC().Format(time.RFC3339Nano),
			}
			if l.LinkID != nil {
				lm["link_id"] = l.LinkID
			}
			lines = append(lines, lm)
		}
		m["lines"] = lines
	}
	return m
}

func (h Handler) ListInvoices(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var supplierID *uuid.UUID
	if q := r.URL.Query().Get("supplier_id"); q != "" {
		id, err := uuid.Parse(q)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid supplier_id"))
			return
		}
		supplierID = &id
	}
	var status *domain.InvoiceStatus
	if q := r.URL.Query().Get("status"); q != "" {
		st := domain.InvoiceStatus(q)
		if !domain.ValidInvoiceStatus(st) {
			response.Error(w, shared.NewValidation("invalid status"))
			return
		}
		status = &st
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.ListInvoices(r.Context(), claims.BranchID, supplierID, status, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapInvoice(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) ListSupplierInvoices(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.ListInvoices(r.Context(), claims.BranchID, &sid, nil, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapInvoice(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) CreateInvoice(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body appsvc.CreateInvoiceInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	body.BranchID = claims.BranchID
	body.ActorID = claims.UserID
	inv, err := h.Svc.CreateInvoice(r.Context(), body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapInvoice(inv))
}

func (h Handler) GetInvoice(w http.ResponseWriter, r *http.Request) {
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
	inv, err := h.Svc.GetInvoice(r.Context(), claims.BranchID, id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapInvoice(inv))
}

func (h Handler) UpdateInvoiceStatus(w http.ResponseWriter, r *http.Request) {
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
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	inv, err := h.Svc.UpdateInvoiceStatus(r.Context(), claims.BranchID, id, domain.InvoiceStatus(body.Status))
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapInvoice(inv))
}

func (h Handler) SetInvoiceLines(w http.ResponseWriter, r *http.Request) {
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
	var body []appsvc.SetInvoiceLinesInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	inv, err := h.Svc.SetInvoiceLines(r.Context(), claims.BranchID, id, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapInvoice(inv))
}
