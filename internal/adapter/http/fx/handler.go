package fx

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appfx "github.com/wodi-crm/wodi-crm-be/internal/app/fx"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Service is the FX port the handler needs (ISP).
type Service interface {
	List(ctx context.Context, f domain.ListFilter) ([]domain.StoredRate, int64, error)
	Create(ctx context.Context, in appfx.CreateInput) (*domain.StoredRate, error)
	Update(ctx context.Context, in appfx.UpdateInput) (*domain.StoredRate, error)
	Delete(ctx context.Context, id, actorID uuid.UUID) error
	Convert(ctx context.Context, amount int64, from, to string, on *time.Time) (*appfx.ConvertResult, error)
}

type Handler struct {
	Svc Service
}

// RateDTO is the public FX rate contract.
type RateDTO struct {
	ID            uuid.UUID  `json:"id"`
	Base          string     `json:"base"`
	Quote         string     `json:"quote"`
	Rate          string     `json:"rate"`
	EffectiveDate string     `json:"effective_date"`
	Source        string     `json:"source"`
	CreatedBy     *uuid.UUID `json:"created_by"`
	CreatedAt     string     `json:"created_at"`
}

func toDTO(r *domain.StoredRate) RateDTO {
	return RateDTO{
		ID: r.ID, Base: r.Base, Quote: r.Quote, Rate: domain.FormatRate(r.Scaled),
		EffectiveDate: r.EffectiveDate.Format(time.DateOnly), Source: r.Source,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	page := request.Page(r)
	f := domain.ListFilter{
		Base: request.FilterString(r, "base"), Quote: request.FilterString(r, "quote"),
		Limit: page.Limit, Offset: page.Offset,
	}
	var err error
	if f.From, err = optionalDate(r, "from"); err != nil {
		response.Error(w, err)
		return
	}
	if f.To, err = optionalDate(r, "to"); err != nil {
		response.Error(w, err)
		return
	}
	items, total, err := h.Svc.List(r.Context(), f)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]RateDTO, 0, len(items))
	for i := range items {
		out = append(out, toDTO(&items[i]))
	}
	meta := shared.NewPageMeta(total, page)
	response.JSONMeta(w, http.StatusOK, out, map[string]any{
		"total": meta.Total, "limit": meta.Limit, "offset": meta.Offset, "page": meta.Page, "total_pages": meta.TotalPages,
	})
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		Base          string `json:"base"`
		Quote         string `json:"quote"`
		Rate          string `json:"rate"`
		EffectiveDate string `json:"effective_date"`
		Source        string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json (rate must be a string like \"3.75\")"))
		return
	}
	rate, err := h.Svc.Create(r.Context(), appfx.CreateInput{
		Base: body.Base, Quote: body.Quote, Rate: body.Rate, EffectiveDate: body.EffectiveDate,
		Source: body.Source, ActorID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, toDTO(rate))
}

func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
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
		Rate   string  `json:"rate"`
		Source *string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json (rate must be a string like \"3.75\")"))
		return
	}
	rate, err := h.Svc.Update(r.Context(), appfx.UpdateInput{ID: id, Rate: body.Rate, Source: body.Source, ActorID: claims.UserID})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, toDTO(rate))
}

func (h Handler) Delete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Svc.Delete(r.Context(), id, claims.UserID); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h Handler) Convert(w http.ResponseWriter, r *http.Request) {
	amount, err := strconv.ParseInt(request.FilterString(r, "amount"), 10, 64)
	if err != nil {
		response.Error(w, shared.NewValidation("amount must be an integer in minor units"))
		return
	}
	on, err := optionalDate(r, "on")
	if err != nil {
		response.Error(w, err)
		return
	}
	res, err := h.Svc.Convert(r.Context(), amount, request.FilterString(r, "from"), request.FilterString(r, "to"), on)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"amount": res.Amount, "from": res.From, "to": res.To, "converted": res.Converted,
		"rate": domain.FormatRate(res.Rate.Scaled), "effective_date": res.Rate.EffectiveDate.Format(time.DateOnly),
	})
}

func optionalDate(r *http.Request, key string) (*time.Time, error) {
	v := request.FilterString(r, key)
	if v == "" {
		return nil, nil
	}
	d, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return nil, shared.NewValidation(key + " must be YYYY-MM-DD")
	}
	return &d, nil
}
