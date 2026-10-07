// Package support serves help requests: submitting and reading one's own, and
// the platform team's inbox.
package support

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/support"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/support"
)

const maxBody = 16 << 10

type Handler struct {
	Svc *appsvc.Service
}

func actor(r *http.Request) (appsvc.Actor, error) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return appsvc.Actor{}, shared.NewUnauthorized("unauthenticated")
	}
	return appsvc.Actor{UserID: claims.UserID, BranchID: claims.BranchID, IP: middleware.ClientIP(r), UserAgent: r.UserAgent()}, nil
}

func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(dst); err != nil {
		return shared.NewValidation("invalid JSON body")
	}
	return nil
}

type requestView struct {
	ID          string     `json:"id"`
	Number      int64      `json:"number"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Page        string     `json:"page"`
	AdminNote   string     `json:"admin_note"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
}

func view(r *domain.Request) requestView {
	return requestView{
		ID: r.ID.String(), Number: r.Number, Title: r.Title, Description: r.Description, Status: string(r.Status),
		Page: r.Page, AdminNote: r.AdminNote, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ResolvedAt: r.ResolvedAt,
	}
}

type inboundView struct {
	requestView
	Requester struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"requester"`
	Company string `json:"company"`
	Branch  string `json:"branch"`
	Locale  string `json:"locale"`
}

type submitBody struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Context     struct {
		Page   string `json:"page"`
		Locale string `json:"locale"`
	} `json:"context"`
}

// Submit: POST /v1/support/requests.
func (h Handler) Submit(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var body submitBody
	if err := decode(w, r, &body); err != nil {
		response.Error(w, err)
		return
	}
	req, err := h.Svc.Submit(r.Context(), a, domain.Draft{
		Title: body.Title, Description: body.Description, Page: body.Context.Page, Locale: body.Context.Locale,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, view(req))
}

// Mine: GET /v1/support/requests/mine.
func (h Handler) Mine(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	list, err := h.Svc.Mine(r.Context(), a.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]requestView, 0, len(list))
	for i := range list {
		out = append(out, view(&list[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

// Inbox: GET /v1/platform/support/requests?status=&q=&limit=&offset=.
func (h Handler) Inbox(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	inbox, err := h.Svc.Inbox(r.Context(), appsvc.Filter{
		Status: domain.Status(q.Get("status")), Query: q.Get("q"), Limit: limit, Offset: offset,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	items := make([]inboundView, 0, len(inbox.Items))
	for i := range inbox.Items {
		in := &inbox.Items[i]
		v := inboundView{requestView: view(&in.Request), Company: in.CompanyName, Branch: in.BranchName, Locale: in.Locale}
		v.Requester.Name, v.Requester.Email, v.Requester.Role = in.RequesterName, in.RequesterEmail, in.RequesterRole
		items = append(items, v)
	}
	counts := map[string]int{}
	for s, n := range inbox.Counts {
		counts[string(s)] = n
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "total": inbox.Total, "counts": counts})
}

type updateBody struct {
	Status string  `json:"status"`
	Note   *string `json:"note"`
}

// Update: PATCH /v1/platform/support/requests/{id}.
func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var body updateBody
	if err := decode(w, r, &body); err != nil {
		response.Error(w, err)
		return
	}
	req, err := h.Svc.Update(r.Context(), a, id, domain.Status(body.Status), body.Note)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, view(req))
}
