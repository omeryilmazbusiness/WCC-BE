package payment

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type Handler struct {
	Svc *appsvc.Service
}

type recordRequest struct {
	BookingID      uuid.UUID `json:"booking_id"`
	Amount         int64     `json:"amount"`
	Currency       string    `json:"currency"`
	Method         string    `json:"method"`
	Reference      string    `json:"reference"`
	Note           string    `json:"note"`
	IdempotencyKey string    `json:"idempotency_key"`
	AutoVerify     bool      `json:"auto_verify"`
}

func mapPayment(p *domain.Payment) map[string]any {
	if p == nil {
		return nil
	}
	m := map[string]any{
		"id": p.ID, "booking_id": p.BookingID, "amount": p.Amount, "currency": p.Currency,
		"method": p.Method, "reference": p.Reference, "recorded_by": p.RecordedBy,
		"idempotency_key": p.IdempotencyKey, "event_type": p.EventType, "status": p.Status,
		"note": p.Note, "created_at": p.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if p.ReversesPaymentID != nil {
		m["reverses_payment_id"] = *p.ReversesPaymentID
	}
	if p.ApprovedBy != nil {
		m["approved_by"] = *p.ApprovedBy
	}
	if p.ApprovedAt != nil {
		m["approved_at"] = p.ApprovedAt.UTC().Format(time.RFC3339Nano)
	}
	return m
}

func mapSchedule(s *domain.Schedule) map[string]any {
	m := map[string]any{
		"id": s.ID, "booking_id": s.BookingID, "amount": s.Amount, "currency": s.Currency,
		"label": s.Label, "status": s.Status,
		"due_at": s.DueAt.UTC().Format(time.RFC3339Nano),
		"created_at": s.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": s.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if s.ReminderSentAt != nil {
		m["reminder_sent_at"] = s.ReminderSentAt.UTC().Format(time.RFC3339Nano)
	}
	return m
}

func (h Handler) Record(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	if claims.Role == platformauth.RoleEmployee {
		response.Error(w, shared.NewForbidden("employees cannot record payments"))
		return
	}
	var req recordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	key := req.IdempotencyKey
	if key == "" {
		key = r.Header.Get("Idempotency-Key")
	}
	p, err := h.Svc.Record(r.Context(), appsvc.RecordInput{
		BookingID: req.BookingID, Amount: req.Amount, Currency: req.Currency,
		Method: req.Method, Reference: req.Reference, Note: req.Note,
		RecordedBy: claims.UserID, IdempotencyKey: key, AutoVerify: req.AutoVerify,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapPayment(p))
}

func (h Handler) ListByBooking(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListByBooking(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapPayment(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) FinancialSummary(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	sum, err := h.Svc.FinancialSummary(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, sum)
}

func (h Handler) Verify(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.Svc.Verify(r.Context(), id, claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapPayment(p))
}

func (h Handler) Reverse(w http.ResponseWriter, r *http.Request) {
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
		Note           string `json:"note"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	key := body.IdempotencyKey
	if key == "" {
		key = r.Header.Get("Idempotency-Key")
	}
	if key == "" {
		key = uuid.NewString()
	}
	p, err := h.Svc.Reverse(r.Context(), appsvc.ReverseInput{
		PaymentID: id, ActorID: claims.UserID, Note: body.Note, IdempotencyKey: key,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapPayment(p))
}

func (h Handler) Adjust(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		BookingID      uuid.UUID `json:"booking_id"`
		Amount         int64     `json:"amount"`
		Currency       string    `json:"currency"`
		Note           string    `json:"note"`
		IdempotencyKey string    `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	key := body.IdempotencyKey
	if key == "" {
		key = r.Header.Get("Idempotency-Key")
	}
	p, err := h.Svc.Adjust(r.Context(), appsvc.AdjustInput{
		BookingID: body.BookingID, Amount: body.Amount, Currency: body.Currency,
		Note: body.Note, ActorID: claims.UserID, IdempotencyKey: key,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapPayment(p))
}

func (h Handler) RequestRefund(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		BookingID      uuid.UUID `json:"booking_id"`
		Amount         int64     `json:"amount"`
		Currency       string    `json:"currency"`
		Note           string    `json:"note"`
		IdempotencyKey string    `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	key := body.IdempotencyKey
	if key == "" {
		key = r.Header.Get("Idempotency-Key")
	}
	p, err := h.Svc.RequestRefund(r.Context(), appsvc.RefundInput{
		BookingID: body.BookingID, Amount: body.Amount, Currency: body.Currency,
		Note: body.Note, ActorID: claims.UserID, IdempotencyKey: key,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapPayment(p))
}

func (h Handler) ApproveRefund(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.Svc.ApproveRefund(r.Context(), id, claims.UserID, true)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapPayment(p))
}

func (h Handler) RejectRefund(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.Svc.ApproveRefund(r.Context(), id, claims.UserID, false)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapPayment(p))
}

func (h Handler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	bookingID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var body struct {
		DueAt    string `json:"due_at"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		Label    string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	due, err := time.Parse(time.RFC3339, body.DueAt)
	if err != nil {
		due, err = time.Parse(time.RFC3339Nano, body.DueAt)
	}
	if err != nil {
		response.Error(w, shared.NewValidation("due_at must be RFC3339"))
		return
	}
	sc, err := h.Svc.UpsertSchedule(r.Context(), appsvc.ScheduleInput{
		BookingID: bookingID, DueAt: due, Amount: body.Amount, Currency: body.Currency,
		Label: body.Label, ActorID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapSchedule(sc))
}

func (h Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListSchedules(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapSchedule(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) CancelSchedule(w http.ResponseWriter, r *http.Request) {
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
	sc, err := h.Svc.CancelSchedule(r.Context(), id, claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapSchedule(sc))
}

func (h Handler) Queue(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	kind := domain.QueueKind(chi.URLParam(r, "kind"))
	items, err := h.Svc.FinanceQueue(r.Context(), claims.BranchID, kind, 100)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "kind": kind})
}

func (h Handler) Export(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	kind := domain.QueueKind(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = domain.QueueOverdue
	}
	csv, err := h.Svc.ExportQueueCSV(r.Context(), claims.BranchID, kind)
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=finance-"+string(kind)+".csv")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(csv))
}

func (h Handler) ProcessReminders(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	n, err := h.Svc.ProcessPaymentDueReminders(r.Context(), 72*time.Hour, 100)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"reminders_sent": n})
}

func (h Handler) SetReportingCurrency(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		Currency string `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	if err := h.Svc.SetReportingCurrency(r.Context(), claims.BranchID, body.Currency, claims.UserID); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"reporting_currency": body.Currency})
}
