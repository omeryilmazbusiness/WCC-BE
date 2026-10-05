package finance

import (
	"net/http"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	app "github.com/wodi-crm/wodi-crm-be/internal/app/finance"
)

// ---- receivables ----

func (h Handler) Receivables(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	rc, err := h.Hub.Receivables.Receivables(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapReceivables(rc))
}

func (h Handler) Agencies(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	items, err := h.Hub.Receivables.Agencies(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapAgencyView(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) CreateAgency(w http.ResponseWriter, r *http.Request) {
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
	var in app.AgencyInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	a, err := h.Hub.Receivables.CreateAgency(r.Context(), branchID, c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapAgency(a))
}

func (h Handler) UpdateAgency(w http.ResponseWriter, r *http.Request) {
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
	var in app.AgencyInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	a, err := h.Hub.Receivables.UpdateAgency(r.Context(), id, c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAgency(a))
}

func (h Handler) SetAgencyStatus(w http.ResponseWriter, r *http.Request) {
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
		Status string `json:"status"`
	}
	if err := decode(r, &body); err != nil {
		response.Error(w, err)
		return
	}
	a, err := h.Hub.Receivables.SetStatus(r.Context(), id, c.UserID, body.Status)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapAgency(a))
}

func (h Handler) AssignBooking(w http.ResponseWriter, r *http.Request) {
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
	bookingID, err := pathID(r, "bookingId")
	if err != nil {
		response.Error(w, err)
		return
	}
	if err := h.Hub.Receivables.AssignBooking(r.Context(), id, bookingID, c.UserID); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"assigned": true})
}

func (h Handler) UnassignBooking(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	bookingID, err := pathID(r, "bookingId")
	if err != nil {
		response.Error(w, err)
		return
	}
	if err := h.Hub.Receivables.UnassignBooking(r.Context(), bookingID, c.UserID); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"assigned": false})
}

// ---- payables ----

func (h Handler) Payables(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	p, err := h.Hub.Payables.Payables(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapPayables(p))
}

func (h Handler) PayInvoice(w http.ResponseWriter, r *http.Request) {
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
	var in app.PayInvoiceInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	in.InvoiceID = id
	m, err := h.Hub.Payables.PayInvoice(r.Context(), c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapMovement(m))
}

func (h Handler) TopUpDeposit(w http.ResponseWriter, r *http.Request) {
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
	var in app.TopUpInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	in.SupplierID = id
	m, err := h.Hub.Payables.TopUpDeposit(r.Context(), c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapMovement(m))
}

// ---- profitability ----

func (h Handler) Profitability(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	q := r.URL.Query()
	from, to, err := h.Hub.Profit.Window(q.Get("from"), q.Get("to"))
	if err != nil {
		response.Error(w, err)
		return
	}
	p, err := h.Hub.Profit.Profitability(r.Context(), branchID, from, to)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapProfitability(p))
}

func (h Handler) SetBudget(w http.ResponseWriter, r *http.Request) {
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
	var in app.BudgetInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	b, err := h.Hub.Profit.SetBudget(r.Context(), id, c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBudget(b))
}

func (h Handler) Settings(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	s, err := h.Hub.Profit.Settings(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapSettings(s))
}

func (h Handler) SetRates(w http.ResponseWriter, r *http.Request) {
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
	var in app.RatesInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	s, err := h.Hub.Profit.SetRates(r.Context(), branchID, c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapSettings(s))
}
