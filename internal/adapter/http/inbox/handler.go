package inbox

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appinbox "github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appinbox.Service
}

func mapConversation(c *domain.Conversation) map[string]any {
	if c == nil {
		return nil
	}
	ts := func(t *time.Time) any {
		if t == nil {
			return nil
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"id":                     c.ID,
		"branch_id":              c.BranchID,
		"channel":                c.Channel,
		"integration_account_id": c.IntegrationAccountID,
		"channel_identity_id":    c.ChannelIdentityID,
		"customer_id":            c.CustomerID,
		"lead_id":                c.LeadID,
		"owner_id":               c.OwnerID,
		"owner_name":             c.OwnerName,
		"subject":                c.Subject,
		"status":                 c.Status,
		"sla_started_at":         ts(c.SLAStartedAt),
		"sla_due_at":             ts(c.SLADueAt),
		"sla_breached_at":        ts(c.SLABreachedAt),
		"sla_stopped_at":         ts(c.SLAStoppedAt),
		"last_inbound_at":        ts(c.LastInboundAt),
		"last_outbound_at":       ts(c.LastOutboundAt),
		"unanswered_since":       ts(c.UnansweredSince),
		"last_message_preview":   c.LastMessagePreview,
		"customer_name":          c.CustomerName,
		"identity_label":         c.IdentityLabel,
		"created_at":             c.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":             c.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"sla_breached":           c.SLABreachedAt != nil,
	}
}

func mapConversations(items []domain.Conversation) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapConversation(&items[i]))
	}
	return out
}

func mapMessage(m *domain.Message) map[string]any {
	if m == nil {
		return nil
	}
	return map[string]any{
		"id":                  m.ID,
		"conversation_id":     m.ConversationID,
		"branch_id":           m.BranchID,
		"direction":           m.Direction,
		"body":                m.Body,
		"content_type":        m.ContentType,
		"provider":            m.Provider,
		"provider_message_id": m.ProviderMessageID,
		"provider_event_id":   m.ProviderEventID,
		"status":              m.Status,
		"document_id":         m.DocumentID,
		"author_user_id":      m.AuthorUserID,
		"author_name":         m.AuthorName,
		"error_message":       m.ErrorMessage,
		"created_at":          m.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapMessages(items []domain.Message) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapMessage(&items[i]))
	}
	return out
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	q := r.URL.Query()
	f := domain.ListFilter{
		BranchID: &claims.BranchID,
		Channel:  domain.Channel(q.Get("channel")),
		Status:   domain.Status(q.Get("status")),
		Query:    q.Get("q"),
	}
	if q.Get("unassigned") == "true" || q.Get("unassigned") == "1" {
		f.UnassignedOnly = true
	}
	if q.Get("mine") == "true" || q.Get("mine") == "1" {
		f.MineOnly = true
		uid := claims.UserID
		f.OwnerID = &uid
	}
	if v := q.Get("owner_id"); v != "" && !f.UnassignedOnly {
		id, err := uuid.Parse(v)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid owner_id"))
			return
		}
		f.OwnerID = &id
	}
	if q.Get("sla_breached") == "true" || q.Get("sla_breached") == "1" {
		t := true
		f.SLABreached = &t
	}
	if v := q.Get("unanswered_minutes"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			response.Error(w, shared.NewValidation("invalid unanswered_minutes"))
			return
		}
		d := time.Duration(n) * time.Minute
		f.UnansweredMin = &d
	}
	if v := q.Get("limit"); v != "" {
		n, _ := strconv.Atoi(v)
		f.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, _ := strconv.Atoi(v)
		f.Offset = n
	}
	items, total, err := h.Svc.List(r.Context(), f)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSONMeta(w, http.StatusOK, mapConversations(items), map[string]any{"total": total})
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	c, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapConversation(c))
}

func (h Handler) Messages(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.Messages(r.Context(), id, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapMessages(items))
}

type assignRequest struct {
	OwnerID string `json:"owner_id"`
}

func (h Handler) Assign(w http.ResponseWriter, r *http.Request) {
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
	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	ownerID, err := uuid.Parse(req.OwnerID)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid owner_id"))
		return
	}
	c, err := h.Svc.Assign(r.Context(), appinbox.AssignInput{
		ConversationID: id, OwnerID: ownerID, ActorID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapConversation(c))
}

