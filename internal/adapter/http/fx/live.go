package fx

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appfx "github.com/wodi-crm/wodi-crm-be/internal/app/fx"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const refreshTimeout = 20 * time.Second

// LiveService is the live board port the handler needs (ISP).
type LiveService interface {
	Board(ctx context.Context) (*appfx.LiveBoard, error)
	Refresh(ctx context.Context) (*appfx.LiveBoard, error)
	Adopt(ctx context.Context, in appfx.AdoptInput) (*domain.StoredRate, error)
}

// LiveHandler serves the live rate board; a nil Svc means FX_LIVE_ENABLED=false.
type LiveHandler struct {
	Svc LiveService
}

type officialDTO struct {
	Buy        string `json:"buy"`
	Sell       string `json:"sell"`
	Mid        string `json:"mid"`
	ObservedAt string `json:"observed_at"`
	Derived    bool   `json:"derived"`
}

type marketDTO struct {
	officialDTO
	Stale bool `json:"stale"`
}

type liveQuoteDTO struct {
	Currency string       `json:"currency"`
	Pinned   bool         `json:"pinned"`
	Official *officialDTO `json:"official"`
	Market   *marketDTO   `json:"market"`
	USDCross *string      `json:"usd_cross"`
}

type liveSourceDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Attribution string   `json:"attribution,omitempty"`
	Kinds       []string `json:"kinds"`
	OK          bool     `json:"ok"`
	FetchedAt   *string  `json:"fetched_at"`
	Error       string   `json:"error"`
}

// LiveBoardDTO is the public GET /v1/fx/live contract.
type LiveBoardDTO struct {
	LocalCurrency string          `json:"local_currency"`
	UpdatedAt     *string         `json:"updated_at"`
	Stale         bool            `json:"stale"`
	Quotes        []liveQuoteDTO  `json:"quotes"`
	Sources       []liveSourceDTO `json:"sources"`
	Disclaimer    string          `json:"disclaimer"`
}

func toLiveDTO(b *appfx.LiveBoard) LiveBoardDTO {
	out := LiveBoardDTO{
		LocalCurrency: b.Local, UpdatedAt: optTime(b.UpdatedAt), Stale: b.Stale,
		Quotes: make([]liveQuoteDTO, 0, len(b.Quotes)), Sources: make([]liveSourceDTO, 0, len(b.Sources)),
		Disclaimer: b.Disclaimer,
	}
	for _, q := range b.Quotes {
		dto := liveQuoteDTO{Currency: q.Currency, Pinned: q.Pinned}
		if q.Official != nil {
			o := sideDTO(q.Official)
			dto.Official = &o
		}
		if q.Market != nil {
			dto.Market = &marketDTO{officialDTO: sideDTO(q.Market), Stale: q.Market.Stale}
		}
		if q.USDCross > 0 {
			x := domain.FormatRate(q.USDCross)
			dto.USDCross = &x
		}
		out.Quotes = append(out.Quotes, dto)
	}
	for _, s := range b.Sources {
		kinds := make([]string, 0, len(s.Info.Kinds))
		for _, k := range s.Info.Kinds {
			kinds = append(kinds, string(k))
		}
		out.Sources = append(out.Sources, liveSourceDTO{
			ID: s.Info.ID, Name: s.Info.Name, URL: s.Info.URL, Attribution: s.Info.Attribution, Kinds: kinds,
			OK: s.OK, FetchedAt: optTime(s.FetchedAt), Error: s.Error,
		})
	}
	return out
}

func sideDTO(s *domain.BoardSide) officialDTO {
	return officialDTO{
		Buy: domain.FormatRate(s.Buy), Sell: domain.FormatRate(s.Sell), Mid: domain.FormatRate(s.Mid),
		ObservedAt: s.ObservedAt.UTC().Format(time.RFC3339), Derived: s.Derived,
	}
}

func optTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func (h LiveHandler) disabled(w http.ResponseWriter) bool {
	if h.Svc != nil {
		return false
	}
	response.Error(w, &shared.AppError{Code: "fx_live_disabled", Message: "live exchange rates are disabled", Err: shared.ErrNotFound})
	return true
}

func (h LiveHandler) Board(w http.ResponseWriter, r *http.Request) {
	if h.disabled(w) {
		return
	}
	b, err := h.Svc.Board(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	response.JSON(w, http.StatusOK, toLiveDTO(b))
}

func (h LiveHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if h.disabled(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), refreshTimeout)
	defer cancel()
	b, err := h.Svc.Refresh(ctx)
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.JSON(w, http.StatusOK, toLiveDTO(b))
}

func (h LiveHandler) Adopt(w http.ResponseWriter, r *http.Request) {
	if h.disabled(w) {
		return
	}
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		Currency      string `json:"currency"`
		Kind          string `json:"kind"`
		Side          string `json:"side"`
		EffectiveDate string `json:"effective_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	rate, err := h.Svc.Adopt(r.Context(), appfx.AdoptInput{
		Currency: body.Currency, Kind: body.Kind, Side: body.Side, EffectiveDate: body.EffectiveDate, ActorID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, toDTO(rate))
}
