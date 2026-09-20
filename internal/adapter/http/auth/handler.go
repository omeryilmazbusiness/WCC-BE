package auth

import (
	"encoding/json"
	"net/http"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
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

func (h Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	res, err := h.Svc.Login(r.Context(), appsvc.LoginInput{
		Email: req.Email, Password: req.Password, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	if res.MFARequired {
		response.JSON(w, http.StatusOK, map[string]any{
			"mfa_required":  true,
			"mfa_challenge": res.MFAChallenge,
			"user":          mapUser(res.User),
		})
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"access_token":  res.Tokens.AccessToken,
		"refresh_token": res.Tokens.RefreshToken,
		"expires_in":    res.Tokens.ExpiresIn,
		"mfa_required":  false,
		"user":          mapUser(res.User),
	})
}

func (h Handler) VerifyMFA(w http.ResponseWriter, r *http.Request) {
	var req mfaVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	res, err := h.Svc.VerifyMFA(r.Context(), appsvc.MFAVerifyInput{
		Challenge: req.Challenge, Code: req.Code, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"access_token":  res.Tokens.AccessToken,
		"refresh_token": res.Tokens.RefreshToken,
		"expires_in":    res.Tokens.ExpiresIn,
		"user":          mapUser(res.User),
	})
}

func (h Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	pair, err := h.Svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_in":    pair.ExpiresIn,
	})
}

func (h Handler) Logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	_ = h.Svc.Logout(r.Context(), appsvc.LogoutInput{
		UserID: claims.UserID, BranchID: claims.BranchID, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	response.JSON(w, http.StatusOK, payload)
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
