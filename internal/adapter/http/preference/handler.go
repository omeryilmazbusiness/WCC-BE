package preference

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/preference"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/preference"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Handler serves the caller's own preferences; no permission beyond sign-in.
type Handler struct {
	Svc *appsvc.Service
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	p, err := h.Svc.Get(r.Context(), claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, toJSON(p))
}

// Update replaces the sidebar shortcuts; `"nav_favorites": null` restores the default.
func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		NavFavorites []string `json:"nav_favorites"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	p, err := h.Svc.SetNavFavorites(r.Context(), claims.UserID, body.NavFavorites)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, toJSON(p))
}

func toJSON(p *domain.Preferences) map[string]any {
	out := map[string]any{"nav_favorites": p.NavFavorites, "max_nav_favorites": domain.MaxNavFavorites}
	if !p.UpdatedAt.IsZero() {
		out["updated_at"] = p.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}
