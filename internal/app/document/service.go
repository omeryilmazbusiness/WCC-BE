package document

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// ObjectStore is an alias to the shared storage port (DIP).
type ObjectStore = shared.ObjectStore

// BookingContext supplies booking/participant subjects for checklists (ISP).
type BookingContext interface {
	BookingSubjects(ctx context.Context, bookingID uuid.UUID) (branchID, customerID, departureID uuid.UUID, participantIDs []uuid.UUID, err error)
	BookingsOnDeparture(ctx context.Context, departureID uuid.UUID) ([]DepartureBooking, error)
}

// DepartureBooking is the display context of a booking on a departure.
type DepartureBooking struct {
	ID             uuid.UUID
	CustomerID     uuid.UUID
	RefCode        string
	CustomerName   string
	CustomerNameAr string
	Status         string
	PaxCount       int
}

// bookingCancelled is excluded from document follow-up.
const bookingCancelled = "cancelled"

type PresignUploadInput struct {
	BranchID      uuid.UUID
	RelatedType   string
	RelatedID     uuid.UUID
	Kind          string
	FileName      string
	ContentType   string
	UploadedBy    uuid.UUID
	ParticipantID *uuid.UUID
	ExpiresAt     *time.Time
}

type PresignUploadResult struct {
	DocumentID   uuid.UUID `json:"document_id"`
	UploadURL    string    `json:"upload_url"`
	StorageKey   string    `json:"storage_key"`
	ExpiresInSec int       `json:"expires_in_sec"`
	Status       string    `json:"status"`
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
	ID            uuid.UUID  `json:"id"`
	BranchID      uuid.UUID  `json:"branch_id"`
	RelatedType   string     `json:"related_type"`
	RelatedID     uuid.UUID  `json:"related_id"`
	Kind          string     `json:"kind"`
	FileName      string     `json:"file_name"`
	ContentType   string     `json:"content_type"`
	SizeBytes     int64      `json:"size_bytes"`
	StorageKey    string     `json:"storage_key"`
	Status        string     `json:"status"`
	ReviewNote    string     `json:"review_note"`
	ReviewedBy    *uuid.UUID `json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	ReplacesID    *uuid.UUID `json:"replaces_id,omitempty"`
	ParticipantID *uuid.UUID `json:"participant_id,omitempty"`
	Version       int        `json:"version"`
	UploadedBy    uuid.UUID  `json:"uploaded_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

func toDTO(d *domain.Document) DocumentDTO {
	ver := d.Version
	if ver <= 0 {
		ver = 1
	}
	return DocumentDTO{
		ID: d.ID, BranchID: d.BranchID, RelatedType: d.RelatedType, RelatedID: d.RelatedID,
		Kind: d.Kind, FileName: d.FileName, ContentType: d.ContentType, SizeBytes: d.SizeBytes,
		StorageKey: d.StorageKey, Status: d.Status(), ReviewNote: d.ReviewNote,
		ReviewedBy: d.ReviewedBy, ReviewedAt: d.ReviewedAt, ExpiresAt: d.ExpiresAt,
		ReplacesID: d.ReplacesID, ParticipantID: d.ParticipantID, Version: ver,
		UploadedBy: d.UploadedBy, CreatedAt: d.CreatedAt,
	}
}

type PolicyDTO struct {
	ID           uuid.UUID        `json:"id"`
	BranchID     uuid.UUID        `json:"branch_id"`
	Name         string           `json:"name"`
	PackageID    *uuid.UUID       `json:"package_id,omitempty"`
	Nationality  string           `json:"nationality"`
	IsActive     bool             `json:"is_active"`
	CreatedAt    time.Time        `json:"created_at"`
	Requirements []RequirementDTO `json:"requirements"`
}

type RequirementDTO struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"`
	Required bool      `json:"required"`
	Label    string    `json:"label"`
}

type UpsertPolicyInput struct {
	ID           *uuid.UUID
	BranchID     uuid.UUID
	Name         string
	PackageID    *uuid.UUID
	Nationality  string
	IsActive     bool
	Requirements []RequirementDTO
}

