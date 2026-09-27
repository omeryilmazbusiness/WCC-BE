package search

import (
	"net/http"
	"strconv"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/search"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/search"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	branchID, err := request.OptionalUUID(r, "branch_id")
	if err != nil {
		response.Error(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	hits, err := h.Svc.Search(r.Context(), branchID, r.URL.Query().Get("q"), limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	maskPassports(hits)
	response.JSON(w, http.StatusOK, hits)
}

// maskPassports renders the last four passport characters of passport hits
// as "••••1234"; full numbers never leave search (T-260).
func maskPassports(hits []domain.Hit) {
	for i := range hits {
		if hits[i].Kind == domain.KindPassport {
			hits[i].Subtitle = shared.MaskPassportLast4(hits[i].Subtitle)
		}
	}
}
