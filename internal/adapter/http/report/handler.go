package report

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/report"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/report"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func (h Handler) parseFilter(r *http.Request, branchID uuid.UUID) (domain.Filter, error) {
	q := r.URL.Query()
	f := domain.Filter{BranchID: branchID}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t, err = time.Parse("2006-01-02", v)
			if err != nil {
				return f, shared.NewValidation("invalid from")
			}
		}
		f.From = t.UTC()
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t, err = time.Parse("2006-01-02", v)
			if err != nil {
				return f, shared.NewValidation("invalid to")
			}
			t = t.Add(24 * time.Hour)
		}
		f.To = t.UTC()
	}
	if v := q.Get("owner_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return f, shared.NewValidation("invalid owner_id")
		}
		f.OwnerID = &id
	}
	if v := q.Get("departure_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return f, shared.NewValidation("invalid departure_id")
		}
		f.DepartureID = &id
	}
	f.Channel = q.Get("channel")
	f.Provider = q.Get("provider")
	f.Status = q.Get("status")
	if v := q.Get("limit"); v != "" {
		n, _ := strconv.Atoi(v)
		f.Limit = n
	}
	return f, nil
}

func (h Handler) runKind(w http.ResponseWriter, r *http.Request, kind domain.Kind) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	f, err := h.parseFilter(r, claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	res, err := h.Svc.Run(r.Context(), kind, f)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func (h Handler) Sales(w http.ResponseWriter, r *http.Request) {
	h.runKind(w, r, domain.KindSales)
}
func (h Handler) Targets(w http.ResponseWriter, r *http.Request) {
	h.runKind(w, r, domain.KindTargets)
}
func (h Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	h.runKind(w, r, domain.KindReadiness)
}
func (h Handler) SLA(w http.ResponseWriter, r *http.Request) {
	h.runKind(w, r, domain.KindSLA)
}
func (h Handler) Finance(w http.ResponseWriter, r *http.Request) {
	h.runKind(w, r, domain.KindFinance)
}
func (h Handler) Integrations(w http.ResponseWriter, r *http.Request) {
	h.runKind(w, r, domain.KindIntegrations)
}

func (h Handler) Kinds(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{
		{"kind": domain.KindSales, "label": "Sales performance", "sensitive": domain.IsSensitive(domain.KindSales)},
		{"kind": domain.KindTargets, "label": "Target performance", "sensitive": domain.IsSensitive(domain.KindTargets)},
		{"kind": domain.KindReadiness, "label": "Operational readiness", "sensitive": domain.IsSensitive(domain.KindReadiness)},
		{"kind": domain.KindSLA, "label": "Communication SLA", "sensitive": domain.IsSensitive(domain.KindSLA)},
		{"kind": domain.KindFinance, "label": "Finance", "sensitive": domain.IsSensitive(domain.KindFinance)},
		{"kind": domain.KindIntegrations, "label": "Integration logs", "sensitive": domain.IsSensitive(domain.KindIntegrations)},
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Export(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	kind := domain.Kind(r.URL.Query().Get("kind"))
	if !domain.ValidKind(kind) {
		response.Error(w, shared.NewValidation("kind is required"))
		return
	}
	f, err := h.parseFilter(r, claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	csv, filename, err := h.Svc.ExportCSV(r.Context(), kind, f, claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(csv)
}
