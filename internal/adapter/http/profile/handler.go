// Package profile exposes the caller's own profile under /v1/me.
package profile

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/profile"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type updateRequest struct {
	FullName *string `json:"full_name"`
	JobTitle *string `json:"job_title"`
	Phone    *string `json:"phone"`
}

type emailRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type teamView struct {
	ID     string `json:"id"`
	NameEN string `json:"name_en"`
	NameAR string `json:"name_ar"`
}

type profileView struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	FullName      string    `json:"full_name"`
	JobTitle      string    `json:"job_title"`
	Phone         string    `json:"phone"`
	Role          string    `json:"role"`
	BranchID      *string   `json:"branch_id"`
	Team          *teamView `json:"team"`
	MFAEnabled    bool      `json:"mfa_enabled"`
	AvatarVersion *string   `json:"avatar_version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func mapProfile(p *appsvc.Profile) profileView {
	u := p.User
	v := profileView{
		ID: u.ID.String(), Email: u.Email, FullName: u.FullName, JobTitle: u.JobTitle, Phone: u.Phone,
		Role: string(u.Role), MFAEnabled: u.MFAEnabled, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
	if u.BranchID != uuid.Nil {
		id := u.BranchID.String()
		v.BranchID = &id
	}
	if p.Team != nil {
		v.Team = &teamView{ID: p.Team.ID.String(), NameEN: p.Team.NameEN, NameAR: p.Team.NameAR}
	}
	if u.AvatarUpdatedAt != nil {
		version := identity.AvatarVersion(*u.AvatarUpdatedAt)
		v.AvatarVersion = &version
	}
	return v
}

func actor(r *http.Request) (appsvc.Actor, error) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return appsvc.Actor{}, shared.NewUnauthorized("unauthenticated")
	}
	return appsvc.Actor{UserID: claims.UserID, IP: middleware.ClientIP(r), UserAgent: r.UserAgent()}, nil
}

func (h Handler) respond(w http.ResponseWriter, p *appsvc.Profile, err error) {
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapProfile(p))
}

// Get: GET /me/profile.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	p, err := h.Svc.Get(r.Context(), a.UserID)
	h.respond(w, p, err)
}

// Update: PATCH /me/profile with any of full_name, job_title, phone.
func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	p, err := h.Svc.Update(r.Context(), a, appsvc.UpdateInput{FullName: req.FullName, JobTitle: req.JobTitle, Phone: req.Phone})
	h.respond(w, p, err)
}

// ChangeEmail: PUT /me/email with the new address and the current password.
func (h Handler) ChangeEmail(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var req emailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	p, err := h.Svc.ChangeEmail(r.Context(), a, appsvc.ChangeEmailInput{Email: req.Email, Password: req.Password})
	h.respond(w, p, err)
}

// Avatar: GET /me/avatar streams the caller's photo; it is never scripted
// (raster only, CSP none) and private to the signed-in user.
func (h Handler) Avatar(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	avatar, err := h.Svc.Avatar(r.Context(), a.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	etag := `"` + identity.AvatarVersion(avatar.UpdatedAt) + `"`
	hdr := w.Header()
	hdr.Set("ETag", etag)
	hdr.Set("Cache-Control", "private, max-age=300")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hdr.Set("Content-Type", avatar.ContentType)
	hdr.Set("Content-Length", strconv.Itoa(len(avatar.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(avatar.Data)
}

// UploadAvatar: PUT /me/avatar with the raw image as the body; the type is
// sniffed from the bytes.
func (h Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, identity.MaxAvatarBytes+1))
	if err != nil {
		response.Error(w, shared.NewValidation("avatar: could not read upload"))
		return
	}
	p, err := h.Svc.SetAvatar(r.Context(), a, data)
	h.respond(w, p, err)
}

// DeleteAvatar: DELETE /me/avatar.
func (h Handler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	p, err := h.Svc.DeleteAvatar(r.Context(), a)
	h.respond(w, p, err)
}
