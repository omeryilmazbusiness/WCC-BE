package company

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// MaxLogoBytes caps an uploaded company logo.
const MaxLogoBytes = 512 << 10

// logoTypes are the raster formats a logo may use. SVG is excluded on purpose:
// it can carry script and is served from a public, unauthenticated URL.
var logoTypes = map[string]struct{}{"image/png": {}, "image/jpeg": {}, "image/webp": {}}

// LogoVersion identifies one upload; clients key cached logo URLs on it.
func LogoVersion(updatedAt time.Time) string {
	return strconv.FormatInt(updatedAt.UnixMilli(), 36)
}

// Logo is a company's sign-in page icon.
type Logo struct {
	ContentType string
	Data        []byte
	UpdatedAt   time.Time
}

// NewLogo validates raw image bytes; the type comes from the content, never
// from what the client claims.
func NewLogo(data []byte) (Logo, error) {
	if len(data) == 0 {
		return Logo{}, shared.NewValidation("logo: empty file")
	}
	if len(data) > MaxLogoBytes {
		return Logo{}, shared.NewValidation("logo: at most 512 KB")
	}
	ct := http.DetectContentType(data)
	if _, ok := logoTypes[ct]; !ok {
		return Logo{}, shared.NewValidation("logo: PNG, JPEG or WebP image required")
	}
	return Logo{ContentType: ct, Data: data}, nil
}

// Branding is what an unauthenticated visitor of a company's sign-in page may
// see: its URL slug, display names and whether (and since when) it has a logo.
type Branding struct {
	Slug          string
	NameEN        string
	NameAR        string
	LogoUpdatedAt *time.Time
}

// BrandingRepository serves the public face of a company. Lookups by slug are
// deliberately unscoped (they run before sign-in) and see active companies only.
type BrandingRepository interface {
	BrandingBySlug(ctx context.Context, slug string) (*Branding, error)
	LogoBySlug(ctx context.Context, slug string) (*Logo, error)
	// SetLogo and DeleteLogo are tenant-scoped like every other company write.
	SetLogo(ctx context.Context, companyID uuid.UUID, logo Logo) error
	DeleteLogo(ctx context.Context, companyID uuid.UUID) error
	// BrandingOf is the tenant-scoped read used after a write.
	BrandingOf(ctx context.Context, companyID uuid.UUID) (*Branding, error)
}

// Directory answers which company a branch belongs to, for sign-in checks.
type Directory interface {
	CompanySlugOfBranch(ctx context.Context, branchID uuid.UUID) (string, error)
}
