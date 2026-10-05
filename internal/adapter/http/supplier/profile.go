package supplier

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

const maxBody = 64 << 10

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, key))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid "+key))
		return uuid.Nil, false
	}
	return id, true
}

func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func dayOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return domain.FormatDay(*t)
}

func mapSupplier(s *domain.Supplier) map[string]any {
	avail, limited := s.Finance.Available()
	markups := s.Markups
	if markups == nil {
		markups = domain.Markups{}
	}
	regions := s.Regions
	if regions == nil {
		regions = []string{}
	}
	return map[string]any{
		"id": s.ID, "branch_id": s.BranchID, "code": s.Code,
		"name_en": s.NameEn, "name_ar": s.NameAr, "category": s.Category,
		"contact_name": s.ContactName, "contact_phone": s.ContactPhone, "contact_email": s.ContactEmail,
		"emergency_phone": s.EmergencyPhone, "terms": s.Terms,
		"integration": map[string]any{
			"type": s.Integration.Type, "environment": s.Integration.Environment,
			"api_base_url": s.Integration.BaseURL, "webhook_url": s.Integration.WebhookURL,
		},
		"health": map[string]any{
			"status": s.Health.Status, "latency_ms": s.Health.LatencyMs,
			"checked_at": timeOrNil(s.Health.CheckedAt), "note": s.Health.Note,
		},
		"finance": map[string]any{
			"payment_model": s.Finance.Model, "currency": s.Finance.Currency,
			"deposit_balance": s.Finance.DepositBalance, "credit_limit": s.Finance.CreditLimit,
			"credit_used": s.Finance.CreditUsed, "low_balance_threshold": s.Finance.LowBalanceThreshold,
			"payment_terms": s.Finance.PaymentTerms, "available": avail, "limited": limited,
			"low_balance": s.Finance.LowBalance(), "exhausted": s.Finance.Exhausted(), "used_pct": s.Finance.UsedPct(),
		},
		"markups": markups, "regions": regions, "free_cancel_hours": s.FreeCancelHours,
		"contract_start": dayOrNil(s.ContractStart), "contract_end": dayOrNil(s.ContractEnd),
		"is_active":  s.IsActive,
		"created_at": s.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": s.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func withStatus(m map[string]any, s *domain.Supplier, today time.Time) map[string]any {
	m["availability"] = s.AvailabilityOn(today)
	if days, ok := s.ContractDaysLeft(today); ok {
		m["contract_days_left"] = days
	} else {
		m["contract_days_left"] = nil
	}
	return m
}

func mapMetrics(m domain.Metrics) map[string]any {
	return map[string]any{
		"searches": m.Searches, "bookings": m.Bookings, "errors": m.Errors,
		"price_changes": m.PriceChanges, "sold_outs": m.SoldOuts, "avg_latency_ms": m.AvgLatencyMs,
		"look_to_book": m.BookingsPer1000(), "error_rate_pct": m.ErrorRatePct(),
		"failed_booking_pct": m.FailedBookingPct(), "window_days": domain.MetricsWindowDays,
	}
}

func mapLedger(e *domain.LedgerEntry) map[string]any {
	return map[string]any{
		"id": e.ID, "supplier_id": e.SupplierID, "kind": e.Kind, "amount": e.Amount, "currency": e.Currency,
		"balance_after": e.BalanceAfter, "reference": e.Reference, "note": e.Note, "actor_id": e.ActorID,
		"created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapDispute(d *domain.Dispute) map[string]any {
	return map[string]any{
		"id": d.ID, "supplier_id": d.SupplierID, "title": d.Title, "booking_ref": d.BookingRef,
		"amount": d.Amount, "currency": d.Currency, "status": d.Status, "resolution": d.Resolution,
		"opened_by": d.OpenedBy, "opened_at": d.OpenedAt.UTC().Format(time.RFC3339Nano),
		"resolved_at": timeOrNil(d.ResolvedAt), "updated_at": d.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func canFinance(r *http.Request) bool {
	claims, ok := middleware.ClaimsFrom(r.Context())
	return ok && platformauth.HasPermission(claims.Role, platformauth.PermSuppliersFinance)
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	q := r.URL.Query()
	active := q.Get("active")
	items, err := h.Svc.List(r.Context(), domain.ListFilter{
		BranchID: branchID, Query: q.Get("q"), Category: q.Get("category"),
		ActiveOnly: active == "1" || active == "true",
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	today := h.Svc.Today()
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		it := &items[i]
		m := withStatus(mapSupplier(&it.Supplier), &it.Supplier, today)
		m["has_credentials"] = it.HasCredentials
		m["open_disputes"] = it.OpenDisputes
		m["spend_30d"] = it.Spend
		m["bookings_30d"] = it.Bookings
		out = append(out, m)
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var body appsvc.ProfileInput
	if !decode(w, r, &body) {
		return
	}
	body.BranchID = branchID
	s, err := h.Svc.Create(r.Context(), body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, withStatus(mapSupplier(s), s, h.Svc.Today()))
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	finance := canFinance(r)
	d, err := h.Svc.Detail(r.Context(), id, finance)
	if err != nil {
		response.Error(w, err)
		return
	}
	m := withStatus(mapSupplier(d.Supplier), d.Supplier, d.Today)
	m["credentials"] = d.CredentialHints
	m["metrics"] = mapMetrics(d.Metrics)
	m["volume"] = map[string]any{"spend": d.Volume.Spend, "bookings": d.Volume.Bookings, "refunds": d.Volume.Refunds}
	ledger := make([]map[string]any, 0, len(d.Ledger))
	for i := range d.Ledger {
		ledger = append(ledger, mapLedger(&d.Ledger[i]))
	}
	m["ledger"] = ledger
	m["can_view_ledger"] = finance
	disputes := make([]map[string]any, 0, len(d.Disputes))
	open := 0
	for i := range d.Disputes {
		disputes = append(disputes, mapDispute(&d.Disputes[i]))
		if d.Disputes[i].Status == domain.DisputeOpen {
			open++
		}
	}
	m["disputes"] = disputes
	m["open_disputes"] = open
	m["today"] = domain.FormatDay(d.Today)
	response.JSON(w, http.StatusOK, m)
}

func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body appsvc.ProfileInput
	if !decode(w, r, &body) {
		return
	}
	s, err := h.Svc.Update(r.Context(), id, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, withStatus(mapSupplier(s), s, h.Svc.Today()))
}

func (h Handler) SetActive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		IsActive *bool `json:"is_active"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.IsActive == nil {
		response.Error(w, shared.NewValidation("is_active is required"))
		return
	}
	s, err := h.Svc.SetActive(r.Context(), id, *body.IsActive)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, withStatus(mapSupplier(s), s, h.Svc.Today()))
}

func (h Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	s, err := h.Svc.HealthCheck(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, withStatus(mapSupplier(s), s, h.Svc.Today()))
}

func (h Handler) SetHealth(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !decode(w, r, &body) {
		return
	}
	s, err := h.Svc.SetHealth(r.Context(), id, body.Status, body.Note)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, withStatus(mapSupplier(s), s, h.Svc.Today()))
}

func (h Handler) RecordUsage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Day          string `json:"day"`
		Searches     int    `json:"searches"`
		Bookings     int    `json:"bookings"`
		Errors       int    `json:"errors"`
		PriceChanges int    `json:"price_changes"`
		SoldOuts     int    `json:"sold_outs"`
		LatencyMs    int    `json:"latency_ms"`
	}
	if !decode(w, r, &body) {
		return
	}
	u := domain.Usage{
		SupplierID: id, Searches: body.Searches, Bookings: body.Bookings, Errors: body.Errors,
		PriceChanges: body.PriceChanges, SoldOuts: body.SoldOuts, LatencyMs: body.LatencyMs,
	}
	if body.Day != "" {
		day, err := domain.ParseDay(body.Day)
		if err != nil {
			e := shared.NewValidation("invalid day")
			e.Details = map[string]any{"day": "YYYY-MM-DD"}
			response.Error(w, e)
			return
		}
		u.Day = day
	}
	m, err := h.Svc.RecordUsage(r.Context(), u)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapMetrics(m))
}

func (h Handler) ListLedger(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.ListLedger(r.Context(), id, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapLedger(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) PostLedger(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body appsvc.LedgerInput
	if !decode(w, r, &body) {
		return
	}
	body.SupplierID, body.ActorID = id, claims.UserID
	e, s, err := h.Svc.PostLedger(r.Context(), body)
	if err != nil && e == nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{
		"entry": mapLedger(e), "supplier": withStatus(mapSupplier(s), s, h.Svc.Today()),
	})
}

func (h Handler) Routing(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	product := r.URL.Query().Get("product")
	opts, err := h.Svc.Routing(r.Context(), branchID, product)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(opts))
	for i := range opts {
		o := &opts[i]
		out = append(out, map[string]any{
			"supplier_id": o.Supplier.ID, "code": o.Supplier.Code, "name_en": o.Supplier.NameEn,
			"name_ar": o.Supplier.NameAr, "category": o.Supplier.Category, "availability": o.Availability,
			"score": o.Score, "markup_bps": o.MarkupBps, "health": o.Supplier.Health.Status,
			"latency_ms": o.Supplier.Health.LatencyMs, "currency": o.Supplier.Finance.Currency,
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"product": product, "options": out})
}

func (h Handler) OpenDispute(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body appsvc.DisputeInput
	if !decode(w, r, &body) {
		return
	}
	body.SupplierID, body.ActorID = id, claims.UserID
	d, err := h.Svc.OpenDispute(r.Context(), body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapDispute(d))
}

func (h Handler) CloseDispute(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	disputeID, ok := pathID(w, r, "disputeId")
	if !ok {
		return
	}
	var body struct {
		Status     string `json:"status"`
		Resolution string `json:"resolution"`
	}
	if !decode(w, r, &body) {
		return
	}
	d, err := h.Svc.CloseDispute(r.Context(), id, disputeID, body.Status, body.Resolution)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapDispute(d))
}
