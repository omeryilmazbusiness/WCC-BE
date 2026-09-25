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
	inboxhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/inbox"
	importhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/importexport"
	leadhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	opshttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/ops"
	paymenthttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/payment"
	targethttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/revenuetarget"
	supplierhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/supplier"
	taskhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/task"
	pkghttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/tourpackage"
	usershttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/users"
	visahttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/visa"
	notificationhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/notification"
	webhookhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/webhook"
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
	Target    targethttp.Handler
	Task      taskhttp.Handler
	Dashboard dashboardhttp.Handler
	Document  documenthttp.Handler
	Visa      visahttp.Handler
	Supplier  supplierhttp.Handler
	Package   pkghttp.Handler
	Ops       opshttp.Handler
	Inbox     inboxhttp.Handler
	Import       importhttp.Handler
	Notification notificationhttp.Handler
	Webhook      webhookhttp.Handler
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
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Post("/{id}/readiness-override", h.Booking.OverrideReadiness)
				r.With(middleware.RequirePermission(platformauth.PermBookingsRead)).Get("/{id}/participants", h.Booking.ListParticipants)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Post("/{id}/participants", h.Booking.AddParticipant)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Patch("/{id}/participants/{participantId}", h.Booking.UpdateParticipant)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Delete("/{id}/participants/{participantId}", h.Booking.DeleteParticipant)
				r.With(middleware.RequirePermission(platformauth.PermBookingsRead)).Get("/{id}/line-items", h.Booking.ListLineItems)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Put("/{id}/line-items", h.Booking.SetLineItems)
				r.With(middleware.RequirePermission(platformauth.PermBookingsRead)).Get("/{id}/checklist", h.Booking.ListChecklist)
				r.With(middleware.RequirePermission(platformauth.PermBookingsWrite)).Patch("/{id}/checklist/{itemId}", h.Booking.UpdateChecklist)
				r.With(middleware.RequirePermission(platformauth.PermPaymentsRead)).Get("/{id}/payments", h.Payment.ListByBooking)
				r.With(middleware.RequirePermission(platformauth.PermPaymentsRead)).Get("/{id}/financial-summary", h.Payment.FinancialSummary)
				r.With(middleware.RequirePermission(platformauth.PermPaymentsRead)).Get("/{id}/payment-schedules", h.Payment.ListSchedules)
				r.With(middleware.RequirePermission(platformauth.PermPaymentsWrite)).Post("/{id}/payment-schedules", h.Payment.CreateSchedule)
			})

			r.Route("/tasks", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermTasksRead)).Get("/", h.Task.List)
				r.With(middleware.RequirePermission(platformauth.PermTasksRead)).Get("/mine", h.Task.ListMine)
				r.With(middleware.RequirePermission(platformauth.PermTasksWrite)).Post("/", h.Task.Create)
				r.With(middleware.RequirePermission(platformauth.PermTasksWrite)).Post("/assign", h.Task.BulkAssign)
				r.With(middleware.RequirePermission(platformauth.PermTasksWrite)).Post("/escalate-overdue", h.Task.EscalateOverdue)
				r.With(middleware.RequirePermission(platformauth.PermTasksRead)).Get("/{id}", h.Task.Get)
				r.With(middleware.RequirePermission(platformauth.PermTasksWrite)).Post("/{id}/status", h.Task.ChangeStatus)
				r.With(middleware.RequirePermission(platformauth.PermTasksWrite)).Post("/{id}/complete", h.Task.Complete)
				r.With(middleware.RequirePermission(platformauth.PermTasksWrite)).Post("/{id}/reschedule", h.Task.Reschedule)
				r.With(middleware.RequirePermission(platformauth.PermTasksWrite)).Post("/{id}/assign", h.Task.Assign)
			})

			r.With(middleware.RequirePermission(platformauth.PermPaymentsWrite)).Post("/payments", h.Payment.Record)
			r.With(middleware.RequirePermission(platformauth.PermPaymentsWrite)).Post("/payments/adjust", h.Payment.Adjust)
			r.With(middleware.RequirePermission(platformauth.PermPaymentsWrite)).Post("/payments/refunds", h.Payment.RequestRefund)
			r.With(middleware.RequirePermission(platformauth.PermPaymentsWrite)).Post("/payments/{id}/verify", h.Payment.Verify)
			r.With(middleware.RequirePermission(platformauth.PermPaymentsWrite)).Post("/payments/{id}/reverse", h.Payment.Reverse)
			r.With(middleware.RequirePermission(platformauth.PermPaymentsApprove)).Post("/payments/{id}/approve", h.Payment.ApproveRefund)
			r.With(middleware.RequirePermission(platformauth.PermPaymentsApprove)).Post("/payments/{id}/reject", h.Payment.RejectRefund)
			r.With(middleware.RequirePermission(platformauth.PermPaymentsWrite)).Post("/payment-schedules/{id}/cancel", h.Payment.CancelSchedule)

			r.Route("/finance", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermPaymentsRead)).Get("/queues/{kind}", h.Payment.Queue)
				r.With(middleware.RequirePermission(platformauth.PermPaymentsRead)).Get("/export", h.Payment.Export)
				r.With(middleware.RequirePermission(platformauth.PermPaymentsWrite)).Post("/reminders/process", h.Payment.ProcessReminders)
				r.With(middleware.RequirePermission(platformauth.PermPaymentsApprove)).Put("/reporting-currency", h.Payment.SetReportingCurrency)
			})

			r.Route("/targets", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermTargetsRead)).Get("/", h.Target.List)
				r.With(middleware.RequirePermission(platformauth.PermTargetsWrite)).Post("/", h.Target.Create)
				r.With(middleware.RequirePermission(platformauth.PermTargetsWrite)).Post("/recompute", h.Target.RecomputeBranch)
				r.With(middleware.RequirePermission(platformauth.PermTargetsRead)).Get("/{id}", h.Target.Get)
				r.With(middleware.RequirePermission(platformauth.PermTargetsWrite)).Patch("/{id}", h.Target.Patch)
				r.With(middleware.RequirePermission(platformauth.PermTargetsRead)).Get("/{id}/weights", h.Target.ListWeights)
				r.With(middleware.RequirePermission(platformauth.PermTargetsWrite)).Put("/{id}/weights", h.Target.SetWeights)
				r.With(middleware.RequirePermission(platformauth.PermTargetsWrite)).Put("/{id}/shares", h.Target.SetShares)
				r.With(middleware.RequirePermission(platformauth.PermTargetsRead)).Get("/{id}/progress", h.Target.Progress)
				r.With(middleware.RequirePermission(platformauth.PermTargetsRead)).Get("/{id}/contributions", h.Target.Contributions)
				r.With(middleware.RequirePermission(platformauth.PermTargetsRead)).Get("/{id}/series", h.Target.Series)
				r.With(middleware.RequirePermission(platformauth.PermTargetsRead)).Get("/{id}/sources", h.Target.Sources)
				r.With(middleware.RequirePermission(platformauth.PermTargetsRead)).Get("/{id}/revisions", h.Target.Revisions)
				r.With(middleware.RequirePermission(platformauth.PermTargetsWrite)).Post("/{id}/recompute", h.Target.Recompute)
			})

			r.With(middleware.RequirePermission(platformauth.PermDashboardRead)).
				Get("/dashboard/kpis", h.Dashboard.KPIs)
			r.With(middleware.RequirePermission(platformauth.PermDashboardRead)).
				Get("/dashboard/team", h.Dashboard.Team)
			r.With(middleware.RequirePermission(platformauth.PermDashboardRead)).
				Get("/dashboard/attention", h.Dashboard.Attention)
			r.With(middleware.RequirePermission(platformauth.PermTasksRead)).
				Get("/dashboard/my-work", h.Dashboard.MyWork)
			r.Get("/dashboard/my-target", h.Dashboard.MyTarget)

			r.Route("/ops", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermOpsRead)).Get("/queue", h.Ops.QueueStats)
				r.With(middleware.RequirePermission(platformauth.PermOpsRead)).Get("/jobs/{id}", h.Ops.JobStatus)
			})

			r.Route("/documents", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermDocsRead)).Get("/", h.Document.List)
				r.With(middleware.RequirePermission(platformauth.PermDocsWrite)).Post("/presign", h.Document.PresignUpload)
				r.With(middleware.RequirePermission(platformauth.PermDocsRead)).Get("/policies", h.Document.ListPolicies)
				r.With(middleware.RequirePermission(platformauth.PermDocsWrite)).Put("/policies", h.Document.UpsertPolicy)
				r.With(middleware.RequirePermission(platformauth.PermDocsRead)).Get("/checklist", h.Document.Checklist)
				r.With(middleware.RequirePermission(platformauth.PermDocsRead)).Get("/missing-docs", h.Document.MissingDocs)
				r.With(middleware.RequirePermission(platformauth.PermDocsWrite)).Post("/reminders/expiry", h.Document.ProcessExpiryReminders)
				r.With(middleware.RequirePermission(platformauth.PermDocsRead)).Get("/{id}", h.Document.Get)
				r.With(middleware.RequirePermission(platformauth.PermDocsWrite)).Patch("/{id}", h.Document.Classify)
				r.With(middleware.RequirePermission(platformauth.PermDocsWrite)).Post("/{id}/complete", h.Document.Complete)
				r.With(middleware.RequirePermission(platformauth.PermDocsRead)).Post("/{id}/download", h.Document.PresignDownload)
				r.With(middleware.RequirePermission(platformauth.PermDocsWrite)).Post("/{id}/submit", h.Document.Submit)
				r.With(middleware.RequirePermission(platformauth.PermDocsReview)).Post("/{id}/approve", h.Document.Approve)
				r.With(middleware.RequirePermission(platformauth.PermDocsReview)).Post("/{id}/reject", h.Document.Reject)
				r.With(middleware.RequirePermission(platformauth.PermDocsWrite)).Post("/{id}/replace", h.Document.Replace)
			})

			r.Route("/visa-cases", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermVisaRead)).Get("/", h.Visa.List)
				r.With(middleware.RequirePermission(platformauth.PermVisaWrite)).Post("/", h.Visa.Create)
				r.With(middleware.RequirePermission(platformauth.PermVisaRead)).Get("/{id}", h.Visa.Get)
				r.With(middleware.RequirePermission(platformauth.PermVisaWrite)).Post("/{id}/transition", h.Visa.Transition)
			})

			r.Route("/suppliers", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermSuppliersRead)).Get("/", h.Supplier.List)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersWrite)).Post("/", h.Supplier.Create)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersRead)).Get("/unconfirmed", h.Supplier.ListUnconfirmed)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersRead)).Get("/oversold", h.Supplier.ListOversold)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersWrite)).Post("/reminders/unconfirmed", h.Supplier.ProcessUnconfirmedReminders)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersRead)).Get("/{id}", h.Supplier.Get)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersWrite)).Patch("/{id}", h.Supplier.Update)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersRead)).Get("/{id}/links", h.Supplier.ListLinks)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersWrite)).Post("/{id}/links", h.Supplier.Link)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersWrite)).Delete("/{id}/links/{linkId}", h.Supplier.Unlink)
				r.With(middleware.RequirePermission(platformauth.PermSuppliersWrite)).Post("/links/{linkId}/confirm", h.Supplier.ConfirmLink)
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

			r.Route("/inbox", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermInboxRead)).Get("/conversations", h.Inbox.List)
				r.With(middleware.RequirePermission(platformauth.PermInboxRead)).Get("/conversations/{id}", h.Inbox.Get)
				r.With(middleware.RequirePermission(platformauth.PermInboxRead)).Get("/conversations/{id}/messages", h.Inbox.Messages)
				r.With(middleware.RequirePermission(platformauth.PermInboxWrite)).Post("/conversations/{id}/assign", h.Inbox.Assign)
				r.With(middleware.RequirePermission(platformauth.PermInboxWrite)).Post("/conversations/{id}/reply", h.Inbox.Reply)
				r.With(middleware.RequirePermission(platformauth.PermInboxWrite)).Post("/conversations/{id}/status", h.Inbox.SetStatus)
				r.With(middleware.RequirePermission(platformauth.PermInboxWrite)).Post("/sla/check", h.Inbox.CheckSLA)
			})
			r.With(middleware.RequirePermission(platformauth.PermIntegrationsRead)).
				Get("/integrations/health", h.Inbox.Health)
			r.With(middleware.RequirePermission(platformauth.PermIntegrationsWrite)).
				Post("/integrations/accounts/{provider}/connect", h.Inbox.Connect)
			r.With(middleware.RequirePermission(platformauth.PermIntegrationsWrite)).
				Post("/integrations/accounts/{provider}/disconnect", h.Inbox.Disconnect)

			r.Route("/imports", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermImportsWrite)).Post("/", h.Import.Upload)
				r.With(middleware.RequirePermission(platformauth.PermImportsRead)).Get("/", h.Import.List)
				r.With(middleware.RequirePermission(platformauth.PermImportsRead)).Get("/templates", h.Import.ListTemplates)
				r.With(middleware.RequirePermission(platformauth.PermImportsWrite)).Post("/templates", h.Import.CreateTemplate)
				r.With(middleware.RequirePermission(platformauth.PermImportsWrite)).Delete("/templates/{id}", h.Import.DeleteTemplate)
				r.With(middleware.RequirePermission(platformauth.PermImportsRead)).Get("/{id}", h.Import.Get)
				r.With(middleware.RequirePermission(platformauth.PermImportsWrite)).Put("/{id}/mapping", h.Import.SetMapping)
				r.With(middleware.RequirePermission(platformauth.PermImportsWrite)).Post("/{id}/validate", h.Import.Validate)
				r.With(middleware.RequirePermission(platformauth.PermImportsWrite)).Post("/{id}/confirm", h.Import.Confirm)
				r.With(middleware.RequirePermission(platformauth.PermImportsRead)).Get("/{id}/errors", h.Import.Errors)
			})
			r.With(middleware.RequirePermission(platformauth.PermImportsWrite)).Post("/exports", h.Import.Export)
			r.With(middleware.RequirePermission(platformauth.PermImportsRead)).Get("/exports/schemas", h.Import.Schemas)

			r.Route("/notifications", func(r chi.Router) {
				r.With(middleware.RequirePermission(platformauth.PermNotificationsRead)).Get("/", h.Notification.List)
				r.With(middleware.RequirePermission(platformauth.PermNotificationsRead)).Get("/unread-count", h.Notification.UnreadCount)
				r.With(middleware.RequirePermission(platformauth.PermNotificationsRead)).Get("/preferences", h.Notification.GetPreferences)
				r.With(middleware.RequirePermission(platformauth.PermNotificationsWrite)).Put("/preferences", h.Notification.UpdatePreferences)
				r.With(middleware.RequirePermission(platformauth.PermNotificationsRead)).Get("/rules", h.Notification.Rules)
				r.With(middleware.RequirePermission(platformauth.PermNotificationsWrite)).Post("/ack-all", h.Notification.AcknowledgeAll)
				r.With(middleware.RequirePermission(platformauth.PermNotificationsManage)).Post("/escalate", h.Notification.ProcessEscalations)
				r.With(middleware.RequirePermission(platformauth.PermNotificationsManage)).Post("/emit", h.Notification.Emit)
				r.With(middleware.RequirePermission(platformauth.PermNotificationsWrite)).Post("/{id}/acknowledge", h.Notification.Acknowledge)
				r.With(middleware.RequirePermission(platformauth.PermNotificationsWrite)).Post("/{id}/resolve", h.Notification.Resolve)
			})
		})

		// Provider webhooks — signature verification added when live credentials land.
		r.Post("/webhooks/{provider}", h.Webhook.Ingest)
	})

	return r
}
