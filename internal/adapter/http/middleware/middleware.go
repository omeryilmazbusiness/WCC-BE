package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type ctxKey int

const claimsKey ctxKey = 1

func ClaimsFrom(ctx context.Context) (*platformauth.Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*platformauth.Claims)
	return c, ok
}

func RequestID(next http.Handler) http.Handler {
	return middleware.RequestID(next)
}

func RealIP(next http.Handler) http.Handler {
	return middleware.RealIP(next)
}

func Recoverer(next http.Handler) http.Handler {
	return middleware.Recoverer(next)
}

func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return middleware.Timeout(d)
}

// Authenticate validates Bearer JWT and injects claims.
func Authenticate(tokens *platformauth.TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" || !strings.HasPrefix(h, "Bearer ") {
				response.Error(w, shared.NewUnauthorized("missing bearer token"))
				return
			}
			raw := strings.TrimPrefix(h, "Bearer ")
			claims, err := tokens.ParseAccess(raw)
			if err != nil {
				response.Error(w, shared.NewUnauthorized("invalid token"))
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRoles enforces RBAC at the API boundary.
func RequireRoles(roles ...platformauth.Role) func(http.Handler) http.Handler {
	allowed := make(map[platformauth.Role]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFrom(r.Context())
			if !ok {
				response.Error(w, shared.NewUnauthorized("unauthenticated"))
				return
			}
			if _, ok := allowed[claims.Role]; !ok {
				response.Error(w, shared.NewForbidden("insufficient role"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission enforces fine-grained permission from the role matrix.
func RequirePermission(p platformauth.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFrom(r.Context())
			if !ok {
				response.Error(w, shared.NewUnauthorized("unauthenticated"))
				return
			}
			if !platformauth.HasPermission(claims.Role, p) {
				response.Error(w, shared.NewForbidden("missing permission: "+string(p)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ScopeBranch optionally restricts a query branch_id to the caller's branch
// unless role is GM or Admin.
func ScopeBranch(claims *platformauth.Claims, requested *uuid.UUID) *uuid.UUID {
	if claims.Role == platformauth.RoleGM || claims.Role == platformauth.RoleAdmin {
		return requested
	}
	id := claims.BranchID
	return &id
}

// OwnsOrElevated allows record owners, or manager+/gm/admin, to proceed.
func OwnsOrElevated(claims *platformauth.Claims, ownerID uuid.UUID) bool {
	if claims.UserID == ownerID {
		return true
	}
	switch claims.Role {
	case platformauth.RoleGM, platformauth.RoleAdmin, platformauth.RoleManager:
		return true
	default:
		return false
	}
}
