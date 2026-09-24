package document

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Document lifecycle statuses (persisted).
const (
	StatusPending   = "pending"
	StatusUploaded  = "uploaded"
	StatusSubmitted = "submitted"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusExpired   = "expired"
)

const (
	KindPassport = "passport"
	KindVisa     = "visa"
	KindReceipt  = "receipt"
	KindPhoto    = "photo"
	KindOther    = "other"

	RelatedCustomer    = "customer"
	RelatedBooking     = "booking"
	RelatedLead        = "lead"
	RelatedParticipant = "participant"
)

// Document stores metadata only; bytes live in S3-compatible storage.
type Document struct {
	ID            uuid.UUID
	BranchID      uuid.UUID
	RelatedType   string
	RelatedID     uuid.UUID
	Kind          string
	FileName      string
	ContentType   string
	SizeBytes     int64
	StorageKey    string
	UploadedBy    uuid.UUID
	State         string // persisted status column
	ReviewNote    string
	ReviewedBy    *uuid.UUID
	ReviewedAt    *time.Time
	ExpiresAt     *time.Time
	ReplacesID    *uuid.UUID
	ParticipantID *uuid.UUID
	Version       int
	CreatedAt     time.Time
}

// Status returns the persisted lifecycle status (falls back for legacy rows).
func (d *Document) Status() string {
	if d.State != "" {
		return d.State
	}
	if d.SizeBytes > 0 {
		return StatusUploaded
	}
	return StatusPending
}

func ValidStatus(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case StatusPending, StatusUploaded, StatusSubmitted, StatusApproved, StatusRejected, StatusExpired:
		return true
	default:
		return false
	}
}

var docTransitions = map[string][]string{
	StatusPending:   {StatusUploaded, StatusExpired},
	StatusUploaded:  {StatusSubmitted, StatusExpired, StatusRejected},
	StatusSubmitted: {StatusApproved, StatusRejected, StatusExpired},
	StatusApproved:  {StatusExpired},
	StatusRejected:  {},
	StatusExpired:   {},
}

func CanTransition(from, to string) bool {
	for _, s := range docTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

func (d *Document) transitionTo(to string) error {
	from := d.Status()
	if !CanTransition(from, to) {
		return shared.NewInvalidState("cannot transition document from " + from + " to " + to)
	}
	d.State = to
	return nil
}

func (d *Document) MarkUploaded(sizeBytes int64) error {
	if sizeBytes <= 0 {
		return shared.NewValidation("size_bytes must be > 0")
	}
	cur := d.Status()
	if d.SizeBytes > 0 && cur != StatusPending {
		return shared.NewInvalidState("document already marked uploaded")
	}
	if cur == StatusPending {
		if err := d.transitionTo(StatusUploaded); err != nil {
			return err
		}
	} else if cur != StatusUploaded {
		return shared.NewInvalidState("document already marked uploaded")
	}
	d.SizeBytes = sizeBytes
	d.State = StatusUploaded
	return nil
}

func (d *Document) Classify(kind string) error {
	if !ValidKind(kind) {
		return shared.NewValidation("invalid kind")
	}
	switch d.Status() {
	case StatusPending, StatusUploaded, StatusSubmitted, StatusRejected:
		d.Kind = kind
		return nil
	default:
		return shared.NewInvalidState("cannot classify document in status " + d.Status())
	}
}

func (d *Document) Submit() error {
	return d.transitionTo(StatusSubmitted)
}

func (d *Document) Approve(actorID uuid.UUID, note string) error {
	if err := d.transitionTo(StatusApproved); err != nil {
		return err
	}
	now := time.Now().UTC()
	d.ReviewedBy = &actorID
	d.ReviewedAt = &now
	d.ReviewNote = strings.TrimSpace(note)
	return nil
}

func (d *Document) Reject(actorID uuid.UUID, note string) error {
	if strings.TrimSpace(note) == "" {
		return shared.NewValidation("review_note is required to reject")
	}
	if err := d.transitionTo(StatusRejected); err != nil {
		return err
	}
	now := time.Now().UTC()
	d.ReviewedBy = &actorID
	d.ReviewedAt = &now
	d.ReviewNote = strings.TrimSpace(note)
	return nil
}

func (d *Document) MarkExpired() error {
	if d.Status() == StatusExpired {
		return nil
	}
	return d.transitionTo(StatusExpired)
}

// NewReplacement builds a successor document that replaces d.
func (d *Document) NewReplacement(uploadedBy uuid.UUID, fileName, contentType, storageKey string) (*Document, error) {
	cur := d.Status()
	if cur != StatusRejected && cur != StatusExpired && cur != StatusApproved {
		return nil, shared.NewInvalidState("only rejected, expired, or approved documents can be replaced")
	}
	replaces := d.ID
	now := time.Now().UTC()
	return &Document{
		ID:            uuid.New(),
		BranchID:      d.BranchID,
		RelatedType:   d.RelatedType,
		RelatedID:     d.RelatedID,
		Kind:          d.Kind,
		FileName:      fileName,
		ContentType:   contentType,
		StorageKey:    storageKey,
		UploadedBy:    uploadedBy,
		State:         StatusPending,
		ReplacesID:    &replaces,
		ParticipantID: d.ParticipantID,
		Version:       d.Version + 1,
		ExpiresAt:     d.ExpiresAt,
		CreatedAt:     now,
	}, nil
}

func ValidKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindPassport, KindVisa, KindReceipt, KindPhoto, KindOther:
		return true
	default:
		return false
	}
}

