package document_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/document"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memLedger struct{ slots map[string]bool }

func (l *memLedger) Claim(_ context.Context, job, scope, period string) (bool, error) {
	k := job + "|" + scope + "|" + period
	if l.slots[k] {
		return false, nil
	}
	l.slots[k] = true
	return true, nil
}

type recordingAlerts struct {
	expiring []int
	expired  int
}

func (a *recordingAlerts) DocumentExpiring(_ context.Context, _ appsvc.DocumentDTO, daysLeft int) error {
	a.expiring = append(a.expiring, daysLeft)
	return nil
}

func (a *recordingAlerts) DocumentExpired(context.Context, appsvc.DocumentDTO) error {
	a.expired++
	return nil
}

type memOutbox struct{ events []events.Event }

func (o *memOutbox) Add(_ context.Context, ev events.Event) error {
	o.events = append(o.events, ev)
	return nil
}

func seedDoc(repo *memRepo, expires time.Time) uuid.UUID {
	id := uuid.New()
	_ = repo.Create(context.Background(), &domain.Document{
		ID: id, BranchID: uuid.New(), RelatedType: domain.RelatedBooking, RelatedID: uuid.New(),
		Kind: domain.KindPassport, FileName: "p.pdf", UploadedBy: uuid.New(), State: domain.StatusApproved,
		SizeBytes: 10, ExpiresAt: &expires, Version: 1,
	})
	return id
}

func TestExpiryRemindersFireOncePerSlot(t *testing.T) {
	repo := newMemRepo()
	svc := appsvc.NewService(repo, memStore{}, tx.Nop{})
	alerts := &recordingAlerts{}
	svc.SetExpiryAlerts(alerts)
	svc.SetRunLedger(&memLedger{slots: map[string]bool{}})
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	seedDoc(repo, now.AddDate(0, 0, 20)) // 30-day slot
	seedDoc(repo, now.AddDate(0, 0, 5))  // 7-day slot
	seedDoc(repo, now.AddDate(0, 0, 60)) // outside horizon

	n, err := svc.ProcessExpiryReminders(context.Background(), now, 100)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, _ := svc.ProcessExpiryReminders(context.Background(), now, 100); n != 0 {
		t.Fatalf("slots must fire once, got %d", n)
	}
	// Six days later the 20-day document enters the 14-day slot and the
	// 5-day document has expired.
	if n, _ := svc.ProcessExpiryReminders(context.Background(), now.AddDate(0, 0, 6), 100); n != 2 || alerts.expired != 1 {
		t.Fatalf("want 14-day reminder + expiry, got %d (expired %d)", n, alerts.expired)
	}
	if fmt.Sprint(alerts.expiring) != "[20 5 14]" && fmt.Sprint(alerts.expiring) != "[5 20 14]" {
		t.Fatalf("reminders: %v", alerts.expiring)
	}
}

func TestExpiredDocumentIsMarkedAndPublished(t *testing.T) {
	repo := newMemRepo()
	svc := appsvc.NewService(repo, memStore{}, tx.Nop{})
	alerts := &recordingAlerts{}
	out := &memOutbox{}
	svc.SetExpiryAlerts(alerts)
	svc.SetOutbox(out)
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	id := seedDoc(repo, now.AddDate(0, 0, -1))

	if n, err := svc.ProcessExpiryReminders(context.Background(), now, 100); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	doc, _ := repo.FindByID(context.Background(), id)
	if doc.Status() != domain.StatusExpired || alerts.expired != 1 {
		t.Fatalf("status=%s expired alerts=%d", doc.Status(), alerts.expired)
	}
	if len(out.events) != 1 {
		t.Fatalf("want one status event, got %d", len(out.events))
	}
	p := out.events[0].Payload.(events.DocumentStatusChangedPayload)
	if p.From != domain.StatusApproved || p.To != domain.StatusExpired || p.BookingID == nil {
		t.Fatalf("payload %+v", p)
	}
}

func TestReviewPublishesStatusChange(t *testing.T) {
	repo := newMemRepo()
	svc := appsvc.NewService(repo, memStore{}, tx.Nop{})
	out := &memOutbox{}
	svc.SetOutbox(out)
	actor := uuid.New()
	presign, err := svc.PresignUpload(context.Background(), appsvc.PresignUploadInput{
		BranchID: uuid.New(), RelatedType: domain.RelatedBooking, RelatedID: uuid.New(),
		Kind: domain.KindPassport, FileName: "p.pdf", ContentType: "application/pdf", UploadedBy: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.CompleteUpload(context.Background(), appsvc.CompleteUploadInput{DocumentID: presign.DocumentID, SizeBytes: 5, ActorID: actor})
	_, _ = svc.Submit(context.Background(), presign.DocumentID)
	if _, err := svc.Approve(context.Background(), presign.DocumentID, actor, "ok"); err != nil {
		t.Fatal(err)
	}
	var tos []string
	for _, ev := range out.events {
		tos = append(tos, ev.Payload.(events.DocumentStatusChangedPayload).To)
	}
	if fmt.Sprint(tos) != fmt.Sprint([]string{domain.StatusUploaded, domain.StatusSubmitted, domain.StatusApproved}) {
		t.Fatalf("transitions: %v", tos)
	}
}