type Service struct {
	repo       domain.Repository
	storage    ObjectStore
	tx         tx.Runner
	bookings   BookingContext
	audit      audit.Recorder
	outbox     events.Outbox
	alerts     ExpiryAlerts
	ledger     shared.RunLedger
	presignTTL time.Duration
}

func NewService(repo domain.Repository, storage ObjectStore, txm tx.Runner) *Service {
	return &Service{repo: repo, storage: storage, tx: txm, presignTTL: 15 * time.Minute}
}

func (s *Service) SetBookingContext(b BookingContext) { s.bookings = b }
func (s *Service) SetAuditor(a audit.Recorder)        { s.audit = a }
func (s *Service) SetOutbox(o events.Outbox)          { s.outbox = o }
func (s *Service) SetExpiryAlerts(a ExpiryAlerts)     { s.alerts = a }
func (s *Service) SetRunLedger(l shared.RunLedger)    { s.ledger = l }

// saveTransition persists a status change and records document.status_changed
// in the same transaction.
func (s *Service) saveTransition(ctx context.Context, doc *domain.Document, from string) error {
	if err := s.repo.Update(ctx, doc); err != nil {
		return err
	}
	if doc.Status() == from {
		return nil
	}
	var bookingID *uuid.UUID
	if doc.RelatedType == domain.RelatedBooking {
		id := doc.RelatedID
		bookingID = &id
	}
	return events.Record(ctx, s.outbox, events.Event{Name: events.DocumentStatusChanged, Payload: events.DocumentStatusChangedPayload{
		DocumentID: doc.ID, BranchID: doc.BranchID, BookingID: bookingID, From: from, To: doc.Status(),
	}})
}

// transition loads a document, applies change and saves it atomically.
func (s *Service) transition(ctx context.Context, id uuid.UUID, change func(*domain.Document) error) (*DocumentDTO, error) {
	var out *domain.Document
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		doc, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("document")
		}
		from := doc.Status()
		if err := change(doc); err != nil {
			return err
		}
		out = doc
		return s.saveTransition(ctx, doc, from)
	})
	if err != nil {
		return nil, err
	}
	dto := toDTO(out)
	return &dto, nil
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
		ID:            id,
		BranchID:      in.BranchID,
		RelatedType:   in.RelatedType,
		RelatedID:     in.RelatedID,
		Kind:          kind,
		FileName:      fileName,
		ContentType:   in.ContentType,
		StorageKey:    key,
		UploadedBy:    in.UploadedBy,
		State:         domain.StatusPending,
		ParticipantID: in.ParticipantID,
		ExpiresAt:     in.ExpiresAt,
		Version:       1,
		CreatedAt:     time.Now().UTC(),
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
	return s.transition(ctx, in.DocumentID, func(doc *domain.Document) error {
		return doc.MarkUploaded(in.SizeBytes)
	})
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
	if doc.SizeBytes <= 0 {
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

func (s *Service) Classify(ctx context.Context, id uuid.UUID, kind string) (*DocumentDTO, error) {
	doc, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("document")
	}
	if err := doc.Classify(kind); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, doc); err != nil {
		return nil, err
	}
	dto := toDTO(doc)
	return &dto, nil
}

func (s *Service) Submit(ctx context.Context, id uuid.UUID) (*DocumentDTO, error) {
	return s.transition(ctx, id, func(doc *domain.Document) error { return doc.Submit() })
}

func (s *Service) Approve(ctx context.Context, id, actorID uuid.UUID, note string) (*DocumentDTO, error) {
	return s.review(ctx, id, actorID, "document.approved", func(doc *domain.Document) error {
		return doc.Approve(actorID, note)
	})
}

func (s *Service) Reject(ctx context.Context, id, actorID uuid.UUID, note string) (*DocumentDTO, error) {
	return s.review(ctx, id, actorID, "document.rejected", func(doc *domain.Document) error {
		return doc.Reject(actorID, note)
	})
}

