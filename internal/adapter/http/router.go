package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	authhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/auth"
	bookinghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/booking"
	customerhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/customer"
	dashboardhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/dashboard"
	documenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/document"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/health"
	leadhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	paymenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/payment"
	taskhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/task"
	pkghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// Handlers aggregates all HTTP handlers for wiring.
type Handlers struct {
	Health    health.Handler
	Auth      authhttp.Handler
	Customer  customerhttp.Handler
	Lead      leadhttp.Handler
	Booking   bookinghttp.Handler
	Payment   paymenthttp.Handler
	Task      taskhttp.Handler
	Dashboard dashboardhttp.Handler
	Document  documenthttp.Handler
	Package   pkghttp.Handler
}

func NewRouter(cfg config.Config, tokens *platformauth.TokenService, h Handlers) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(chimw.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(cfg.HTTP.WriteTimeout))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.HTTP.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Idempotency-Key", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           int((12 * time.Hour).Seconds()),
	}))

	r.Get("/healthz", h.Health.Live)
	r.Get("/readyz", h.Health.Ready)

	r.Route("/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.Post("/login", h.Auth.Login)
			r.Post("/refresh", h.Auth.Refresh)
			r.Group(func(r chi.Router) {
				r.Use(middleware.Authenticate(tokens))
				r.Get("/me", h.Auth.Me)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Authenticate(tokens))

			r.Route("/customers", func(r chi.Router) {
				r.Get("/", h.Customer.Search)
				r.Post("/", h.Customer.Create)
				r.Get("/{id}", h.Customer.Get)
			})

			r.Route("/leads", func(r chi.Router) {
				r.Post("/", h.Lead.Create)
				r.Post("/{id}/stage", h.Lead.ChangeStage)
			})

			r.Route("/bookings", func(r chi.Router) {
				r.Post("/", h.Booking.Create)
				r.Get("/{id}", h.Booking.Get)
				r.Patch("/{id}", h.Booking.Update)
				r.Post("/{id}/confirm", h.Booking.Confirm)
				r.Post("/{id}/status", h.Booking.ChangeStatus)
				r.Get("/{id}/participants", h.Booking.ListParticipants)
				r.Post("/{id}/participants", h.Booking.AddParticipant)
				r.Get("/{id}/payments", h.Payment.ListByBooking)
			})

			r.Post("/payments", h.Payment.Record)

			r.Route("/tasks", func(r chi.Router) {
				r.Get("/", h.Task.ListByRelated)
				r.Get("/mine", h.Task.ListMine)
				r.Post("/", h.Task.Create)
				r.Get("/{id}", h.Task.Get)
				r.Post("/{id}/status", h.Task.ChangeStatus)
				r.Post("/{id}/complete", h.Task.Complete)
				r.Post("/{id}/reschedule", h.Task.Reschedule)
			})

			r.With(middleware.RequireRoles(platformauth.RoleGM, platformauth.RoleManager)).
				Get("/dashboard/kpis", h.Dashboard.KPIs)

			r.Post("/documents/presign", h.Document.PresignUpload)

			r.Route("/packages", func(r chi.Router) {
				r.Get("/", h.Package.ListPackages)
				r.Post("/", h.Package.CreatePackage)
				r.Get("/{id}", h.Package.GetPackage)
				r.Get("/{id}/departures", h.Package.ListDepartures)
				r.Post("/{id}/departures", h.Package.CreateDeparture)
			})

			r.Route("/departures", func(r chi.Router) {
				r.Get("/{id}", h.Package.GetDeparture)
				r.Post("/{id}/clone", h.Package.CloneDeparture)
			})
		})
	})

	return r
}
