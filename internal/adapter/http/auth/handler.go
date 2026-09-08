package auth

import (
	"encoding/json"
	"net/http"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
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

func (h Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	res, err := h.Svc.Login(r.Context(), appsvc.LoginInput{
		Email:     req.Email,
		Password:  req.Password,
		IP:        r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"access_token":  res.Tokens.AccessToken,
		"refresh_token": res.Tokens.RefreshToken,
		"expires_in":    res.Tokens.ExpiresIn,
		"user": map[string]any{
			"id":        res.User.ID,
			"email":     res.User.Email,
			"full_name": res.User.FullName,
			"role":      res.User.Role,
			"branch_id": res.User.BranchID,
		},
	})
}

func (h Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, err)
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

func (h Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, nil)
		return
	}
	user, err := h.Svc.Me(r.Context(), claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"id":        user.ID,
		"email":     user.Email,
		"full_name": user.FullName,
		"role":      user.Role,
		"branch_id": user.BranchID,
		"team_id":   user.TeamID,
	})
}
