package booking

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
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
	profileRequest
}

type profileRequest struct {
	PNR            string `json:"pnr"`
	ServiceType    string `json:"service_type"`
	SupplierSource string `json:"supplier_source"`
	Channel        string `json:"channel"`
	Summary        string `json:"summary"`
	CompanyName    string `json:"company_name"`
}

func (p profileRequest) profile() domain.Profile {
	return domain.Profile{
		PNR: p.PNR, ServiceType: p.ServiceType, SupplierSource: p.SupplierSource,
		Channel: p.Channel, Summary: p.Summary, CompanyName: p.CompanyName,
	}
}

type updateRequest struct {
	PaxCount    int     `json:"pax_count"`
	TotalAmount int64   `json:"total_amount"`
	Currency    string  `json:"currency"`
	DiscountAmt *int64  `json:"discount_amt"`
	Notes       *string `json:"notes"`
}

type statusRequest struct {
	Status        string  `json:"status"`
	Reason        string  `json:"reason"`
	HoldExpiresAt *string `json:"hold_expires_at"`
	Override      bool    `json:"override"`
}

type participantRequest struct {
	FullName    string  `json:"full_name"`
	PassportNo  *string `json:"passport_no"`
	Nationality string  `json:"nationality"`
	DateOfBirth *string `json:"date_of_birth"`
	Gender      string  `json:"gender"`
	NationalID  *string `json:"national_id"`
	HealthOK    bool    `json:"health_ok"`
}

func newPassport(p *string) string {
	v, _ := shared.PassportUpdate(p)
	return v
}

type lineItemsRequest struct {
	Items []struct {
		Kind      string `json:"kind"`
		Category  string `json:"category"`
		Label     string `json:"label"`
		Quantity  int    `json:"quantity"`
		UnitPrice int64  `json:"unit_price"`
		UnitCost  int64  `json:"unit_cost"`
	} `json:"items"`
}

type checklistRequest struct {
	Completed bool `json:"completed"`
}

