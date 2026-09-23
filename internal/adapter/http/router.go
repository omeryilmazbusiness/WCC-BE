package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	authhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/auth"
	audithttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/audithttp"
	bookinghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/booking"
	customerhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/customer"
	dashboardhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/dashboard"
	documenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/document"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/health"
	leadhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	opshttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/ops"
	paymenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/payment"
	taskhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/task"
	pkghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/tourpackage"
	usershttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/users"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// Handlers aggregates all HTTP handlers for wiring.
type Handlers struct {
	Health    health.Handler
	Auth      authhttp.Handler
	Users     usershttp.Handler
	Audit     audithttp.Handler
	Customer  customerhttp.Handler
	Lead      leadhttp.Handler
	Booking   bookinghttp.Handler
	Payment   paymenthttp.Handler
	Task      taskhttp.Handler
	Dashboard dashboardhttp.Handler
	Document  documenthttp.Handler
	Package   pkghttp.Handler
	Ops       opshttp.Handler
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
			r.Post("/mfa/verify", h.Auth.VerifyMFA)
			r.Group(func(r chi.Router) {
				r.Use(middleware.Authenticate(tokens))
				r.Get("/me", h.Auth.Me)
				r.Post("/logout", h.Auth.Logout)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Authenticate(tokens))

			r.With(middleware.RequirePermission(platformauth.PermBranchesRead)).Get("/branches", h.Users.ListBranches)
			r.With(middleware.RequirePermission(platformauth.PermBranchesRead)).Get("/teams", h.Users.ListTeams)
			r.With(middleware.RequirePermission(platformauth.PermRolesRead)).Get("/permissions", h.Users.PermissionsMatrix)

			r.Route("/users", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermUsersRead)).Get("/", h.Users.List)
				r.With(middleware.RequirePermission(platformauth.PermUsersWrite)).Post("/", h.Users.Create)
				r.With(middleware.RequirePermission(platformauth.PermUsersRead)).Get("/{id}", h.Users.Get)
				r.With(middleware.RequirePermission(platformauth.PermUsersWrite)).Patch("/{id}", h.Users.Update)
			})

			r.With(middleware.RequirePermission(platformauth.PermAuditRead)).Get("/audit-events", h.Audit.List)

			r.Route("/customers", func(r chi.Router) {
				r.Get("/", h.Customer.Search)
				r.Post("/", h.Customer.Create)
				r.Get("/duplicates", h.Customer.CheckDuplicates)
				r.Get("/{id}", h.Customer.Get)
				r.Patch("/{id}", h.Customer.Update)
				r.Post("/{id}/merge", h.Customer.Merge)
				r.Get("/{id}/timeline", h.Customer.Timeline)
				r.Get("/{id}/companions", h.Customer.ListCompanions)
				r.Post("/{id}/companions", h.Customer.LinkCompanion)
				r.Delete("/{id}/companions/{companionId}", h.Customer.UnlinkCompanion)
			})

			r.Route("/leads", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermLeadsRead)).Get("/", h.Lead.List)
				r.With(middleware.RequirePermission(platformauth.PermLeadsRead)).Get("/analytics", h.Lead.Analytics)
				r.With(middleware.RequirePermission(platformauth.PermLeadsRead)).Get("/lost-reasons", h.Lead.LostReasons)
				r.With(middleware.RequirePermission(platformauth.PermLeadsWrite)).Post("/", h.Lead.Create)
				r.With(middleware.RequirePermission(platformauth.PermLeadsWrite)).Post("/assign", h.Lead.Assign)
				r.With(middleware.RequirePermission(platformauth.PermLeadsRead)).Get("/{id}", h.Lead.Get)
				r.With(middleware.RequirePermission(platformauth.PermLeadsWrite)).Post("/{id}/stage", h.Lead.ChangeStage)
				r.With(middleware.RequirePermission(platformauth.PermLeadsWrite)).Post("/{id}/assign", h.Lead.Assign)
				r.With(middleware.RequirePermission(platformauth.PermLeadsRead)).Get("/{id}/history", h.Lead.History)
				r.With(middleware.RequirePermission(platformauth.PermLeadsWrite)).Post("/{id}/convert", h.Lead.Convert)
				r.With(middleware.RequirePermission(platformauth.PermLeadsWrite)).Post("/{id}/no-follow-up", h.Lead.SetNoFollowUp)
			})

			r.Route("/bookings", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermBookingsRead)).Get("/", h.Booking.List)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Post("/", h.Booking.Create)
				r.With(middleware.RequirePermission(platformauth.PermBookingsRead)).Get("/{id}", h.Booking.Get)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Patch("/{id}", h.Booking.Update)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Post("/{id}/confirm", h.Booking.Confirm)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Post("/{id}/status", h.Booking.ChangeStatus)
				r.With(middleware.RequirePermission(platformauth.PermBookingsRead)).Get("/{id}/readiness", h.Booking.Readiness)
				r.With(middleware.RequirePermission(platformauth.PermBookingsRead)).Get("/{id}/participants", h.Booking.ListParticipants)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Post("/{id}/participants", h.Booking.AddParticipant)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Patch("/{id}/participants/{participantId}", h.Booking.UpdateParticipant)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Delete("/{id}/participants/{participantId}", h.Booking.DeleteParticipant)
				r.With(middleware.RequirePermission(platformauth.PermBookingsRead)).Get("/{id}/line-items", h.Booking.ListLineItems)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Put("/{id}/line-items", h.Booking.SetLineItems)
				r.With(middleware.RequirePermission(platformauth.PermBookingsRead)).Get("/{id}/checklist", h.Booking.ListChecklist)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Patch("/{id}/checklist/{itemId}", h.Booking.UpdateChecklist)
				r.With(middleware.RequirePermission(platformauth.PermPaymentsRead)).Get("/{id}/payments", h.Payment.ListByBooking)
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

			r.With(middleware.RequirePermission(platformauth.PermDashboardRead)).
				Get("/dashboard/kpis", h.Dashboard.KPIs)

			r.Route("/ops", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermOpsRead)).Get("/queue", h.Ops.QueueStats)
				r.With(middleware.RequirePermission(platformauth.PermOpsRead)).Get("/jobs/{id}", h.Ops.JobStatus)
			})

			r.Route("/documents", func(r chi.Router) {
				r.Get("/", h.Document.List)
				r.Post("/presign", h.Document.PresignUpload)
				r.Get("/{id}", h.Document.Get)
				r.Post("/{id}/complete", h.Document.Complete)
				r.Post("/{id}/download", h.Document.PresignDownload)
			})

			r.Route("/packages", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermPackagesRead)).Get("/", h.Package.ListPackages)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Post("/", h.Package.CreatePackage)
				r.With(middleware.RequirePermission(platformauth.PermPackagesRead)).Get("/{id}", h.Package.GetPackage)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Patch("/{id}", h.Package.UpdatePackage)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Post("/{id}/clone", h.Package.ClonePackage)
				r.With(middleware.RequirePermission(platformauth.PermPackagesRead)).Get("/{id}/tiers", h.Package.ListPackageTiers)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Put("/{id}/tiers", h.Package.SetPackageTiers)
				r.With(middleware.RequirePermission(platformauth.PermPackagesRead)).Get("/{id}/departures", h.Package.ListDepartures)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Post("/{id}/departures", h.Package.CreateDeparture)
			})

			r.Route("/departures", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermPackagesRead)).Get("/{id}", h.Package.GetDeparture)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Patch("/{id}", h.Package.UpdateDeparture)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Post("/{id}/clone", h.Package.CloneDeparture)
				r.With(middleware.RequirePermission(platformauth.PermPackagesRead)).Get("/{id}/tiers", h.Package.ListDepartureTiers)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Post("/{id}/close-sales", h.Package.CloseSales)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Post("/{id}/mark-full", h.Package.MarkFull)
				r.With(middleware.RequirePermission(platformauth.PermPackagesRead)).Get("/{id}/readiness", h.Package.Readiness)
				r.With(middleware.RequirePermission(platformauth.PermPackagesWrite)).Post("/{id}/recompute-capacity", h.Package.RecomputeCapacity)
			})
		})
	})

	return r
}
