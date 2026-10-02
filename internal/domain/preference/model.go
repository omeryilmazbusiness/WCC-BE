// Package preference holds per-user UI preferences.
package preference

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// MaxNavFavorites caps the pinned sidebar shortcuts.
const MaxNavFavorites = 5

// navRoute is an app route path such as "/inbox" or "/finance/fx".
var navRoute = regexp.MustCompile(`^(/[a-z0-9-]{1,32}){1,4}$`)

// Preferences is one user's UI settings. A nil NavFavorites means the user
// never customised the sidebar and the client applies its role default.
type Preferences struct {
	UserID       uuid.UUID
	NavFavorites []string
	UpdatedAt    time.Time
}

type Repository interface {
	Get(ctx context.Context, userID uuid.UUID) (*Preferences, error)
	Upsert(ctx context.Context, p *Preferences) error
}

// NormalizeNavFavorites trims and de-duplicates the shortcuts, keeping order.
// The backend does not know frontend routes, so only the shape is validated;
// the client drops entries the viewer cannot open.
func NormalizeNavFavorites(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		route := strings.TrimSpace(raw)
		if !navRoute.MatchString(route) {
			return nil, shared.NewValidation("nav_favorites: invalid route " + route)
		}
		if _, dup := seen[route]; dup {
			continue
		}
		seen[route] = struct{}{}
		out = append(out, route)
	}
	if len(out) > MaxNavFavorites {
		return nil, shared.NewValidation("nav_favorites: at most 5 shortcuts")
	}
	return out, nil
}
