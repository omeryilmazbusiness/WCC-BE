package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type ctxKey int

const claimsKey ctxKey = 1

// 401 error codes that tell clients how to recover.
const (
	// CodeSessionRevoked: the session is revoked or expired, or the user is inactive; sign in again.
	CodeSessionRevoked = "session_revoked"
	// CodeTokenStale: role, branch, team or credentials changed; call /auth/refresh.
	CodeTokenStale = "token_stale"
)

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

// AccessTokenParser verifies an access token's signature and registered claims.
type AccessTokenParser interface {
	ParseAccess(token string) (*platformauth.Claims, error)
}

// SessionValidator checks that a verified token's session is live and its
// claims current. It returns authsec.ErrSessionRevoked or
// authsec.ErrTokenStale to reject; any other error means the check failed.
type SessionValidator interface {
	Validate(ctx context.Context, sid, userID uuid.UUID, version int) error
}

// sessionCheckRetryAfter is suggested to clients when the session store is down.
const sessionCheckRetryAfter = 5 * time.Second

// Authenticate validates the Bearer JWT, then its session, and injects
// claims. It fails closed: when the session cannot be checked the request
// gets 503, never access.
func Authenticate(tokens AccessTokenParser, sessions SessionValidator) func(http.Handler) http.Handler {
	if tokens == nil || sessions == nil {
		panic("middleware.Authenticate: token parser and session validator are required")
	}
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
			switch err := sessions.Validate(r.Context(), claims.SessionID, claims.UserID, claims.TokenVersion); {
			case err == nil:
			case errors.Is(err, authsec.ErrTokenStale):
				response.Error(w, shared.NewUnauthorizedCode(CodeTokenStale, "token claims are outdated; refresh the session"))
				return
			case errors.Is(err, authsec.ErrSessionRevoked):
				response.Error(w, shared.NewUnauthorizedCode(CodeSessionRevoked, "session has ended; sign in again"))
				return
			default:
				slog.ErrorContext(r.Context(), "session check failed", "error", err)
				response.Error(w, shared.NewUnavailable("session check unavailable", sessionCheckRetryAfter))
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			ctx = access.WithScope(ctx, platformauth.ScopeFor(claims))
			ctx = audit.WithActor(ctx, userActor(r, claims))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IdentifyBearer injects claims from a correctly signed Bearer token without
// checking its session and without ever rejecting the request. It injects no
// access scope, so it must only guard routes that end sessions (logout).
func IdentifyBearer(tokens AccessTokenParser) func(http.Handler) http.Handler {
	if tokens == nil {
		panic("middleware.IdentifyBearer: token parser is required")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if raw, ok := strings.CutPrefix(h, "Bearer "); ok {
				if claims, err := tokens.ParseAccess(raw); err == nil {
					r = r.WithContext(context.WithValue(r.Context(), claimsKey, claims))
				}
			}
			next.ServeHTTP(w, r)
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

// ScopeBranch restricts a query branch_id to the caller's branch unless the
// caller has global scope (GM/Admin), who may pick any branch (nil = all).
func ScopeBranch(claims *platformauth.Claims, requested *uuid.UUID) *uuid.UUID {
	s := platformauth.ScopeFor(claims)
	if s.Level == access.LevelGlobal {
		return requested
	}
	id := claims.BranchID
	return &id
}

// OwnsOrElevated reports whether the caller may act on a record in branchID
// owned by ownerID (and ownerTeamID when known).
func OwnsOrElevated(claims *platformauth.Claims, branchID uuid.UUID, ownerID, ownerTeamID *uuid.UUID) bool {
	return platformauth.ScopeFor(claims).CanAccess(access.Record{
		BranchID: branchID, OwnerID: ownerID, OwnerTeamID: ownerTeamID,
	})
}

// SecurityHeaders sets conservative headers for a JSON API.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Cross-Origin-Resource-Policy", "same-site")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			h.Set("Cache-Control", "no-store")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBody caps request bodies; larger uploads must use presigned URLs.
func MaxBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				response.Error(w, shared.NewValidation("request body too large"))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// TrustedRealIP sets r.RemoteAddr from X-Forwarded-For / X-Real-IP only when
// the direct peer is a trusted proxy, so clients cannot spoof their IP.
func TrustedRealIP(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := peerIP(r.RemoteAddr)
			if peer != nil && ipIn(peer, trusted) {
				if ip := clientFromForwarded(r, trusted); ip != "" {
					r.RemoteAddr = ip
				}
			} else if peer != nil {
				r.RemoteAddr = peer.String()
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ParseCIDRs parses a trusted proxy list; bare IPs become /32 or /128.
func ParseCIDRs(items []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(items))
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		if !strings.Contains(it, "/") {
			if strings.Contains(it, ":") {
				it += "/128"
			} else {
				it += "/32"
			}
		}
		_, n, err := net.ParseCIDR(it)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func clientFromForwarded(r *http.Request, trusted []*net.IPNet) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				continue
			}
			if !ipIn(ip, trusted) {
				return ip.String()
			}
		}
	}
	if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
		return ip.String()
	}
	return ""
}

func peerIP(remote string) net.IP {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	return net.ParseIP(host)
}

func ipIn(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the resolved client address (after TrustedRealIP).
func ClientIP(r *http.Request) string {
	if ip := peerIP(r.RemoteAddr); ip != nil {
		return ip.String()
	}
	return r.RemoteAddr
}
