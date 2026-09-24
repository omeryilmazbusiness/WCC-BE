package document_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestMarkUploaded(t *testing.T) {
	d := &document.Document{State: document.StatusPending}
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

func TestSubmitApproveReject(t *testing.T) {
	d := &document.Document{State: document.StatusPending, Kind: document.KindPassport}
	_ = d.MarkUploaded(100)
	if err := d.Submit(); err != nil {
		t.Fatal(err)
	}
	if d.Status() != document.StatusSubmitted {
		t.Fatal(d.Status())
	}
	actor := uuid.New()
	if err := d.Approve(actor, "ok"); err != nil {
		t.Fatal(err)
	}
	if d.Status() != document.StatusApproved || d.ReviewedBy == nil {
		t.Fatal(d.Status())
	}

	d2 := &document.Document{State: document.StatusUploaded}
	_ = d2.Submit()
	if err := d2.Reject(actor, ""); err == nil {
		t.Fatal("reject requires note")
	}
	if err := d2.Reject(actor, "blurry"); err != nil {
		t.Fatal(err)
	}
	if d2.Status() != document.StatusRejected {
		t.Fatal(d2.Status())
	}
}

func TestDocumentTransitions(t *testing.T) {
	if !document.CanTransition(document.StatusUploaded, document.StatusSubmitted) {
		t.Fatal("uploaded→submitted")
	}
	if document.CanTransition(document.StatusApproved, document.StatusSubmitted) {
		t.Fatal("approved→submitted forbidden")
	}
	d := &document.Document{State: document.StatusApproved, Version: 1, ID: uuid.New(), BranchID: uuid.New()}
	rep, err := d.NewReplacement(uuid.New(), "p.pdf", "application/pdf", "docs/x")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Version != 2 || rep.ReplacesID == nil || *rep.ReplacesID != d.ID {
		t.Fatalf("%#v", rep)
	}
	_ = time.Now()
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
