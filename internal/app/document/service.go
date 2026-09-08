package document

import (
	"context"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// ObjectStore is the S3-compatible port (presigned uploads).
type ObjectStore interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (url string, err error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (url string, err error)
}

type PresignUploadInput struct {
	BranchID    uuid.UUID
	RelatedType string
	RelatedID   uuid.UUID
	Kind        string
	FileName    string
	ContentType string
	UploadedBy  uuid.UUID
}

type PresignUploadResult struct {
	DocumentID  uuid.UUID `json:"document_id"`
	UploadURL   string    `json:"upload_url"`
	StorageKey  string    `json:"storage_key"`
}

type Service struct {
	repo    domain.Repository
	storage ObjectStore
}

func NewService(repo domain.Repository, storage ObjectStore) *Service {
	return &Service{repo: repo, storage: storage}
}

func (s *Service) PresignUpload(ctx context.Context, in PresignUploadInput) (*PresignUploadResult, error) {
	if in.FileName == "" || in.ContentType == "" {
		return nil, shared.NewValidation("file_name and content_type are required")
	}
	id := uuid.New()
	key := "docs/" + in.BranchID.String() + "/" + id.String() + "/" + in.FileName
	url, err := s.storage.PresignPut(ctx, key, in.ContentType, 15*time.Minute)
	if err != nil {
		return nil, err
	}
	doc := &domain.Document{
		ID:          id,
		BranchID:    in.BranchID,
		RelatedType: in.RelatedType,
		RelatedID:   in.RelatedID,
		Kind:        in.Kind,
		FileName:    in.FileName,
		ContentType: in.ContentType,
		StorageKey:  key,
		UploadedBy:  in.UploadedBy,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.repo.Create(ctx, doc); err != nil {
		return nil, err
	}
	return &PresignUploadResult{DocumentID: id, UploadURL: url, StorageKey: key}, nil
}
