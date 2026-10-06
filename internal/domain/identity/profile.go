package identity

import (
	"context"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const (
	MinFullNameRunes = 2
	MaxFullNameRunes = 120
	MaxJobTitleRunes = 80
	MaxPhoneRunes    = 32
	MaxEmailBytes    = 254
	// E.164 numbers carry at most 15 digits; shorter than 7 is never a reachable number.
	minPhoneDigits = 7
	maxPhoneDigits = 15
	// MaxAvatarBytes caps an uploaded profile photo; clients downscale before upload.
	MaxAvatarBytes = 1 << 20
)

// ErrEmailTaken is returned by UpdateUser when another account already uses the email.
var ErrEmailTaken = shared.NewConflict("email already in use")

// avatarTypes are the raster formats a photo may use; SVG can carry script.
var avatarTypes = map[string]struct{}{"image/png": {}, "image/jpeg": {}, "image/webp": {}}

// Avatar is a user's profile photo.
type Avatar struct {
	ContentType string
	Data        []byte
	UpdatedAt   time.Time
}

// AvatarVersion identifies one upload; clients key cached photo URLs on it.
func AvatarVersion(updatedAt time.Time) string {
	return strconv.FormatInt(updatedAt.UnixMilli(), 36)
}

// AvatarStore keeps profile photos beside the users table.
type AvatarStore interface {
	Avatar(ctx context.Context, userID uuid.UUID) (*Avatar, error)
	SetAvatar(ctx context.Context, userID uuid.UUID, avatar Avatar) error
	DeleteAvatar(ctx context.Context, userID uuid.UUID) error
}

func fieldError(field, reason, message string) error {
	err := shared.NewValidation(message)
	err.Details = map[string]any{field: reason}
	return err
}

func hasControl(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

// collapseSpaces trims and folds inner whitespace runs into one space.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// NormalizeFullName returns the display name as stored.
func NormalizeFullName(raw string) (string, error) {
	if hasControl(raw) {
		return "", fieldError("full_name", "invalid", "full_name contains invalid characters")
	}
	name := collapseSpaces(raw)
	n := utf8.RuneCountInString(name)
	if n < MinFullNameRunes {
		return "", fieldError("full_name", "required", "full_name is required")
	}
	if n > MaxFullNameRunes {
		return "", fieldError("full_name", "too_long", "full_name is too long")
	}
	return name, nil
}

// NormalizeJobTitle returns the title as stored; empty clears it.
func NormalizeJobTitle(raw string) (string, error) {
	if hasControl(raw) {
		return "", fieldError("job_title", "invalid", "job_title contains invalid characters")
	}
	title := collapseSpaces(raw)
	if utf8.RuneCountInString(title) > MaxJobTitleRunes {
		return "", fieldError("job_title", "too_long", "job_title is too long")
	}
	return title, nil
}

// NormalizePhone accepts international numbers written with spaces, dashes,
// dots or parentheses and keeps that formatting; empty clears it.
func NormalizePhone(raw string) (string, error) {
	phone := collapseSpaces(raw)
	if phone == "" {
		return "", nil
	}
	digits := 0
	for i, r := range phone {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '+' && i == 0:
		case r == ' ' || r == '-' || r == '.' || r == '(' || r == ')':
		default:
			return "", fieldError("phone", "invalid", "phone may contain digits, spaces, + - . ( ) only")
		}
	}
	if digits < minPhoneDigits || digits > maxPhoneDigits || utf8.RuneCountInString(phone) > MaxPhoneRunes {
		return "", fieldError("phone", "invalid", "phone must have 7 to 15 digits")
	}
	return phone, nil
}

// NormalizeEmail returns a lower-cased bare address or a validation error.
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", fieldError("email", "required", "email is required")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || addr.Name != "" || len(email) > MaxEmailBytes || !strings.Contains(email[strings.LastIndexByte(email, '@'):], ".") {
		return "", fieldError("email", "invalid", "email is not a valid address")
	}
	return email, nil
}

// NewAvatar validates raw image bytes; the type comes from the content, never
// from what the client claims.
func NewAvatar(data []byte) (Avatar, error) {
	if len(data) == 0 {
		return Avatar{}, fieldError("avatar", "required", "avatar: empty file")
	}
	if len(data) > MaxAvatarBytes {
		return Avatar{}, fieldError("avatar", "too_large", "avatar: at most 1 MB")
	}
	ct := http.DetectContentType(data)
	if _, ok := avatarTypes[ct]; !ok {
		return Avatar{}, fieldError("avatar", "type", "avatar: PNG, JPEG or WebP image required")
	}
	return Avatar{ContentType: ct, Data: data}, nil
}