// review applies a review decision and its audit event atomically (fail closed).
func (s *Service) review(ctx context.Context, id, actorID uuid.UUID, action string, decide func(*domain.Document) error) (*DocumentDTO, error) {
	var out *domain.Document
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		doc, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("document")
		}
		before := reviewSnapshot(doc)
		from := doc.Status()
		if err := decide(doc); err != nil {
			return err
		}
		if err := s.saveTransition(ctx, doc, from); err != nil {
			return err
		}
		out = doc
		if s.audit == nil {
			return nil
		}
		docID, branchID := doc.ID, doc.BranchID
		return s.audit.Record(ctx, audit.RecordInput{
			ActorID: actorID, Action: action, EntityType: "document", EntityID: &docID, BranchID: &branchID,
			Before: before, After: reviewSnapshot(doc),
			Extra: map[string]any{
				"related_type": doc.RelatedType, "related_id": doc.RelatedID, "kind": doc.Kind,
				"file_name": doc.FileName, "version": doc.Version,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	dto := toDTO(out)
	return &dto, nil
}

func reviewSnapshot(d *domain.Document) map[string]any {
	return map[string]any{
		"status": d.Status(), "review_note": d.ReviewNote, "reviewed_by": d.ReviewedBy, "reviewed_at": d.ReviewedAt,
	}
}

type ReplaceInput struct {
	DocumentID  uuid.UUID
	FileName    string
	ContentType string
	UploadedBy  uuid.UUID
}

func (s *Service) Replace(ctx context.Context, in ReplaceInput) (*PresignUploadResult, error) {
	fileName, err := domain.SanitizeFileName(in.FileName)
	if err != nil {
		return nil, err
	}
	if in.ContentType == "" {
		return nil, shared.NewValidation("content_type is required")
	}
	old, err := s.repo.FindByID(ctx, in.DocumentID)
	if err != nil {
		return nil, shared.NewNotFound("document")
	}
	id := uuid.New()
	key := "docs/" + old.BranchID.String() + "/" + id.String() + "/" + fileName
	url, err := s.storage.PresignPut(ctx, key, in.ContentType, s.presignTTL)
	if err != nil {
		return nil, err
	}
	rep, err := old.NewReplacement(in.UploadedBy, fileName, in.ContentType, key)
	if err != nil {
		return nil, err
	}
	rep.ID = id
	if err := s.repo.Create(ctx, rep); err != nil {
		return nil, err
	}
	return &PresignUploadResult{
		DocumentID:   rep.ID,
		UploadURL:    url,
		StorageKey:   key,
		ExpiresInSec: int(s.presignTTL.Seconds()),
		Status:       rep.Status(),
	}, nil
}

func (s *Service) ListPolicies(ctx context.Context, branchID uuid.UUID) ([]PolicyDTO, error) {
	items, err := s.repo.ListPolicies(ctx, branchID)
	if err != nil {
		return nil, err
	}
	out := make([]PolicyDTO, 0, len(items))
	for i := range items {
		out = append(out, toPolicyDTO(&items[i]))
	}
	return out, nil
}

func toPolicyDTO(p *domain.Policy) PolicyDTO {
	reqs := make([]RequirementDTO, 0, len(p.Requirements))
	for _, r := range p.Requirements {
		reqs = append(reqs, RequirementDTO{ID: r.ID, Kind: r.Kind, Required: r.Required, Label: r.Label})
	}
	return PolicyDTO{
		ID: p.ID, BranchID: p.BranchID, Name: p.Name, PackageID: p.PackageID,
		Nationality: p.Nationality, IsActive: p.IsActive, CreatedAt: p.CreatedAt, Requirements: reqs,
	}
}

func (s *Service) UpsertPolicy(ctx context.Context, in UpsertPolicyInput) (*PolicyDTO, error) {
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	if in.Name == "" {
		return nil, shared.NewValidation("name is required")
	}
	for _, r := range in.Requirements {
		if !domain.ValidKind(r.Kind) {
			return nil, shared.NewValidation("invalid requirement kind: " + r.Kind)
		}
	}
	var out *domain.Policy
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if in.ID != nil && *in.ID != uuid.Nil {
			p, err := s.repo.FindPolicyByID(ctx, *in.ID)
			if err != nil {
				return shared.NewNotFound("document_policy")
			}
			p.Name = in.Name
			p.PackageID = in.PackageID
			p.Nationality = in.Nationality
			p.IsActive = in.IsActive
			if err := s.repo.UpdatePolicy(ctx, p); err != nil {
				return err
			}
			reqs := make([]domain.Requirement, 0, len(in.Requirements))
			for _, r := range in.Requirements {
				id := r.ID
				if id == uuid.Nil {
					id = uuid.New()
				}
				reqs = append(reqs, domain.Requirement{ID: id, PolicyID: p.ID, Kind: r.Kind, Required: r.Required, Label: r.Label})
			}
			if err := s.repo.ReplaceRequirements(ctx, p.ID, reqs); err != nil {
				return err
			}
			p.Requirements = reqs
			out = p
			return nil
		}
		p := &domain.Policy{
			ID: uuid.New(), BranchID: in.BranchID, Name: in.Name, PackageID: in.PackageID,
			Nationality: in.Nationality, IsActive: in.IsActive, CreatedAt: time.Now().UTC(),
		}
		if err := s.repo.CreatePolicy(ctx, p); err != nil {
			return err
		}
		reqs := make([]domain.Requirement, 0, len(in.Requirements))
		for _, r := range in.Requirements {
			reqs = append(reqs, domain.Requirement{
				ID: uuid.New(), PolicyID: p.ID, Kind: r.Kind, Required: r.Required, Label: r.Label,
			})
		}
		if err := s.repo.ReplaceRequirements(ctx, p.ID, reqs); err != nil {
			return err
		}
		p.Requirements = reqs
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	dto := toPolicyDTO(out)
	return &dto, nil
}

func (s *Service) Checklist(ctx context.Context, bookingID uuid.UUID) (*domain.Checklist, error) {
	if s.bookings == nil {
		return nil, shared.NewValidation("booking context not configured")
	}
	branchID, customerID, _, participantIDs, err := s.bookings.BookingSubjects(ctx, bookingID)
	if err != nil {
		return nil, shared.NewNotFound("booking")
	}
	policy, err := s.repo.FindActivePolicy(ctx, branchID, nil, "")
	if err != nil || policy == nil {
		return &domain.Checklist{BookingID: bookingID, Items: []domain.ChecklistItem{}, MissingRequired: []string{}}, nil
	}
	subjects := []domain.SubjectRef{
		{RelatedType: domain.RelatedBooking, RelatedID: bookingID},
		{RelatedType: domain.RelatedCustomer, RelatedID: customerID},
	}
	for _, pid := range participantIDs {
		subjects = append(subjects, domain.SubjectRef{RelatedType: domain.RelatedParticipant, RelatedID: pid})
	}
	approved, err := s.repo.ListApprovedBySubjects(ctx, subjects)
	if err != nil {
		return nil, err
	}
	byKind := map[string]uuid.UUID{}
	for i := range approved {
		k := approved[i].Kind
		if _, ok := byKind[k]; !ok {
			byKind[k] = approved[i].ID
		}
	}
	cl := &domain.Checklist{
		BookingID: bookingID, PolicyName: policy.Name, Items: []domain.ChecklistItem{}, MissingRequired: []string{},
	}
	pid := policy.ID
	cl.PolicyID = &pid
	for _, req := range policy.Requirements {
		item := domain.ChecklistItem{Kind: req.Kind, Label: req.Label, Required: req.Required}
		if id, ok := byKind[req.Kind]; ok {
			item.Satisfied = true
			item.DocumentID = &id
		} else if req.Required {
			cl.MissingRequired = append(cl.MissingRequired, req.Kind)
		}
		cl.Items = append(cl.Items, item)
	}
	return cl, nil
}

func (s *Service) DepartureMissingDocs(ctx context.Context, departureID uuid.UUID) ([]domain.MissingDocsRow, error) {
	if s.bookings == nil {
		return nil, shared.NewValidation("booking context not configured")
	}
	if departureID == uuid.Nil {
		return nil, shared.NewValidation("departure_id is required")
	}
	bookings, err := s.bookings.BookingsOnDeparture(ctx, departureID)
	if err != nil {
		return nil, err
	}
	out := []domain.MissingDocsRow{}
	for _, bk := range bookings {
		if bk.Status == bookingCancelled {
			continue
		}
		cl, err := s.Checklist(ctx, bk.ID)
		if err != nil || len(cl.MissingRequired) == 0 {
			continue
		}
		out = append(out, domain.MissingDocsRow{
			BookingID: bk.ID, CustomerID: bk.CustomerID, MissingKinds: cl.MissingRequired,
			RefCode: bk.RefCode, CustomerName: bk.CustomerName, CustomerNameAr: bk.CustomerNameAr,
			BookingStatus: bk.Status, PaxCount: bk.PaxCount,
		})
	}
	return out, nil
}

// MissingRequiredKinds is used by booking readiness (docs-driven blocking).
func (s *Service) MissingRequiredKinds(ctx context.Context, bookingID uuid.UUID) ([]string, error) {
	cl, err := s.Checklist(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	return append([]string{}, cl.MissingRequired...), nil
}

// ExpiryReminderDays are the days-before-expiry reminder slots (T-280).
var ExpiryReminderDays = []int{30, 14, 7}

// ExpiryAlerts delivers document expiry reminders (DIP).
type ExpiryAlerts interface {
	DocumentExpiring(ctx context.Context, doc DocumentDTO, daysLeft int) error
	DocumentExpired(ctx context.Context, doc DocumentDTO) error
}

const jobExpiryReminder = "document.expiry_reminder"

// ProcessExpiryReminders marks documents past expiry as expired and sends one
// reminder per slot (30/14/7 days) to the uploader; safe to run repeatedly.
func (s *Service) ProcessExpiryReminders(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	today := dateOf(now)
	horizon := today.AddDate(0, 0, ExpiryReminderDays[0])
	items, err := s.repo.ListExpiring(ctx, horizon, limit)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range items {
		d := items[i]
		daysLeft := int(dateOf(*d.ExpiresAt).Sub(today).Hours() / 24)
		var acted bool
		if daysLeft < 0 {
			acted, err = s.expire(ctx, &d)
		} else {
			acted, err = s.remindExpiry(ctx, &d, daysLeft)
		}
		if err != nil {
			return n, err
		}
		if acted {
			n++
		}
	}
	return n, nil
}

func (s *Service) expire(ctx context.Context, d *domain.Document) (bool, error) {
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		from := d.Status()
		if err := d.MarkExpired(); err != nil {
			return err
		}
		if err := s.saveTransition(ctx, d, from); err != nil {
			return err
		}
		if s.alerts == nil {
			return nil
		}
		return s.alerts.DocumentExpired(ctx, toDTO(d))
	})
	if err != nil {
		if errors.Is(err, shared.ErrInvalidState) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Service) remindExpiry(ctx context.Context, d *domain.Document, daysLeft int) (bool, error) {
	slot := reminderSlot(daysLeft)
	if slot == 0 || s.alerts == nil || s.ledger == nil {
		return false, nil
	}
	sent := false
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		claimed, err := s.ledger.Claim(ctx, jobExpiryReminder, d.ID.String(), strconv.Itoa(slot))
		if err != nil || !claimed {
			return err
		}
		sent = true
		return s.alerts.DocumentExpiring(ctx, toDTO(d), daysLeft)
	})
	return sent, err
}

// reminderSlot is the tightest reminder slot daysLeft falls into (0 = none).
func reminderSlot(daysLeft int) int {
	slot := 0
	for _, d := range ExpiryReminderDays {
		if daysLeft <= d {
			slot = d
		}
	}
	return slot
}

func dateOf(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
