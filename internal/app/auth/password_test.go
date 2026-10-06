package auth

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// withCredentialTrigger mirrors the database: a new password hash bumps
// token_version and ends every session of the user.
func withCredentialTrigger(h *harness) {
	h.users.onUpdate = func(before, after *identity.User) {
		if before.PasswordHash != after.PasswordHash {
			after.TokenVersion = before.TokenVersion + 1
			for _, s := range h.sec.sessions {
				if s.UserID == after.ID && s.RevokedAt == nil {
					at := h.clock
					s.RevokedAt, s.RevokedReason = &at, authsec.RevokePasswordChanged
				}
			}
		}
	}
}

func fieldOf(t *testing.T, err error) string {
	t.Helper()
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want a validation error, got %v", err)
	}
	for k := range app.Details {
		return k + "=" + app.Details[k].(string)
	}
	return ""
}

func TestChangePasswordKeepsThisDeviceSignedIn(t *testing.T) {
	h := newHarness(t, platformauth.RoleGM)
	withCredentialTrigger(h)
	ctx := context.Background()
	_, other := h.mustLogin(t)
	_, current := h.mustLogin(t)

	pair, err := h.svc.ChangePassword(ctx, ChangePasswordInput{
		SessionActor:    SessionActor{UserID: h.user.ID, CurrentSessionID: current.SessionID, IP: "10.0.0.2"},
		CurrentPassword: "correct-horse", NewPassword: "Brand-new-secret-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := h.users.byID[h.user.ID]
	if bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("Brand-new-secret-42")) != nil {
		t.Fatal("new password must be stored")
	}
	if h.sec.sessions[other.SessionID].RevokedAt == nil || h.sec.sessions[current.SessionID].RevokedAt == nil {
		t.Fatal("every previous session must end")
	}
	claims, err := h.tokens.ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	fresh := h.sec.sessions[claims.SessionID]
	if fresh == nil || fresh.RevokedAt != nil || claims.SessionID == current.SessionID {
		t.Fatal("this device must get a new live session")
	}
	if claims.TokenVersion != stored.TokenVersion {
		t.Fatalf("token must carry the bumped version: %d vs %d", claims.TokenVersion, stored.TokenVersion)
	}
	if !h.audit.has("auth.password_changed") || len(h.cache.users) == 0 {
		t.Fatal("change must be audited and cached checks dropped")
	}
	if _, err := h.login("correct-horse"); err == nil {
		t.Fatal("old password must stop working")
	}
	if _, err := h.login("Brand-new-secret-42"); err != nil {
		t.Fatalf("new password must work: %v", err)
	}
}

func TestChangePasswordValidation(t *testing.T) {
	h := newHarness(t, platformauth.RoleGM)
	withCredentialTrigger(h)
	ctx := context.Background()
	actor := SessionActor{UserID: h.user.ID}
	change := func(current, next string) error {
		_, err := h.svc.ChangePassword(ctx, ChangePasswordInput{SessionActor: actor, CurrentPassword: current, NewPassword: next})
		return err
	}
	if got := fieldOf(t, change("wrong", "Brand-new-secret-42")); got != "current_password=incorrect" {
		t.Fatalf("wrong current password: %s", got)
	}
	if got := fieldOf(t, change("correct-horse", "short1")); got != "new_password=weak" {
		t.Fatalf("weak password: %s", got)
	}
	if got := fieldOf(t, change("correct-horse", "correct-horse")); got != "new_password=weak" {
		t.Fatalf("letters only is weak: %s", got)
	}
	h.users.byID[h.user.ID].PasswordHash = mustHash(t, "Same-password-1")
	if got := fieldOf(t, change("Same-password-1", "Same-password-1")); got != "new_password=unchanged" {
		t.Fatalf("unchanged password: %s", got)
	}
}

func TestChangePasswordThrottlesGuessing(t *testing.T) {
	h := newHarness(t, platformauth.RoleGM)
	ctx := context.Background()
	in := ChangePasswordInput{SessionActor: SessionActor{UserID: h.user.ID}, CurrentPassword: "guess", NewPassword: "Brand-new-secret-42"}
	for i := 0; i < passwordChangeMaxFailures; i++ {
		if _, err := h.svc.ChangePassword(ctx, in); !errors.Is(err, shared.ErrValidation) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	in.CurrentPassword = "correct-horse"
	if _, err := h.svc.ChangePassword(ctx, in); !errors.Is(err, shared.ErrRateLimited) {
		t.Fatalf("after %d misses even the right password waits: %v", passwordChangeMaxFailures, err)
	}
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}
