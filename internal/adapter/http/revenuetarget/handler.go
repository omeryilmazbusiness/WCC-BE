package revenuetarget

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/revenuetarget"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/revenuetarget"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func mapTarget(t *domain.Target) map[string]any {
	if t == nil {
		return nil
	}
	m := map[string]any{
		"id": t.ID, "branch_id": t.BranchID, "label": t.Label,
		"target_amount": t.TargetAmount, "currency": t.Currency,
		"metric": t.Metric, "scope_type": t.ScopeType, "curve_type": t.CurveType,
		"period_start": t.PeriodStart.Format("2006-01-02"),
		"period_end":   t.PeriodEnd.Format("2006-01-02"),
		"created_at":   t.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":   t.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if t.OwnerID != nil {
		m["owner_id"] = *t.OwnerID
	}
	if t.TeamID != nil {
		m["team_id"] = *t.TeamID
	}
	if t.CreatedBy != nil {
		m["created_by"] = *t.CreatedBy
	}
	return m
}

func mapRevision(r domain.Revision) map[string]any {
	var before, after any
	_ = json.Unmarshal(r.BeforeJSON, &before)
	_ = json.Unmarshal(r.AfterJSON, &after)
	return map[string]any{
		"id": r.ID, "target_id": r.TargetID, "actor_id": r.ActorID,
		"action": r.Action, "before": before, "after": after,
		"created_at": r.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.List(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapTarget(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		OwnerID      *uuid.UUID `json:"owner_id"`
		TeamID       *uuid.UUID `json:"team_id"`
		Label        string     `json:"label"`
		TargetAmount int64      `json:"target_amount"`
		Currency     string     `json:"currency"`
		Metric       string     `json:"metric"`
		ScopeType    string     `json:"scope_type"`
		CurveType    string     `json:"curve_type"`
		PeriodStart  string     `json:"period_start"`
		PeriodEnd    string     `json:"period_end"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	start, err := parseDate(body.PeriodStart)
	if err != nil {
		response.Error(w, shared.NewValidation("period_start must be YYYY-MM-DD"))
		return
	}
	end, err := parseDate(body.PeriodEnd)
	if err != nil {
		response.Error(w, shared.NewValidation("period_end must be YYYY-MM-DD"))
		return
	}
	t, err := h.Svc.Create(r.Context(), appsvc.CreateInput{
		BranchID: claims.BranchID, OwnerID: body.OwnerID, TeamID: body.TeamID,
		Label: body.Label, TargetAmount: body.TargetAmount, Currency: body.Currency,
		Metric: domain.Metric(body.Metric), ScopeType: domain.ScopeType(body.ScopeType),
		CurveType: domain.CurveType(body.CurveType), PeriodStart: start, PeriodEnd: end,
		ActorID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapTarget(t))
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	t, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapTarget(t))
}

func (h Handler) Patch(w http.ResponseWriter, r *http.Request) {
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
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	in := appsvc.PatchInput{ID: id, ActorID: claims.UserID}
	if raw, ok := body["owner_id"]; ok {
		if string(raw) == "null" {
			in.ClearOwner = true
		} else {
			var oid uuid.UUID
			if err := json.Unmarshal(raw, &oid); err != nil {
				response.Error(w, shared.NewValidation("invalid owner_id"))
				return
			}
			in.OwnerID = &oid
		}
	}
	if raw, ok := body["team_id"]; ok {
		if string(raw) == "null" {
			in.ClearTeam = true
		} else {
			var tid uuid.UUID
			if err := json.Unmarshal(raw, &tid); err != nil {
				response.Error(w, shared.NewValidation("invalid team_id"))
				return
			}
			in.TeamID = &tid
		}
	}
	if raw, ok := body["label"]; ok {
		var v string
		_ = json.Unmarshal(raw, &v)
		in.Label = &v
	}
	if raw, ok := body["target_amount"]; ok {
		var v int64
		_ = json.Unmarshal(raw, &v)
		in.TargetAmount = &v
	}
	if raw, ok := body["currency"]; ok {
		var v string
		_ = json.Unmarshal(raw, &v)
		in.Currency = &v
	}
	if raw, ok := body["metric"]; ok {
		var v string
		_ = json.Unmarshal(raw, &v)
		m := domain.Metric(v)
		in.Metric = &m
	}
	if raw, ok := body["scope_type"]; ok {
		var v string
		_ = json.Unmarshal(raw, &v)
		m := domain.ScopeType(v)
		in.ScopeType = &m
	}
	if raw, ok := body["curve_type"]; ok {
		var v string
		_ = json.Unmarshal(raw, &v)
		m := domain.CurveType(v)
		in.CurveType = &m
	}
	if raw, ok := body["period_start"]; ok {
		var v string
		_ = json.Unmarshal(raw, &v)
		t, err := parseDate(v)
		if err != nil {
			response.Error(w, shared.NewValidation("period_start must be YYYY-MM-DD"))
			return
		}
		in.PeriodStart = &t
	}
	if raw, ok := body["period_end"]; ok {
		var v string
		_ = json.Unmarshal(raw, &v)
		t, err := parseDate(v)
		if err != nil {
			response.Error(w, shared.NewValidation("period_end must be YYYY-MM-DD"))
			return
		}
		in.PeriodEnd = &t
	}
	t, err := h.Svc.Update(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapTarget(t))
}

func (h Handler) ListWeights(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	weights, err := h.Svc.ListWeights(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(weights))
	for _, wrow := range weights {
		out = append(out, map[string]any{"bucket": wrow.Bucket, "weight_bps": wrow.WeightBps})
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) SetWeights(w http.ResponseWriter, r *http.Request) {
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
		Weights []struct {
			Bucket    int `json:"bucket"`
			WeightBps int `json:"weight_bps"`
		} `json:"weights"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	weights := make([]domain.Weight, 0, len(body.Weights))
	for _, wrow := range body.Weights {
		weights = append(weights, domain.Weight{Bucket: wrow.Bucket, WeightBps: wrow.WeightBps})
	}
	if err := h.Svc.SetWeights(r.Context(), id, claims.UserID, weights); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"weights": body.Weights})
}

func (h Handler) SetShares(w http.ResponseWriter, r *http.Request) {
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
		Shares []struct {
			UserID   uuid.UUID `json:"user_id"`
			ShareBps int       `json:"share_bps"`
		} `json:"shares"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	shares := make([]domain.Share, 0, len(body.Shares))
	for _, s := range body.Shares {
		shares = append(shares, domain.Share{UserID: s.UserID, ShareBps: s.ShareBps})
	}
	if err := h.Svc.SetShares(r.Context(), id, claims.UserID, shares); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"shares": body.Shares})
}

func (h Handler) Progress(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	p, err := h.Svc.Progress(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, p)
}

func (h Handler) Contributions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.Contributions(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) Series(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.Series(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) Sources(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.Sources(r.Context(), id, 100)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) Revisions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.Revisions(r.Context(), id, 50)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, rev := range items {
		out = append(out, mapRevision(rev))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Recompute(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	p, err := h.Svc.Recompute(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, p)
}

func (h Handler) RecomputeBranch(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.RecomputeBranch(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}