type replyRequest struct {
	Body         string `json:"body"`
	InternalNote *bool  `json:"internal_note"`
}

func (h Handler) Reply(w http.ResponseWriter, r *http.Request) {
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
	var req replyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	note := req.InternalNote != nil && *req.InternalNote
	msg, err := h.Svc.Reply(r.Context(), appinbox.ReplyInput{
		ConversationID: id, Body: req.Body, ActorID: claims.UserID, InternalNote: note,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapMessage(msg))
}

type statusRequest struct {
	Status string `json:"status"`
}

func (h Handler) SetStatus(w http.ResponseWriter, r *http.Request) {
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
	var req statusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	c, err := h.Svc.SetStatus(r.Context(), appinbox.StatusInput{
		ConversationID: id,
		Status:         domain.Status(strings.TrimSpace(req.Status)),
		ActorID:        claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapConversation(c))
}

func (h Handler) Health(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	accounts, live, err := h.Svc.IntegrationHealth(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	acctOut := make([]map[string]any, 0, len(accounts))
	for i := range accounts {
		a := &accounts[i]
		var lastOK any
		if a.LastOKAt != nil {
			lastOK = a.LastOKAt.UTC().Format(time.RFC3339Nano)
		}
		path := a.WebhookPath
		if path == "" {
			path = "/v1/webhooks/" + string(a.Provider)
		}
		acctOut = append(acctOut, map[string]any{
			"id": a.ID, "branch_id": a.BranchID, "provider": a.Provider,
			"display_name": a.DisplayName, "status": a.Status,
			"connected": a.Connected, "public_meta": a.PublicMeta,
			"webhook_path": path,
			"webhook_url":  path + "?branch_id=" + claims.BranchID.String(),
			"last_ok_at":   lastOK, "last_error": a.LastError,
			"updated_at": a.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	liveOut := make([]map[string]any, 0, len(live))
	for i := range live {
		hth := &live[i]
		liveOut = append(liveOut, map[string]any{
			"provider": hth.Provider, "status": hth.Status,
			"latency_ms": hth.LatencyMS, "detail": hth.Detail,
			"checked_at": hth.CheckedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"accounts": acctOut,
		"live":     liveOut,
	})
}

func (h Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.ListAccounts(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) Connect(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	provider := domain.Channel(chi.URLParam(r, "provider"))
	var body struct {
		DisplayName   string `json:"display_name"`
		AccessToken   string `json:"access_token"`
		PhoneNumberID string `json:"phone_number_id"`
		WABAID        string `json:"waba_id"`
		DisplayPhone  string `json:"display_phone"`
		PageID        string `json:"page_id"`
		IGUserID      string `json:"ig_user_id"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		RefreshToken  string `json:"refresh_token"`
		MailboxEmail  string `json:"mailbox_email"`
		VerifyToken   string `json:"verify_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	res, err := h.Svc.Connect(r.Context(), appinbox.ConnectInput{
		BranchID: claims.BranchID, Provider: provider, DisplayName: body.DisplayName,
		AccessToken: body.AccessToken, PhoneNumberID: body.PhoneNumberID, WABAID: body.WABAID,
		DisplayPhone: body.DisplayPhone, PageID: body.PageID, IGUserID: body.IGUserID,
		ClientID: body.ClientID, ClientSecret: body.ClientSecret, RefreshToken: body.RefreshToken,
		MailboxEmail: body.MailboxEmail, VerifyToken: body.VerifyToken,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	a := res.Account
	response.JSON(w, http.StatusOK, map[string]any{
		"provider": a.Provider, "display_name": a.DisplayName, "status": a.Status,
		"connected": true, "public_meta": a.PublicMeta, "webhook_path": a.WebhookPath,
		"webhook_url": res.WebhookURL,
	})
}

func (h Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	provider := domain.Channel(chi.URLParam(r, "provider"))
	a, err := h.Svc.Disconnect(r.Context(), claims.BranchID, provider)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"provider": a.Provider, "display_name": a.DisplayName, "status": a.Status,
		"connected": false, "public_meta": a.PublicMeta, "webhook_path": a.WebhookPath,
	})
}

func (h Handler) CheckSLA(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	n, err := h.Svc.CheckSLABreaches(r.Context(), 100)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"breached": n})
}
