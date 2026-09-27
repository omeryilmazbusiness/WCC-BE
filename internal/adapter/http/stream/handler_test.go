package stream

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/app/realtime"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type stubTokens struct{ c *platformauth.Claims }

func (s stubTokens) ParseAccess(string) (*platformauth.Claims, error) { return s.c, nil }

type stubSessions struct{}

func (stubSessions) Validate(context.Context, uuid.UUID, uuid.UUID, int) error { return nil }

func authed(claims *platformauth.Claims, next http.HandlerFunc) http.Handler {
	inner := middleware.Authenticate(stubTokens{claims}, stubSessions{})(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer t")
		inner.ServeHTTP(w, r)
	})
}

func readEvents(t *testing.T, body *bufio.Reader, n int) []string {
	t.Helper()
	var events []string
	for len(events) < n {
		line, err := body.ReadString('\n')
		if err != nil {
			t.Fatalf("read after %v: %v", events, err)
		}
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "event: "); ok {
			events = append(events, name)
		}
	}
	return events
}

func TestStreamDeliversOnlyVisibleSignalsAndEndsAtExpiry(t *testing.T) {
	hub := realtime.NewHub(8, 10)
	me, other, branch := uuid.New(), uuid.New(), uuid.New()
	claims := &platformauth.Claims{UserID: me, SessionID: uuid.New(), BranchID: branch, Role: platformauth.RoleEmployee,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(700 * time.Millisecond))}}
	srv := httptest.NewServer(authed(claims, Handler{Hub: hub, Heartbeat: time.Hour}.Stream))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	body := bufio.NewReader(res.Body)
	if got := readEvents(t, body, 1); got[0] != "ready" {
		t.Fatalf("first event = %v", got)
	}

	otherBranch := uuid.New()
	hub.Broadcast(realtime.Signal{Type: realtime.TypeNotification, UserID: &other, Topic: "task.overdue"})
	hub.Broadcast(realtime.Signal{Type: realtime.TypeInvalidate, BranchID: &otherBranch, Topic: "payment.reversed"})
	hub.Broadcast(realtime.Signal{Type: realtime.TypeNotification, UserID: &me, Topic: "task.overdue"})
	hub.Broadcast(realtime.Signal{Type: realtime.TypeInvalidate, BranchID: &branch, Topic: "payment.reversed"})

	got := readEvents(t, body, 3)
	if strings.Join(got, ",") != "notification,invalidate,expired" {
		t.Fatalf("events = %v", got)
	}
	deadline := time.Now().Add(time.Second)
	for hub.Len() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Len() != 0 {
		t.Fatal("stream still subscribed after expiry")
	}
}

func TestStreamRejectsWhenHubFull(t *testing.T) {
	hub := realtime.NewHub(1, 1)
	_, cancel, _ := hub.Subscribe(realtime.Subscriber{UserID: uuid.New()})
	defer cancel()
	claims := &platformauth.Claims{UserID: uuid.New(), BranchID: uuid.New(), Role: platformauth.RoleEmployee}
	rec := httptest.NewRecorder()
	authed(claims, Handler{Hub: hub}.Stream).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/stream", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestStreamEndsWhenServerCloses(t *testing.T) {
	hub := realtime.NewHub(8, 10)
	closing := make(chan struct{})
	claims := &platformauth.Claims{UserID: uuid.New(), SessionID: uuid.New(), BranchID: uuid.New(), Role: platformauth.RoleEmployee}
	srv := httptest.NewServer(authed(claims, Handler{Hub: hub, Heartbeat: time.Hour, Closing: closing}.Stream))
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body := bufio.NewReader(res.Body)
	readEvents(t, body, 1)
	close(closing)
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, body)
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream still open after server closing")
	}
}
