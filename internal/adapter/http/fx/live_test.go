package fx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	appfx "github.com/wodi-crm/wodi-crm-be/internal/app/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type fakeLive struct {
	board   *appfx.LiveBoard
	adopted appfx.AdoptInput
	err     error
}

func (f *fakeLive) Board(context.Context) (*appfx.LiveBoard, error)   { return f.board, f.err }
func (f *fakeLive) Refresh(context.Context) (*appfx.LiveBoard, error) { return f.board, f.err }
func (f *fakeLive) Adopt(_ context.Context, in appfx.AdoptInput) (*domain.StoredRate, error) {
	f.adopted = in
	if f.err != nil {
		return nil, f.err
	}
	return &domain.StoredRate{
		ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Rate: domain.Rate{Base: "USD", Quote: "SYP", Scaled: 12_200_000_000, EffectiveDate: time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC),
			Source: "lirascope:official:mid"},
		CreatedBy: &in.ActorID, CreatedAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
	}, nil
}

func sampleBoard() *appfx.LiveBoard {
	at := time.Date(2026, 9, 27, 10, 9, 53, 0, time.UTC)
	return &appfx.LiveBoard{
		Board: domain.Board{Local: "SYP", UpdatedAt: at, Quotes: []domain.BoardQuote{
			{Currency: "USD", Pinned: true, USDCross: domain.RateScale,
				Official: &domain.BoardSide{Buy: 12_150_000_000, Sell: 12_250_000_000, Mid: 12_200_000_000, ObservedAt: at},
				Market:   &domain.BoardSide{Buy: 13_700_000_000, Sell: 13_775_000_000, Mid: 13_737_500_000, ObservedAt: at}},
			{Currency: "KWD"},
		}},
		Sources: []appfx.SourceStatus{
			{Info: domain.LiveSourceInfo{ID: "lirascope", Name: "LiraScope", URL: "https://lirascope.syria-cloud.sy",
				Kinds: []domain.SourceKind{domain.KindOfficial, domain.KindMarket}}, OK: true, FetchedAt: at},
			{Info: domain.LiveSourceInfo{ID: "exchangerate-api", Name: "ExchangeRate-API", URL: "https://www.exchangerate-api.com",
				Attribution: "Rates By Exchange Rate API", Kinds: []domain.SourceKind{domain.KindReference}}, Error: "status 429"},
		},
		Disclaimer: "indicative",
	}
}

func TestLiveBoardContract(t *testing.T) {
	h := LiveHandler{Svc: &fakeLive{board: sampleBoard()}}
	rec := httptest.NewRecorder()
	h.Board(rec, httptest.NewRequest(http.MethodGet, "/v1/fx/live", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "private, max-age=60" {
		t.Fatalf("status %d, cache %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	want := `{"data":{"local_currency":"SYP","updated_at":"2026-09-27T10:09:53Z","stale":false,"quotes":[` +
		`{"currency":"USD","pinned":true,` +
		`"official":{"buy":"121.50000000","sell":"122.50000000","mid":"122.00000000","observed_at":"2026-09-27T10:09:53Z","derived":false},` +
		`"market":{"buy":"137.00000000","sell":"137.75000000","mid":"137.37500000","observed_at":"2026-09-27T10:09:53Z","derived":false,"stale":false},` +
		`"usd_cross":"1.00000000"},` +
		`{"currency":"KWD","pinned":false,"official":null,"market":null,"usd_cross":null}],` +
		`"sources":[{"id":"lirascope","name":"LiraScope","url":"https://lirascope.syria-cloud.sy","kinds":["official","market"],"ok":true,"fetched_at":"2026-09-27T10:09:53Z","error":""},` +
		`{"id":"exchangerate-api","name":"ExchangeRate-API","url":"https://www.exchangerate-api.com","attribution":"Rates By Exchange Rate API","kinds":["reference"],"ok":false,"fetched_at":null,"error":"status 429"}],` +
		`"disclaimer":"indicative"}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("contract drift:\n got %s\nwant %s", got, want)
	}
}

type allowSessions struct{}

func (allowSessions) Validate(context.Context, uuid.UUID, uuid.UUID, int) error { return nil }

// authed runs h behind the real Authenticate middleware with a valid token.
func authed(t *testing.T, h http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	tokens, err := platformauth.NewTokenServiceFromConfig(config.AuthConfig{
		JWTAccessSecret: "test-access-secret-test-access-secret", JWTAccessKeyID: "t1",
		AccessTTL: time.Minute, RefreshTTL: time.Hour, Issuer: "test", Audience: "test-api",
	})
	if err != nil {
		t.Fatal(err)
	}
	at, err := tokens.IssueAccess(platformauth.AccessSubject{
		UserID: uuid.New(), Role: platformauth.RoleFinance, BranchID: uuid.New(), SessionID: uuid.New(), TokenVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+at.Token)
	rec := httptest.NewRecorder()
	middleware.Authenticate(tokens, allowSessions{})(h).ServeHTTP(rec, r)
	return rec
}

func TestLiveAdoptHandler(t *testing.T) {
	svc := &fakeLive{}
	h := LiveHandler{Svc: svc}
	body := `{"currency":"USD","kind":"official","side":"mid","effective_date":"2026-09-27"}`
	rec := authed(t, h.Adopt, httptest.NewRequest(http.MethodPost, "/v1/fx-rates/adopt", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Data RateDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.Rate != "122.00000000" || out.Data.Base != "USD" || out.Data.Quote != "SYP" || out.Data.Source != "lirascope:official:mid" {
		t.Errorf("rate: %+v", out.Data)
	}
	if svc.adopted.Currency != "USD" || svc.adopted.Kind != "official" || svc.adopted.EffectiveDate != "2026-09-27" || svc.adopted.ActorID == uuid.Nil {
		t.Errorf("input: %+v", svc.adopted)
	}

	svc.err = &shared.AppError{Code: "live_quote_unavailable", Message: "no quote", Err: shared.ErrInvalidState}
	rec = authed(t, h.Adopt, httptest.NewRequest(http.MethodPost, "/v1/fx-rates/adopt", strings.NewReader(body)))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "live_quote_unavailable") {
		t.Errorf("unavailable: %d %s", rec.Code, rec.Body)
	}
}
