package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type Handler struct {
	Svc *appsvc.Service
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type mfaVerifyRequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

type mfaCodeRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

type mfaSetupRequest struct {
	EnrollmentToken string `json:"enrollment_token"`
	Code            string `json:"code"`
}

// Login responds with exactly one of three shapes:
//   - tokens:     mfa_required=false, mfa_enrollment_required=false, access_token, refresh_token, ...
//   - challenge:  mfa_required=true, mfa_challenge → POST /auth/mfa/verify
//   - enrollment: mfa_enrollment_required=true, enrollment_token → POST /auth/mfa/setup, /auth/mfa/setup/confirm
//
// Roles with mandatory MFA receive no tokens until enrollment completes.
// Locked accounts get 423 (code account_locked, retry_after); throttled IPs get 429.
func (h Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	res, err := h.Svc.Login(r.Context(), appsvc.LoginInput{
		Email: req.Email, Password: req.Password, IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	switch {
	case res.MFARequired:
		response.JSON(w, http.StatusOK, map[string]any{
			"mfa_required":            true,
			"mfa_enrollment_required": false,
			"mfa_challenge":           res.MFAChallenge,
			"challenge_expires_in":    res.ExpiresIn,
			"user":                    mapUser(res.User),
		})
	case res.MFAEnrollmentRequired:
		response.JSON(w, http.StatusOK, map[string]any{
			"mfa_required":            false,
			"mfa_enrollment_required": true,
			"enrollment_token":        res.EnrollmentToken,
			"enrollment_expires_in":   res.ExpiresIn,
			"user":                    mapUser(res.User),
		})
	default:
		response.JSON(w, http.StatusOK, tokenPayload(res.Tokens, res.User, nil))
	}
}

func (h Handler) VerifyMFA(w http.ResponseWriter, r *http.Request) {
	var req mfaVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	res, err := h.Svc.VerifyMFA(r.Context(), appsvc.MFAVerifyInput{
		Challenge: req.Challenge, Code: req.Code, IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, tokenPayload(res.Tokens, res.User, nil))
}

func (h Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	pair, err := h.Svc.Refresh(r.Context(), appsvc.RefreshInput{
		RefreshToken: req.RefreshToken, IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"access_token":       pair.AccessToken,
		"refresh_token":      pair.RefreshToken,
		"expires_in":         pair.ExpiresIn,
		"refresh_expires_at": pair.RefreshExpiresAt,
	})
}

// Logout ends the session of a correctly signed access token (its session is
// not re-checked) and, when {"refresh_token"} is given, the session that token
// belongs to. It always answers 200 so it cannot be used to probe tokens.
func (h Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	in := appsvc.LogoutInput{RefreshToken: req.RefreshToken, IP: middleware.ClientIP(r), UserAgent: r.UserAgent()}
	if claims, ok := middleware.ClaimsFrom(r.Context()); ok {
		in.UserID, in.BranchID, in.SessionID = claims.UserID, claims.BranchID, claims.SessionID
	}
	if err := h.Svc.Logout(r.Context(), in); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListSessions returns the caller's active sessions, newest first; current
// marks the session of the calling access token.
func (h Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.ListSessions(r.Context(), claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]sessionView, 0, len(items))
	for _, s := range items {
		out = append(out, sessionView{
			ID: s.ID, CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, LastIP: s.LastIP, UserAgent: s.UserAgent,
			AuthMethod: s.AuthMethod, IdleExpiresAt: s.IdleExpiresAt, AbsoluteExpiresAt: s.AbsoluteExpiresAt,
			Current: s.ID == claims.SessionID,
		})
	}
	response.JSON(w, http.StatusOK, out)
}

// RevokeSession ends one of the caller's sessions (404 unless it is theirs
// and active). Revoking the current session is a logout.
func (h Handler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	actor, ok := sessionActor(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewNotFound("session"))
		return
	}
	if err := h.Svc.RevokeSession(r.Context(), appsvc.RevokeSessionInput{SessionActor: actor, SessionID: id}); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// RevokeOtherSessions ends every session of the caller except the current one.
func (h Handler) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	actor, ok := sessionActor(w, r)
	if !ok {
		return
	}
	n, err := h.Svc.RevokeOtherSessions(r.Context(), actor)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"revoked": n})
}

// AdminRevokeUserSessions ends every session of a user (PermUsersWrite).
func (h Handler) AdminRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid user id"))
		return
	}
	n, err := h.Svc.AdminRevokeSessions(r.Context(), appsvc.AdminRevokeSessionsInput{
		ActorID: claims.UserID, UserID: id, IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"revoked": n})
}

