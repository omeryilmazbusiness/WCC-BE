// Package finance exposes the finance hub: overview, treasury, receivables,
// payables, profitability, invoicing and reconciliation.
package finance

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	app "github.com/wodi-crm/wodi-crm-be/internal/app/finance"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type Handler struct {
	Hub app.Hub
}

func decode(r *http.Request, dst any) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return shared.NewValidation("request body is required")
		}
		return shared.NewValidation("invalid json")
	}
	return nil
}

func pathID(r *http.Request, key string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, key))
	if err != nil {
		return uuid.Nil, shared.NewValidation("invalid " + key)
	}
	return id, nil
}

func actor(r *http.Request) (*platformauth.Claims, error) {
	c, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return nil, shared.NewUnauthorized("unauthenticated")
	}
	return c, nil
}

func mayApprove(c *platformauth.Claims) bool {
	return platformauth.HasPermission(c.Role, platformauth.PermPaymentsApprove)
}

func now() time.Time { return time.Now().UTC() }

// ---- overview ----

func (h Handler) Overview(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	o, err := h.Hub.Overview.Overview(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapOverview(o))
}

// ---- treasury ----

func (h Handler) Accounts(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	items, err := h.Hub.Treasury.Accounts(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapAccount(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var in app.AccountInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	a, err := h.Hub.Treasury.CreateAccount(r.Context(), branchID, c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapAccount(a))
}

func (h Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		response.Error(w, err)
		return
	}
	var in app.AccountInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	a, err := h.Hub.Treasury.UpdateAccount(r.Context(), id, c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAccount(a))
}

func (h Handler) PostMovement(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		response.Error(w, err)
		return
	}
	var in app.MovementInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	in.AccountID = id
	m, a, err := h.Hub.Treasury.Post(r.Context(), c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"movement": mapMovement(m), "account": mapAccount(a)})
}

func (h Handler) Transfer(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var in app.TransferInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	legs, err := h.Hub.Treasury.Transfer(r.Context(), c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapMovements(legs))
}

func (h Handler) Movements(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	accountID, err := request.OptionalUUID(r, "account_id")
	if err != nil {
		response.Error(w, err)
		return
	}
	q := r.URL.Query()
	items, err := h.Hub.Treasury.Movements(r.Context(), app.MovementFilter{
		BranchID: branchID, AccountID: accountID, UnmatchedOnly: q.Get("unmatched") == "true", Limit: 200,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapMovements(items))
}

func (h Handler) ImportFeed(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		response.Error(w, err)
		return
	}
	var body struct {
		Rows []app.FeedRow `json:"rows"`
	}
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	res, err := h.Hub.Treasury.ImportFeed(r.Context(), id, c.UserID, mayApprove(c), body.Rows)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h Handler) MatchMovement(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		response.Error(w, err)
		return
	}
	var body struct {
		BookingID uuid.UUID `json:"booking_id"`
	}
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	if body.BookingID == uuid.Nil {
		response.Error(w, shared.NewValidation("booking_id is required"))
		return
	}
	m, err := h.Hub.Treasury.Match(r.Context(), id, body.BookingID, c.UserID, mayApprove(c))
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapMovement(m))
}

func (h Handler) IgnoreMovement(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		response.Error(w, err)
		return
	}
	if err := h.Hub.Treasury.Ignore(r.Context(), id, c.UserID); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"ignored": true})
}

func (h Handler) POSStats(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	items, err := h.Hub.Treasury.POSStats(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, s := range items {
		effective := 0
		if s.Gross > 0 {
			effective = int(s.Fees * 10_000 / s.Gross)
		}
		out = append(out, map[string]any{
			"account_id": s.AccountID, "name": s.Name, "currency": s.Currency, "commission_bps": s.CommissionBPS,
			"gross": s.Gross, "fees": s.Fees, "net": s.Gross - s.Fees, "count": s.Count, "effective_bps": effective,
		})
	}
	response.JSON(w, http.StatusOK, out)
}
