package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// BranchHeader selects the branch a company-wide caller (GM) acts on; the
// web app sends the branch from its /{company}/{branch}/... URL.
const BranchHeader = "X-Branch-ID"

// WorkspaceResolver loads the tenant of a home branch.
type WorkspaceResolver interface {
	Workspace(ctx context.Context, branchID uuid.UUID) (*company.Workspace, error)
}

type workspaceKey struct{}

// WorkspaceFrom returns the caller's tenant resolved by Tenancy.
func WorkspaceFrom(ctx context.Context) (*company.Workspace, bool) {
	w, ok := ctx.Value(workspaceKey{}).(*company.Workspace)
	return w, ok
}

// Tenancy runs after Authenticate: it confines the access scope to the
// caller's company and, for company-wide scopes, switches the active branch
// to the one named in BranchHeader. It fails closed.
func Tenancy(workspaces WorkspaceResolver) func(http.Handler) http.Handler {
	if workspaces == nil {
		panic("middleware.Tenancy: workspace resolver is required")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			claims, ok := ClaimsFrom(ctx)
			if !ok {
				response.Error(w, shared.NewUnauthorized("unauthenticated"))
				return
			}
			if claims.BranchID == uuid.Nil {
				if access.From(ctx).Level != access.LevelGlobal {
					response.Error(w, shared.NewForbidden("account is not linked to a company"))
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			ws, err := workspaces.Workspace(ctx, claims.BranchID)
			if err != nil {
				if errors.Is(err, shared.ErrNotFound) {
					response.Error(w, shared.NewForbidden("account is not linked to a company"))
					return
				}
				slog.ErrorContext(ctx, "workspace lookup failed", "error", err)
				response.Error(w, shared.NewUnavailable("workspace lookup unavailable", sessionCheckRetryAfter))
				return
			}
			s := access.From(ctx)
			if !ws.Company.IsActive && s.Level != access.LevelGlobal {
				response.Error(w, shared.NewForbidden("company is suspended"))
				return
			}
			s.CompanyID = ws.Company.ID
			if s.Level == access.LevelCompany {
				s.Branches = ws.BranchIDs()
				if raw := r.Header.Get(BranchHeader); raw != "" {
					id, err := uuid.Parse(raw)
					if _, inCompany := ws.Branch(id); err != nil || !inCompany {
						response.Error(w, shared.NewForbidden("branch outside your company"))
						return
					}
					s.BranchID = id
				}
			}
			ctx = access.WithScope(ctx, s)
			ctx = context.WithValue(ctx, workspaceKey{}, ws)
			if a, ok := audit.ActorFrom(ctx); ok {
				a.BranchID = s.BranchID
				ctx = audit.WithActor(ctx, a)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