func mapBooking(b *domain.Booking, fa fieldAccess) map[string]any {
	if b == nil {
		return nil
	}
	var cost, margin int64
	if fa.financials {
		cost, margin = b.CostAmt, b.Margin()
	}
	var holdExpiresAt any
	if b.HoldExpiresAt != nil {
		holdExpiresAt = b.HoldExpiresAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"ref_code":            domain.RefCode(b.RefNo),
		"pnr":                 b.PNR,
		"service_type":        b.ServiceType,
		"supplier_source":     b.SupplierSource,
		"channel":             b.Channel,
		"summary":             b.Summary,
		"company_name":        b.CompanyName,
		"ticket_status":       b.TicketStatus(),
		"payment_status":      b.PaymentStatus(),
		"reissue_count":       b.ReissueCount,
		"info":                mapInfo(b.Info),
		"id":                  b.ID,
		"branch_id":           b.BranchID,
		"customer_id":         b.CustomerID,
		"departure_id":        b.DepartureID,
		"lead_id":             b.LeadID,
		"status":              b.Status,
		"status_changed_at":   b.StatusChangedAt.UTC().Format(time.RFC3339Nano),
		"status_reason":       b.StatusReason,
		"hold_expires_at":     holdExpiresAt,
		"allowed_transitions": domain.AllowedTransitions(b.Status, fa.override),
		"pax_count":           b.PaxCount,
		"subtotal_amt":        b.Subtotal(),
		"discount_amt":        b.DiscountAmt,
		"tax_amt":             b.TaxAmt,
		"fee_amt":             b.FeeAmt,
		"total_amount":        b.TotalAmount,
		"cost_amt":            cost,
		"margin":              margin,
		"collected_amt":       b.CollectedAmt,
		"balance_amt":         b.BalanceAmt,
		"currency":            b.Currency,
		"notes":               b.Notes,
		"owner_id":            b.OwnerID,
		"created_at":          b.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":          b.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func day(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}

func mapInfo(i domain.Info) map[string]any {
	return map[string]any{
		"customer_name":      i.CustomerName,
		"customer_name_ar":   i.CustomerNameAr,
		"owner_name":         i.OwnerName,
		"package_id":         i.PackageID,
		"package_code":       i.PackageCode,
		"package_name":       i.PackageName,
		"package_name_ar":    i.PackageNameAr,
		"package_kind":       i.PackageKind,
		"departure_code":     i.DepartureCode,
		"depart_date":        day(i.DepartDate),
		"return_date":        day(i.ReturnDate),
		"makkah_hotel":       i.MakkahHotel,
		"madinah_hotel":      i.MadinahHotel,
		"flight_routing":     i.FlightRouting,
		"participants_count": i.ParticipantsCount,
		"refunded_amt":       i.RefundedAmt,
		"overdue_schedule":   i.OverdueSchedule,
		"visa_pending":       i.VisaPending,
		"open_changes":       i.OpenChanges,
	}
}

// mapParticipant always masks the passport; the full number is only
// disclosed through the audited reveal endpoint (T-261).
func mapParticipant(p *domain.Participant) map[string]any {
	if p == nil {
		return nil
	}
	var dob any
	if p.DateOfBirth != nil {
		dob = p.DateOfBirth.Format("2006-01-02")
	}
	return map[string]any{
		"id":                p.ID,
		"booking_id":        p.BookingID,
		"full_name":         p.FullName,
		"passport_no":       shared.MaskedPassport(p.PassportNo),
		"passport_last4":    shared.PassportLast4(p.PassportNo),
		"nationality":       p.Nationality,
		"date_of_birth":     dob,
		"gender":            p.Gender,
		"national_id":       shared.MaskPassportLast4(p.NationalIDLast4),
		"national_id_last4": p.NationalIDLast4,
		"health_ok":         p.HealthOK,
		"created_at":        p.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapLine(l domain.LineItem, fa fieldAccess) map[string]any {
	var unitCost, lineCost int64
	if fa.financials {
		unitCost, lineCost = l.UnitCost, l.LineCost()
	}
	return map[string]any{
		"id":         l.ID,
		"booking_id": l.BookingID,
		"kind":       l.Kind,
		"category":   l.Category,
		"label":      l.Label,
		"quantity":   l.Quantity,
		"unit_price": l.UnitPrice,
		"unit_cost":  unitCost,
		"line_total": l.LineTotal(),
		"line_cost":  lineCost,
		"sort_order": l.SortOrder,
		"created_at": l.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": l.UpdatedAt.UTC().Format(time.RFC3339Nano),
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
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	fa := fieldAccessFor(r)
	b, err := h.Svc.CreateDraft(r.Context(), appsvc.CreateInput{
		CustomerID: req.CustomerID, DepartureID: req.DepartureID,
		LeadID: req.LeadID, PaxCount: req.PaxCount, TotalAmount: req.TotalAmount,
		DiscountAmt: req.DiscountAmt, Currency: req.Currency, Notes: req.Notes,
		CanDiscount: fa.discount, Profile: req.profile(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapBooking(b, fa))
}

// parseListInput reads the list / stats query. Dates are YYYY-MM-DD
// calendar days; "to" is inclusive and converted to an exclusive bound.
// day_end (RFC 3339) is the end of the caller's local day.
func parseListInput(r *http.Request) (appsvc.ListInput, error) {
	q := r.URL.Query()
	in := appsvc.ListInput{
		Status: domain.Status(q.Get("status")), Query: q.Get("q"),
		ServiceType: q.Get("service_type"), Channel: q.Get("channel"),
		Segment: domain.Segment(q.Get("segment")), DateField: domain.DateField(q.Get("date_field")),
		Sort: q.Get("sort"),
	}
	var err error
	ids := []struct {
		name string
		dst  **uuid.UUID
	}{
		{"branch_id", &in.BranchID}, {"customer_id", &in.CustomerID}, {"departure_id", &in.DepartureID},
		{"package_id", &in.PackageID}, {"owner_id", &in.OwnerID}, {"lead_id", &in.LeadID},
	}
	for _, p := range ids {
		if *p.dst, err = request.OptionalUUID(r, p.name); err != nil {
			return in, err
		}
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return in, shared.NewValidation("from must be YYYY-MM-DD")
		}
		in.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return in, shared.NewValidation("to must be YYYY-MM-DD")
		}
		t = t.AddDate(0, 0, 1)
		in.To = &t
	}
	if v := q.Get("day_end"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return in, shared.NewValidation("day_end must be RFC3339")
		}
		in.DayEnd = t.UTC()
	}
	if v := q.Get("limit"); v != "" {
		n, _ := strconv.Atoi(v)
		if n > 200 {
			n = 200
		}
		in.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, _ := strconv.Atoi(v)
		if n < 0 {
			n = 0
		}
		in.Offset = n
	}
	return in, nil
}

func (h Handler) Stats(w http.ResponseWriter, r *http.Request) {
	in, err := parseListInput(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	st, err := h.Svc.Stats(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, st)
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	in, err := parseListInput(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	items, total, err := h.Svc.List(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	fa := fieldAccessFor(r)
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapBooking(&items[i], fa))
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
	response.JSON(w, http.StatusOK, mapBooking(b, fieldAccessFor(r)))
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
	fa := fieldAccessFor(r)
	b, err := h.Svc.Update(r.Context(), id, appsvc.UpdateInput{
		PaxCount: req.PaxCount, TotalAmount: req.TotalAmount, Currency: req.Currency,
		DiscountAmt: req.DiscountAmt, Notes: req.Notes, CanDiscount: fa.discount,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBooking(b, fa))
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
	response.JSON(w, http.StatusOK, mapBooking(b, fieldAccessFor(r)))
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
	in, err := req.input()
	if err != nil {
		response.Error(w, err)
		return
	}
	fa := fieldAccessFor(r)
	if in.Override && !fa.override {
		response.Error(w, shared.NewForbidden("override requires bookings.override"))
		return
	}
	b, err := h.Svc.Transition(r.Context(), id, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBooking(b, fa))
}

func (req statusRequest) input() (appsvc.TransitionInput, error) {
	in := appsvc.TransitionInput{
		Status:   domain.Status(strings.TrimSpace(req.Status)),
		Reason:   req.Reason,
		Override: req.Override,
	}
	if in.Status == "" {
		return in, shared.NewValidation("status is required")
	}
	if req.HoldExpiresAt != nil && strings.TrimSpace(*req.HoldExpiresAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.HoldExpiresAt))
		if err != nil {
			return in, shared.NewValidation("hold_expires_at must be RFC3339")
		}
		t = t.UTC()
		in.HoldExpiresAt = &t
	}
	return in, nil
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
	nid := ""
	if req.NationalID != nil {
		nid = *req.NationalID
	}
	p, err := h.Svc.AddParticipant(r.Context(), id, appsvc.AddParticipantInput{
		FullName: req.FullName, PassportNo: newPassport(req.PassportNo), Nationality: req.Nationality, DateOfBirth: dob,
		Gender: req.Gender, NationalID: nid, HealthOK: req.HealthOK,
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
		Gender: req.Gender, NationalID: req.NationalID, HealthOK: req.HealthOK,
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
			Kind: it.Kind, Category: it.Category, Label: it.Label, Quantity: it.Quantity, UnitPrice: it.UnitPrice, UnitCost: it.UnitCost,
		})
	}
	b, lines, err := h.Svc.SetLineItems(r.Context(), id, inputs)
	if err != nil {
		response.Error(w, err)
		return
	}
	fa := fieldAccessFor(r)
	mapped := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		mapped = append(mapped, mapLine(l, fa))
	}
	response.JSONMeta(w, http.StatusOK, mapped, map[string]any{"booking": mapBooking(b, fa)})
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
	fa := fieldAccessFor(r)
	out := make([]map[string]any, 0, len(items))
	for _, l := range items {
		out = append(out, mapLine(l, fa))
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
