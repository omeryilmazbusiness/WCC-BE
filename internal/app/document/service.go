package document

import (
	"context"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// ObjectStore is the S3-compatible port (presigned uploads/downloads).
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
	ExpiresInSec int      `json:"expires_in_sec"`
	Status      string    `json:"status"`
}

type CompleteUploadInput struct {
	DocumentID uuid.UUID
	SizeBytes  int64
	ActorID    uuid.UUID
}

type DownloadResult struct {
	DocumentID   uuid.UUID `json:"document_id"`
	DownloadURL  string    `json:"download_url"`
	ExpiresInSec int       `json:"expires_in_sec"`
	Status       string    `json:"status"`
}

type DocumentDTO struct {
	ID          uuid.UUID `json:"id"`
	BranchID    uuid.UUID `json:"branch_id"`
	RelatedType string    `json:"related_type"`
	RelatedID   uuid.UUID `json:"related_id"`
	Kind        string    `json:"kind"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	StorageKey  string    `json:"storage_key"`
	Status      string    `json:"status"`
	UploadedBy  uuid.UUID `json:"uploaded_by"`
	CreatedAt   time.Time `json:"created_at"`
}

func toDTO(d *domain.Document) DocumentDTO {
	return DocumentDTO{
		ID: d.ID, BranchID: d.BranchID, RelatedType: d.RelatedType, RelatedID: d.RelatedID,
		Kind: d.Kind, FileName: d.FileName, ContentType: d.ContentType, SizeBytes: d.SizeBytes,
		StorageKey: d.StorageKey, Status: d.Status(), UploadedBy: d.UploadedBy, CreatedAt: d.CreatedAt,
	}
}

type Service struct {
	repo    domain.Repository
	storage ObjectStore
	presignTTL time.Duration
}

func NewService(repo domain.Repository, storage ObjectStore) *Service {
	return &Service{repo: repo, storage: storage, presignTTL: 15 * time.Minute}
}

func (s *Service) PresignUpload(ctx context.Context, in PresignUploadInput) (*PresignUploadResult, error) {
	fileName, err := domain.SanitizeFileName(in.FileName)
	if err != nil {
		return nil, err
	}
	if in.ContentType == "" {
		return nil, shared.NewValidation("content_type is required")
	}
	if in.RelatedID == uuid.Nil {
		return nil, shared.NewValidation("related_id is required")
	}
	if !domain.ValidRelatedType(in.RelatedType) {
		return nil, shared.NewValidation("invalid related_type")
	}
	kind := in.Kind
	if kind == "" {
		kind = domain.KindOther
	}
	if !domain.ValidKind(kind) {
		return nil, shared.NewValidation("invalid kind")
	}
	if in.BranchID == uuid.Nil || in.UploadedBy == uuid.Nil {
		return nil, shared.NewValidation("branch_id and uploaded_by are required")
	}

	id := uuid.New()
	key := "docs/" + in.BranchID.String() + "/" + id.String() + "/" + fileName
	url, err := s.storage.PresignPut(ctx, key, in.ContentType, s.presignTTL)
	if err != nil {
		return nil, err
	}
	doc := &domain.Document{
		ID:          id,
		BranchID:    in.BranchID,
		RelatedType: in.RelatedType,
		RelatedID:   in.RelatedID,
		Kind:        kind,
		FileName:    fileName,
		ContentType: in.ContentType,
		StorageKey:  key,
		UploadedBy:  in.UploadedBy,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.repo.Create(ctx, doc); err != nil {
		return nil, err
	}
	return &PresignUploadResult{
		DocumentID:   id,
		UploadURL:    url,
		StorageKey:   key,
		ExpiresInSec: int(s.presignTTL.Seconds()),
		Status:       doc.Status(),
	}, nil
}

func (s *Service) CompleteUpload(ctx context.Context, in CompleteUploadInput) (*DocumentDTO, error) {
	doc, err := s.repo.FindByID(ctx, in.DocumentID)
	if err != nil {
		return nil, shared.NewNotFound("document")
	}
	if err := doc.MarkUploaded(in.SizeBytes); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, doc); err != nil {
		return nil, err
	}
	dto := toDTO(doc)
	return &dto, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*DocumentDTO, error) {
	doc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("document")
	}
	dto := toDTO(doc)
	return &dto, nil
}

func (s *Service) ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]DocumentDTO, error) {
	if !domain.ValidRelatedType(relatedType) {
		return nil, shared.NewValidation("invalid related_type")
	}
	if relatedID == uuid.Nil {
		return nil, shared.NewValidation("related_id is required")
	}
	items, err := s.repo.ListByRelated(ctx, relatedType, relatedID)
	if err != nil {
		return nil, err
	}
	out := make([]DocumentDTO, 0, len(items))
	for i := range items {
		out = append(out, toDTO(&items[i]))
	}
	return out, nil
}

func (s *Service) PresignDownload(ctx context.Context, id uuid.UUID) (*DownloadResult, error) {
	doc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("document")
	}
	if doc.Status() != domain.StatusUploaded {
		return nil, shared.NewInvalidState("document upload not completed")
	}
	url, err := s.storage.PresignGet(ctx, doc.StorageKey, s.presignTTL)
	if err != nil {
		return nil, err
	}
	return &DownloadResult{
		DocumentID:   doc.ID,
		DownloadURL:  url,
		ExpiresInSec: int(s.presignTTL.Seconds()),
		Status:       doc.Status(),
	}, nil
}
