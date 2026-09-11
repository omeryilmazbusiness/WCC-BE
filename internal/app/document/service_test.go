package document_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/document"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type memRepo struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*domain.Document
}

func newMemRepo() *memRepo {
	return &memRepo{byID: map[uuid.UUID]*domain.Document{}}
}

func (r *memRepo) Create(_ context.Context, d *domain.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *d
	r.byID[d.ID] = &cp
	return nil
}

func (r *memRepo) Update(_ context.Context, d *domain.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[d.ID]; !ok {
		return fmt.Errorf("missing")
	}
	cp := *d
	r.byID[d.ID] = &cp
	return nil
}

func (r *memRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("missing")
	}
	cp := *d
	return &cp, nil
}

func (r *memRepo) ListByRelated(_ context.Context, relatedType string, relatedID uuid.UUID) ([]domain.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Document
	for _, d := range r.byID {
		if d.RelatedType == relatedType && d.RelatedID == relatedID {
			out = append(out, *d)
		}
	}
	return out, nil
}

type memStore struct{}

func (memStore) PresignPut(_ context.Context, key, contentType string, ttl time.Duration) (string, error) {
	return fmt.Sprintf("put://%s?ct=%s&ttl=%d", key, contentType, int(ttl.Seconds())), nil
}
func (memStore) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	return fmt.Sprintf("get://%s?ttl=%d", key, int(ttl.Seconds())), nil
}

func TestPresignUploadAndComplete(t *testing.T) {
	svc := appsvc.NewService(newMemRepo(), memStore{})
	branch := uuid.New()
	related := uuid.New()
	actor := uuid.New()

	res, err := svc.PresignUpload(context.Background(), appsvc.PresignUploadInput{
		BranchID: branch, RelatedType: domain.RelatedBooking, RelatedID: related,
		Kind: domain.KindPassport, FileName: "pass.pdf", ContentType: "application/pdf", UploadedBy: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != domain.StatusPending || res.UploadURL == "" {
		t.Fatalf("%#v", res)
	}

	dto, err := svc.CompleteUpload(context.Background(), appsvc.CompleteUploadInput{
		DocumentID: res.DocumentID, SizeBytes: 2048, ActorID: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Status != domain.StatusUploaded || dto.SizeBytes != 2048 {
		t.Fatalf("%#v", dto)
	}

	dl, err := svc.PresignDownload(context.Background(), res.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if dl.DownloadURL == "" {
		t.Fatal("missing download url")
	}

	list, err := svc.ListByRelated(context.Background(), domain.RelatedBooking, related)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

func TestPresignUploadValidation(t *testing.T) {
	svc := appsvc.NewService(newMemRepo(), memStore{})
	_, err := svc.PresignUpload(context.Background(), appsvc.PresignUploadInput{
		BranchID: uuid.New(), RelatedType: "invoice", RelatedID: uuid.New(),
		FileName: "a.pdf", ContentType: "application/pdf", UploadedBy: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected related_type validation")
	}
	if _, ok := err.(*shared.AppError); !ok {
		t.Fatalf("want AppError, got %T", err)
	}
}

func TestDownloadRequiresComplete(t *testing.T) {
	svc := appsvc.NewService(newMemRepo(), memStore{})
	res, err := svc.PresignUpload(context.Background(), appsvc.PresignUploadInput{
		BranchID: uuid.New(), RelatedType: domain.RelatedCustomer, RelatedID: uuid.New(),
		Kind: domain.KindOther, FileName: "x.png", ContentType: "image/png", UploadedBy: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PresignDownload(context.Background(), res.DocumentID)
	if err == nil {
		t.Fatal("expected invalid state")
	}
}
