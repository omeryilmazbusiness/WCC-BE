// Package branding exposes a company's public sign-in identity (name, logo)
// and the GM's logo management.
package branding

import (
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/branding"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

// MapBranding renders the public payload. logo_version changes with every
// upload so clients can cache the logo URL forever.
func MapBranding(b *domain.Branding) map[string]any {
	out := map[string]any{
		"slug": b.Slug, "name_en": b.NameEN, "name_ar": b.NameAR,
		"has_logo": b.LogoUpdatedAt != nil, "logo_version": nil,
	}
	if b.LogoUpdatedAt != nil {
		out["logo_version"] = domain.LogoVersion(*b.LogoUpdatedAt)
	}
	return out
}

// Public is the unauthenticated sign-in page lookup.
func (h Handler) Public(w http.ResponseWriter, r *http.Request) {
	b, err := h.Svc.Branding(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, MapBranding(b))
}

// PublicLogo streams the logo bytes; never scripted (raster only, CSP none).
func (h Handler) PublicLogo(w http.ResponseWriter, r *http.Request) {
	logo, err := h.Svc.Logo(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		response.Error(w, err)
		return
	}
	etag := `"` + domain.LogoVersion(logo.UpdatedAt) + `"`
	hdr := w.Header()
	hdr.Set("ETag", etag)
	hdr.Set("Cache-Control", "public, max-age=300")
	hdr.Set("Last-Modified", logo.UpdatedAt.UTC().Format(http.TimeFormat))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hdr.Set("Content-Type", logo.ContentType)
	hdr.Set("Content-Length", strconv.Itoa(len(logo.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(logo.Data)
}

func actor(r *http.Request) (appsvc.Actor, error) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return appsvc.Actor{}, shared.NewUnauthorized("unauthenticated")
	}
	s := access.From(r.Context())
	if s.CompanyID == uuid.Nil {
		return appsvc.Actor{}, shared.NewForbidden("account is not linked to a company")
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		return appsvc.Actor{}, err
	}
	return appsvc.Actor{
		CompanyID: s.CompanyID, BranchID: branchID, UserID: claims.UserID,
		IP: middleware.ClientIP(r), UserAgent: r.UserAgent(),
	}, nil
}

// platformActor acts on the company in the URL (platform admin; the route
// requires companies.manage and the repository a global scope).
func platformActor(r *http.Request) (appsvc.Actor, error) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return appsvc.Actor{}, shared.NewUnauthorized("unauthenticated")
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return appsvc.Actor{}, shared.NewNotFound("company")
	}
	return appsvc.Actor{CompanyID: id, UserID: claims.UserID, IP: middleware.ClientIP(r), UserAgent: r.UserAgent()}, nil
}

func (h Handler) upload(w http.ResponseWriter, r *http.Request, a appsvc.Actor) {
	data, err := io.ReadAll(io.LimitReader(r.Body, domain.MaxLogoBytes+1))
	if err != nil {
		response.Error(w, shared.NewValidation("logo: could not read upload"))
		return
	}
	b, err := h.Svc.SetLogo(r.Context(), a, data)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, MapBranding(b))
}

func (h Handler) remove(w http.ResponseWriter, r *http.Request, a appsvc.Actor) {
	b, err := h.Svc.DeleteLogo(r.Context(), a)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, MapBranding(b))
}

// PlatformUploadLogo sets any company's logo (platform console).
func (h Handler) PlatformUploadLogo(w http.ResponseWriter, r *http.Request) {
	a, err := platformActor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	h.upload(w, r, a)
}

func (h Handler) PlatformDeleteLogo(w http.ResponseWriter, r *http.Request) {
	a, err := platformActor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	h.remove(w, r, a)
}

// UploadLogo takes the raw image as the request body (any image Content-Type;
// the real type is sniffed from the bytes).
func (h Handler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	h.upload(w, r, a)
}

func (h Handler) DeleteLogo(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	h.remove(w, r, a)
}
