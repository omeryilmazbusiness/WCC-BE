package audithttp

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type fakeTokens struct{ claims *platformauth.Claims }

func (f fakeTokens) ParseAccess(string) (*platformauth.Claims, error) { return f.claims, nil }

type okSessions struct{}

func (okSessions) Validate(context.Context, uuid.UUID, uuid.UUID, int) error { return nil }

type fakeSvc struct {
	events    []domain.Event
	exportErr error
	filter    domain.ListFilter
}

func (f *fakeSvc) List(_ context.Context, fl domain.ListFilter) ([]domain.Event, int64, error) {
	f.filter = fl
	return f.events, int64(len(f.events)), nil
}

func (f *fakeSvc) Actions(context.Context) ([]string, error) {
	return []string{"booking.status_changed"}, nil
}

func (f *fakeSvc) Export(_ context.Context, fl domain.ListFilter, fn func(domain.Event) error) error {
	f.filter = fl
	if f.exportErr != nil {
		return f.exportErr
	}
	for _, e := range f.events {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

func serve(t *testing.T, h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	t.Helper()
	claims := &platformauth.Claims{UserID: uuid.New(), Role: platformauth.RoleAdmin, BranchID: uuid.New(), SessionID: uuid.New()}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer x")
	rec := httptest.NewRecorder()
	middleware.Authenticate(fakeTokens{claims}, okSessions{})(h).ServeHTTP(rec, req)
	return rec
}

func sampleEvent() domain.Event {
	actor, entity, session := uuid.New(), uuid.New(), uuid.New()
	return domain.Event{
		ID: uuid.New(), ActorID: &actor, ActorName: "Ayşe", ActorType: domain.ActorUser,
		Action: "payment.reversed", EntityType: "payment", EntityID: &entity,
		Before: json.RawMessage(`{"status":"verified"}`), After: json.RawMessage(`{"amount":-100}`),
		Metadata: json.RawMessage(`{"note":"=HYPERLINK(\"x\")"}`), IP: "10.0.0.1", UserAgent: "ua",
		SessionID: &session, RequestID: "req-1", CreatedAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
	}
}

func TestListContract(t *testing.T) {
	svc := &fakeSvc{events: []domain.Event{sampleEvent(), {ID: uuid.New(), ActorType: domain.ActorSystem, Action: "x.y", EntityType: "x"}}}
	rec := serve(t, Handler{Svc: svc}.List, "/v1/audit-events?action=payment.reversed&from=2026-09-01&to=2026-09-27&limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	keys := []string{"id", "actor_id", "actor_name", "actor_type", "action", "entity_type", "entity_id", "branch_id",
		"before", "after", "extra", "ip", "user_agent", "session_id", "request_id", "created_at"}
	for _, k := range keys {
		if _, ok := body.Data[0][k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	if body.Data[0]["created_at"] != "2026-09-27T10:00:00Z" || body.Data[0]["before"].(map[string]any)["status"] != "verified" {
		t.Fatalf("row: %v", body.Data[0])
	}
	sys := body.Data[1]
	if sys["actor_id"] != nil || sys["before"] != nil || sys["after"] != nil || sys["actor_name"] != "" {
		t.Fatalf("system row: %v", sys)
	}
	if extra, ok := sys["extra"].(map[string]any); !ok || len(extra) != 0 {
		t.Fatalf("extra must be an object: %v", sys["extra"])
	}
	for _, k := range []string{"total", "limit", "offset", "page", "total_pages"} {
		if _, ok := body.Meta[k]; !ok {
			t.Errorf("missing meta %q", k)
		}
	}
	if svc.filter.To == nil || svc.filter.To.Hour() != 23 || svc.filter.Action != "payment.reversed" {
		t.Fatalf("filter: %+v", svc.filter)
	}
}

func TestListRejectsBadFilters(t *testing.T) {
	for _, q := range []string{"actor_id=nope", "entity_id=1", "from=yesterday"} {
		if rec := serve(t, Handler{Svc: &fakeSvc{}}.List, "/v1/audit-events?"+q); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d", q, rec.Code)
		}
	}
}

func TestActionsAndEmptyListReturnArrays(t *testing.T) {
	rec := serve(t, Handler{Svc: &fakeSvc{}}.List, "/v1/audit-events")
	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Fatalf("empty list: %s", rec.Body)
	}
	rec = serve(t, Handler{Svc: &fakeSvc{}}.Actions, "/v1/audit-events/actions")
	if !strings.Contains(rec.Body.String(), `"data":["booking.status_changed"]`) {
		t.Fatalf("actions: %s", rec.Body)
	}
}

func TestExportCSVNeutralizesFormulas(t *testing.T) {
	e := sampleEvent()
	e.ActorName = "=cmd|' /C calc'!A0"
	rec := serve(t, Handler{Svc: &fakeSvc{events: []domain.Event{e}}}.ExportCSV, "/v1/audit-events/export.csv")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("status %d headers %v", rec.Code, rec.Header())
	}
	rows, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rows[0], ",") != strings.Join(csvHeader, ",") || len(rows) != 2 {
		t.Fatalf("rows: %v", rows)
	}
	if rows[1][1] != "'=cmd|' /C calc'!A0" {
		t.Fatalf("actor_name not neutralized: %q", rows[1][1])
	}
	if rows[1][10] != `{"status":"verified"}` {
		t.Fatalf("before cell: %q", rows[1][10])
	}
}

func TestExportCSVErrorBeforeFirstRowIsJSON(t *testing.T) {
	rec := serve(t, Handler{Svc: &fakeSvc{exportErr: errors.New("boom")}}.ExportCSV, "/v1/audit-events/export.csv")
	if rec.Code == http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = serve(t, Handler{Svc: &fakeSvc{}}.ExportCSV, "/v1/audit-events/export.csv")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != strings.Join(csvHeader, ",") {
		t.Fatalf("empty export: %d %q", rec.Code, rec.Body)
	}
}

func TestNeutralizeFormula(t *testing.T) {
	cases := map[string]string{"=1+1": "'=1+1", "+1": "'+1", "-1": "'-1", "@SUM": "'@SUM", "abc": "abc", "": ""}
	for in, want := range cases {
		if got := neutralizeFormula(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