type sessionView struct {
	ID                uuid.UUID `json:"id"`
	CreatedAt         time.Time `json:"created_at"`
	LastSeenAt        time.Time `json:"last_seen_at"`
	LastIP            string    `json:"last_ip"`
	UserAgent         string    `json:"user_agent"`
	AuthMethod        string    `json:"auth_method"`
	IdleExpiresAt     time.Time `json:"idle_expires_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
	Current           bool      `json:"current"`
}

func sessionActor(w http.ResponseWriter, r *http.Request) (appsvc.SessionActor, bool) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return appsvc.SessionActor{}, false
	}
	return appsvc.SessionActor{
		UserID: claims.UserID, CurrentSessionID: claims.SessionID, IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	}, true
}

func (h Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	user, err := h.Svc.Me(r.Context(), claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	payload := mapUser(user)
	payload["permissions"] = platformauth.PermissionsFor(user.Role)
	scope := access.From(r.Context())
	payload["scope"] = scope.Level.String()
	if ws, ok := middleware.WorkspaceFrom(r.Context()); ok {
		addWorkspace(payload, ws, scope, user.BranchID)
	}
	response.JSON(w, http.StatusOK, payload)
}

// addWorkspace describes the caller's tenant: company-wide callers see every
// branch and act on the active one, everyone else only on their home branch.
func addWorkspace(payload map[string]any, ws *company.Workspace, scope access.Scope, home uuid.UUID) {
	payload["company"] = map[string]any{
		"id": ws.Company.ID, "slug": ws.Company.Slug, "name_en": ws.Company.NameEN, "name_ar": ws.Company.NameAR,
	}
	payload["home_branch_id"] = home
	active := home
	if scope.Level >= access.LevelCompany && scope.BranchID != uuid.Nil {
		active = scope.BranchID
	}
	payload["active_branch_id"] = active
	branches := make([]map[string]any, 0, len(ws.Branches))
	for _, b := range ws.Branches {
		if scope.Level < access.LevelCompany && b.ID != home {
			continue
		}
		branches = append(branches, map[string]any{
			"id": b.ID, "slug": b.Slug, "code": b.Code, "name_en": b.NameEN, "name_ar": b.NameAR,
			"kind": b.Kind, "is_active": b.IsActive,
		})
	}
	payload["branches"] = branches
}

// MFAEnroll starts TOTP enrollment for the signed-in user.
func (h Handler) MFAEnroll(w http.ResponseWriter, r *http.Request) {
	in, ok := h.codeInput(w, r, false)
	if !ok {
		return
	}
	res, err := h.Svc.Enroll(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

// MFAConfirm enables MFA; recovery codes are returned only here.
func (h Handler) MFAConfirm(w http.ResponseWriter, r *http.Request) {
	in, ok := h.codeInput(w, r, true)
	if !ok {
		return
	}
	codes, err := h.Svc.ConfirmEnrollment(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"mfa_enabled": true, "recovery_codes": codes})
}

func (h Handler) MFADisable(w http.ResponseWriter, r *http.Request) {
	in, ok := h.codeInput(w, r, true)
	if !ok {
		return
	}
	if err := h.Svc.DisableMFA(r.Context(), in); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"mfa_enabled": false})
}

func (h Handler) MFARecoveryCodes(w http.ResponseWriter, r *http.Request) {
	in, ok := h.codeInput(w, r, true)
	if !ok {
		return
	}
	codes, err := h.Svc.RegenerateRecoveryCodes(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// MFASetup starts enrollment with the enrollment_token returned by Login.
func (h Handler) MFASetup(w http.ResponseWriter, r *http.Request) {
	var req mfaSetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EnrollmentToken == "" {
		response.Error(w, shared.NewValidation("enrollment_token is required"))
		return
	}
	res, err := h.Svc.SetupStart(r.Context(), appsvc.MFASetupInput{
		EnrollmentToken: req.EnrollmentToken, IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

// MFASetupConfirm enables MFA and completes login (tokens + recovery codes).
func (h Handler) MFASetupConfirm(w http.ResponseWriter, r *http.Request) {
	var req mfaSetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EnrollmentToken == "" || req.Code == "" {
		response.Error(w, shared.NewValidation("enrollment_token and code are required"))
		return
	}
	res, err := h.Svc.SetupConfirm(r.Context(), appsvc.MFASetupInput{
		EnrollmentToken: req.EnrollmentToken, Code: req.Code, IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, tokenPayload(res.Tokens, res.User, res.RecoveryCodes))
}

// UnlockUser clears a login lockout (PermUsersUnlock).
func (h Handler) UnlockUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid user id"))
		return
	}
	if err := h.Svc.Unlock(r.Context(), appsvc.UnlockInput{
		ActorID: claims.UserID, UserID: id, IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	}); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"user_id": id, "unlocked": true})
}

func (h Handler) codeInput(w http.ResponseWriter, r *http.Request, needBody bool) (appsvc.MFACodeInput, bool) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return appsvc.MFACodeInput{}, false
	}
	var req mfaCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && (needBody || !errors.Is(err, io.EOF)) {
		response.Error(w, shared.NewValidation("invalid json"))
		return appsvc.MFACodeInput{}, false
	}
	return appsvc.MFACodeInput{
		UserID: claims.UserID, Code: req.Code, Password: req.Password,
		IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	}, true
}

func tokenPayload(pair *platformauth.TokenPair, user *identity.User, recoveryCodes []string) map[string]any {
	out := map[string]any{
		"access_token":            pair.AccessToken,
		"refresh_token":           pair.RefreshToken,
		"expires_in":              pair.ExpiresIn,
		"refresh_expires_at":      pair.RefreshExpiresAt,
		"mfa_required":            false,
		"mfa_enrollment_required": false,
		"user":                    mapUser(user),
	}
	if recoveryCodes != nil {
		out["recovery_codes"] = recoveryCodes
	}
	return out
}

func mapUser(user *identity.User) map[string]any {
	if user == nil {
		return nil
	}
	return map[string]any{
		"id":          user.ID,
		"email":       user.Email,
		"full_name":   user.FullName,
		"role":        user.Role,
		"branch_id":   user.BranchID,
		"team_id":     user.TeamID,
		"is_active":   user.IsActive,
		"mfa_enabled": user.MFAEnabled,
	}
}