func ValidRelatedType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case RelatedCustomer, RelatedBooking, RelatedLead, RelatedParticipant:
		return true
	default:
		return false
	}
}

// SanitizeFileName keeps a single path segment safe for object keys.
func SanitizeFileName(name string) (string, error) {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.ReplaceAll(base, "..", "")
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "", shared.NewValidation("file_name is required")
	}
	if len(base) > 200 {
		return "", shared.NewValidation("file_name too long")
	}
	return base, nil
}

// Policy defines required document kinds for a branch (optional package/nationality scope).
type Policy struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	Name         string
	PackageID    *uuid.UUID
	Nationality  string
	IsActive     bool
	CreatedAt    time.Time
	Requirements []Requirement
}

type Requirement struct {
	ID       uuid.UUID
	PolicyID uuid.UUID
	Kind     string
	Required bool
	Label    string
}

// ChecklistItem is a required/optional doc kind evaluated against approved uploads.
type ChecklistItem struct {
	Kind       string     `json:"kind"`
	Label      string     `json:"label"`
	Required   bool       `json:"required"`
	Satisfied  bool       `json:"satisfied"`
	DocumentID *uuid.UUID `json:"document_id,omitempty"`
}

type Checklist struct {
	BookingID         uuid.UUID       `json:"booking_id"`
	PolicyID          *uuid.UUID      `json:"policy_id,omitempty"`
	PolicyName        string          `json:"policy_name,omitempty"`
	Items             []ChecklistItem `json:"items"`
	MissingRequired   []string        `json:"missing_required"`
}

type MissingDocsRow struct {
	BookingID     uuid.UUID  `json:"booking_id"`
	ParticipantID *uuid.UUID `json:"participant_id,omitempty"`
	CustomerID    uuid.UUID  `json:"customer_id"`
	MissingKinds  []string   `json:"missing_kinds"`
}

// SubjectRef identifies an entity that may hold related documents.
type SubjectRef struct {
	RelatedType string
	RelatedID   uuid.UUID
}

type Repository interface {
	Create(ctx context.Context, d *Document) error
	Update(ctx context.Context, d *Document) error
	FindByID(ctx context.Context, id uuid.UUID) (*Document, error)
	ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]Document, error)
	ListExpiring(ctx context.Context, onOrBefore time.Time, limit int) ([]Document, error)
	ListApprovedBySubjects(ctx context.Context, subjects []SubjectRef) ([]Document, error)

	CreatePolicy(ctx context.Context, p *Policy) error
	UpdatePolicy(ctx context.Context, p *Policy) error
	ReplaceRequirements(ctx context.Context, policyID uuid.UUID, reqs []Requirement) error
	FindPolicyByID(ctx context.Context, id uuid.UUID) (*Policy, error)
	ListPolicies(ctx context.Context, branchID uuid.UUID) ([]Policy, error)
	FindActivePolicy(ctx context.Context, branchID uuid.UUID, packageID *uuid.UUID, nationality string) (*Policy, error)
}
