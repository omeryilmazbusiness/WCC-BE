package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/storage"
)

func TestLocalPresignPutGet(t *testing.T) {
	m := storage.New()
	put, err := m.PresignPut(context.Background(), "docs/a/b.pdf", "application/pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(put, "upload=1") || !strings.Contains(put, "/files/docs/a/b.pdf") {
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

func TestLocalRejectsBadKey(t *testing.T) {
	m := storage.New()
	if _, err := m.PresignPut(context.Background(), "../evil", "text/plain", time.Minute); err == nil {
		t.Fatal("expected error")
	}
}
