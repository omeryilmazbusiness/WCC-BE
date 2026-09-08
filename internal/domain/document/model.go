package document

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Document stores metadata only; bytes live in S3-compatible storage.
type Document struct {
	ID          uuid.UUID
	BranchID    uuid.UUID
	RelatedType string
	RelatedID   uuid.UUID
	Kind        string // passport | visa | receipt | other
	FileName    string
	ContentType string
	SizeBytes   int64
	StorageKey  string
	UploadedBy  uuid.UUID
	CreatedAt   time.Time
}

type Repository interface {
	Create(ctx context.Context, d *Document) error
	FindByID(ctx context.Context, id uuid.UUID) (*Document, error)
	ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]Document, error)
}
