package document

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Upload lifecycle derived from size_bytes (metadata-only store; no blob in DB).
const (
	StatusPending  = "pending"  // presigned, bytes not confirmed
	StatusUploaded = "uploaded" // complete callback recorded size
)

const (
	KindPassport = "passport"
	KindVisa     = "visa"
	KindReceipt  = "receipt"
	KindPhoto    = "photo"
	KindOther    = "other"

	RelatedCustomer = "customer"
	RelatedBooking  = "booking"
	RelatedLead     = "lead"
	RelatedParticipant = "participant"
)

// Document stores metadata only; bytes live in S3-compatible storage.
type Document struct {
	ID          uuid.UUID
	BranchID    uuid.UUID
	RelatedType string
	RelatedID   uuid.UUID
	Kind        string
	FileName    string
	ContentType string
	SizeBytes   int64
	StorageKey  string
	UploadedBy  uuid.UUID
	CreatedAt   time.Time
}

func (d *Document) Status() string {
	if d.SizeBytes > 0 {
		return StatusUploaded
	}
	return StatusPending
}

func (d *Document) MarkUploaded(sizeBytes int64) error {
	if sizeBytes <= 0 {
		return shared.NewValidation("size_bytes must be > 0")
	}
	if d.SizeBytes > 0 {
		return shared.NewInvalidState("document already marked uploaded")
	}
	d.SizeBytes = sizeBytes
	return nil
}

func ValidKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindPassport, KindVisa, KindReceipt, KindPhoto, KindOther:
		return true
	default:
		return false
	}
}

func ValidRelatedType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case RelatedCustomer, RelatedBooking, RelatedLead, RelatedParticipant:
		return true
	default:
		return false
	}
}

// SanitizeFileName keeps a single path segment safe for object keys.
func SanitizeFileName(name string) (string, error) {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.ReplaceAll(base, "..", "")
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "", shared.NewValidation("file_name is required")
	}
	if len(base) > 200 {
		return "", shared.NewValidation("file_name too long")
	}
	return base, nil
}

type Repository interface {
	Create(ctx context.Context, d *Document) error
	Update(ctx context.Context, d *Document) error
	FindByID(ctx context.Context, id uuid.UUID) (*Document, error)
	ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]Document, error)
}
