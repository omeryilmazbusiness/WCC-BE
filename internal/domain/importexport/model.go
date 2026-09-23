package importexport

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type EntityType string

const (
	EntityCustomers  EntityType = "customers"
	EntityBookings   EntityType = "bookings"
	EntityPayments   EntityType = "payments"
	EntityDepartures EntityType = "departures"
)

func ValidEntityType(e EntityType) bool {
	switch e {
	case EntityCustomers, EntityBookings, EntityPayments, EntityDepartures:
		return true
	default:
		return false
	}
}

type ImportMode string

const (
	ModeCreate ImportMode = "create"
	ModeUpdate ImportMode = "update"
	ModeUpsert ImportMode = "upsert"
)

func ValidMode(m ImportMode) bool {
	switch m {
	case ModeCreate, ModeUpdate, ModeUpsert:
		return true
	default:
		return false
	}
}

type JobStatus string

const (
	StatusUploaded   JobStatus = "uploaded"
	StatusMapped     JobStatus = "mapped"
	StatusValidated  JobStatus = "validated"
	StatusQueued     JobStatus = "queued"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

// ImportJob is the import lifecycle aggregate (file bytes are source of truth for Process).
type ImportJob struct {
	ID            uuid.UUID
	BranchID      uuid.UUID
	EntityType    EntityType
	Mode          ImportMode
	Status        JobStatus
	FileName      string
	ContentType   string
	FileBytes     []byte
	StorageKey    string
	Headers       []string
	Mapping       map[string]string // canonical field → source header
	PreviewRows   [][]string
	TotalRows     int
	SuccessCount  int
	FailedCount   int
	SkippedCount  int
	RollbackToken uuid.UUID // audit reference only — not a full DB rollback
	ErrorMessage  string
	CreatedBy     uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type RowError struct {
	ID        uuid.UUID
	JobID     uuid.UUID
	RowNumber int
	Field     string
	Message   string
	RawJSON   []byte
	CreatedAt time.Time
}

type MappingTemplate struct {
	ID         uuid.UUID
	BranchID   uuid.UUID
	Name       string
	EntityType EntityType
	Mapping    map[string]string
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
}

// FieldDef describes a canonical import/export column for UI schemas.
type FieldDef struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Type     string `json:"type"` // string|phone|date|currency|status|int|money
}

// ParsedSheet is the result of CSV/XLSX parsing.
type ParsedSheet struct {
	Headers []string
	Rows    [][]string // data rows (excluding header)
}

// ExportRow is a flat string map for CSV export builders.
type ExportRow map[string]string

// Repository is the persistence port (DIP).
type Repository interface {
	CreateJob(ctx context.Context, j *ImportJob) error
	UpdateJob(ctx context.Context, j *ImportJob) error
	GetJob(ctx context.Context, id uuid.UUID) (*ImportJob, error)
	ListJobs(ctx context.Context, branchID uuid.UUID, limit int) ([]ImportJob, error)

	ReplaceRowErrors(ctx context.Context, jobID uuid.UUID, errs []RowError) error
	ListRowErrors(ctx context.Context, jobID uuid.UUID) ([]RowError, error)

	CreateTemplate(ctx context.Context, t *MappingTemplate) error
	ListTemplates(ctx context.Context, branchID uuid.UUID, entityType EntityType) ([]MappingTemplate, error)
	DeleteTemplate(ctx context.Context, id, branchID uuid.UUID) error

	ExportCustomers(ctx context.Context, branchID uuid.UUID, limit int) ([]ExportRow, error)
	ExportBookings(ctx context.Context, branchID uuid.UUID, limit int) ([]ExportRow, error)
	ExportPayments(ctx context.Context, branchID uuid.UUID, limit int) ([]ExportRow, error)
	ExportDepartures(ctx context.Context, branchID uuid.UUID, limit int) ([]ExportRow, error)
}
