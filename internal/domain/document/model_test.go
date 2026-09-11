package document_test

import (
	"errors"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestMarkUploaded(t *testing.T) {
	d := &document.Document{}
	if d.Status() != document.StatusPending {
		t.Fatal("expected pending")
	}
	if err := d.MarkUploaded(0); err == nil {
		t.Fatal("size 0 invalid")
	}
	if err := d.MarkUploaded(1024); err != nil {
		t.Fatal(err)
	}
	if d.Status() != document.StatusUploaded || d.SizeBytes != 1024 {
		t.Fatalf("%#v", d)
	}
	if err := d.MarkUploaded(10); err == nil {
		t.Fatal("double complete must fail")
	} else {
		var app *shared.AppError
		if !errors.As(err, &app) || !errors.Is(app.Err, shared.ErrInvalidState) {
			t.Fatalf("want invalid state, got %v", err)
		}
	}
}

func TestValidKindAndRelated(t *testing.T) {
	if !document.ValidKind("passport") || document.ValidKind("exe") {
		t.Fatal("kind")
	}
	if !document.ValidRelatedType("booking") || document.ValidRelatedType("invoice") {
		t.Fatal("related")
	}
}

func TestSanitizeFileName(t *testing.T) {
	name, err := document.SanitizeFileName(" ../../etc/passwd ")
	if err != nil {
		t.Fatal(err)
	}
	if name != "passwd" {
		t.Fatalf("got %q", name)
	}
	if _, err := document.SanitizeFileName("   "); err == nil {
		t.Fatal("empty")
	}
}
