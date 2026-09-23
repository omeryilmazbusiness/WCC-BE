package importexport_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/importexport"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/importexport"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

func TestCustomerUpsertFlow(t *testing.T) {
	repo := appsvc.NewMemRepo()
	customers := appsvc.NewMemCustomers()
	svc := appsvc.NewService(repo, customers, tx.Nop{})

	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	actor := uuid.MustParse("22222222-2222-2222-2222-222222222201")

	csv := []byte("Full Name,Mobile,Email\nAlice,+971501111111,a@ex.com\nBob,+971502222222,b@ex.com\n")
	job, err := svc.Upload(context.Background(), appsvc.UploadInput{
		BranchID: branch, ActorID: actor, EntityType: domain.EntityCustomers,
		Mode: domain.ModeUpsert, FileName: "c.csv", ContentType: "text/csv", FileBytes: csv,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.StatusUploaded {
		t.Fatalf("status: %s", job.Status)
	}
	if job.Mapping["phone"] == "" || job.Mapping["full_name"] == "" {
		t.Fatalf("auto mapping: %#v", job.Mapping)
	}

	job, err = svc.SetMapping(context.Background(), appsvc.MappingInput{
		JobID: job.ID, BranchID: branch, Mapping: job.Mapping, Mode: domain.ModeUpsert,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = svc.Validate(context.Background(), job.ID, branch)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.StatusValidated {
		t.Fatalf("validated status: %s", job.Status)
	}

	job, err = svc.Confirm(context.Background(), job.ID, branch)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.StatusCompleted {
		t.Fatalf("completed status: %s failed=%d err=%s", job.Status, job.FailedCount, job.ErrorMessage)
	}
	if job.SuccessCount != 2 {
		t.Fatalf("success: %d", job.SuccessCount)
	}

	// Upsert again should update existing (still success 2)
	csv2 := []byte("Full Name,Mobile,Email\nAlice Updated,+971501111111,a2@ex.com\n")
	job2, err := svc.Upload(context.Background(), appsvc.UploadInput{
		BranchID: branch, ActorID: actor, EntityType: domain.EntityCustomers,
		Mode: domain.ModeUpsert, FileName: "c2.csv", ContentType: "text/csv", FileBytes: csv2,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.SetMapping(context.Background(), appsvc.MappingInput{
		JobID: job2.ID, BranchID: branch, Mapping: job2.Mapping,
	})
	_, _ = svc.Validate(context.Background(), job2.ID, branch)
	job2, err = svc.Confirm(context.Background(), job2.ID, branch)
	if err != nil {
		t.Fatal(err)
	}
	if job2.SuccessCount != 1 {
		t.Fatalf("upsert success: %d", job2.SuccessCount)
	}
	c, err := customers.FindByPhone(context.Background(), "+971501111111", branch)
	if err != nil || c == nil {
		t.Fatal("customer missing")
	}
	if c.FullName != "Alice Updated" {
		t.Fatalf("name: %s", c.FullName)
	}
}
