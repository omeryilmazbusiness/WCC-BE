package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/storage"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
)

func TestMinIOPresignPutGet(t *testing.T) {
	m := storage.NewMinIO(config.StorageConfig{
		Bucket:    "wodi-crm",
		PublicURL: "http://localhost:9000",
	})
	put, err := m.PresignPut(context.Background(), "docs/a/b.pdf", "application/pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(put, "upload=1") || !strings.Contains(put, "wodi-crm/docs/a/b.pdf") {
		t.Fatalf("put url=%s", put)
	}
	get, err := m.PresignGet(context.Background(), "docs/a/b.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(get, "expires=") {
		t.Fatalf("get url=%s", get)
	}
}

func TestMinIORejectsBadKey(t *testing.T) {
	m := storage.NewMinIO(config.StorageConfig{Bucket: "b", PublicURL: "http://x"})
	if _, err := m.PresignPut(context.Background(), "../evil", "text/plain", time.Minute); err == nil {
		t.Fatal("expected error")
	}
}
