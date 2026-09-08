package payment

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
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
	IdempotencyKey string    `json:"idempotency_key"`
}

func (h Handler) Record(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	// Finance-sensitive: GM/Manager only in MVP (employee blocked).
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
		BookingID:      req.BookingID,
		Amount:         req.Amount,
		Currency:       req.Currency,
		Method:         req.Method,
		Reference:      req.Reference,
		RecordedBy:     claims.UserID,
		IdempotencyKey: key,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, p)
}
