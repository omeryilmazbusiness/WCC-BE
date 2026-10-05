package finance

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	app "github.com/wodi-crm/wodi-crm-be/internal/app/finance"
)

// ---- reconciliation ----

func (h Handler) Statements(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	items, err := h.Hub.Recon.Statements(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapStatement(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Statement(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		response.Error(w, err)
		return
	}
	st, lines, err := h.Hub.Recon.Statement(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapStatementDetail(st, lines))
}

func (h Handler) ImportStatement(w http.ResponseWriter, r *http.Request) {
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
	var in app.StatementInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	st, lines, err := h.Hub.Recon.ImportStatement(r.Context(), branchID, c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapStatementDetail(st, lines))
}

func (h Handler) QuoteRefund(w http.ResponseWriter, r *http.Request) {
	var in app.RefundQuoteInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	q, err := h.Hub.Recon.QuoteRefund(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapRefundQuote(q))
}

func (h Handler) Letters(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	items, err := h.Hub.Recon.Letters(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	at := now()
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapLetter(&items[i], at))
	}
	response.JSON(w, http.StatusOK, out)
}

// CreateLetter returns the raw token once; only its hash is stored.
func (h Handler) CreateLetter(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	branchID, err := request.Branch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var in app.LetterInput
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	l, token, err := h.Hub.Recon.CreateLetter(r.Context(), branchID, c.UserID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	m := mapLetter(l, now())
	m["token"] = token
	response.JSON(w, http.StatusCreated, m)
}

// ---- public (token holders) ----

func (h Handler) PublicLetter(w http.ResponseWriter, r *http.Request) {
	p, err := h.Hub.Recon.LetterByToken(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.JSON(w, http.StatusOK, mapPublicLetter(p, now()))
}

func (h Handler) RespondLetter(w http.ResponseWriter, r *http.Request) {
	var in app.LetterResponse
	if err := decode(r, &in); err != nil {
		response.Error(w, err)
		return
	}
	p, err := h.Hub.Recon.RespondLetter(r.Context(), chi.URLParam(r, "token"), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.JSON(w, http.StatusOK, mapPublicLetter(p, now()))
}
