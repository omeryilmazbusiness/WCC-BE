package filesync

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Provider for connected workbook hosts (T-202).
type Provider string

const (
	ProviderOneDrive   Provider = "onedrive"
	ProviderSharePoint Provider = "sharepoint"
)

func ValidProvider(p Provider) bool {
	switch p {
	case ProviderOneDrive, ProviderSharePoint:
		return true
	default:
		return false
	}
}

// SourceOfTruth declares which system wins by default.
type SourceOfTruth string

const (
	TruthPlatform      SourceOfTruth = "platform"
	TruthFile          SourceOfTruth = "file"
	TruthManualReview  SourceOfTruth = "manual_review"
)

func ValidSourceOfTruth(s SourceOfTruth) bool {
	switch s {
	case TruthPlatform, TruthFile, TruthManualReview:
		return true
	default:
		return false
	}
}

// ConflictPolicy for field-level clashes during sync.
type ConflictPolicy string

const (
	ConflictPreferPlatform ConflictPolicy = "prefer_platform"
	ConflictPreferFile     ConflictPolicy = "prefer_file"
	ConflictFlag           ConflictPolicy = "flag"
)

func ValidConflictPolicy(p ConflictPolicy) bool {
	switch p {
	case ConflictPreferPlatform, ConflictPreferFile, ConflictFlag:
		return true
	default:
		return false
	}
}

type ConnStatus string

const (
	StatusDisconnected ConnStatus = "disconnected"
	StatusConnected    ConnStatus = "connected"
	StatusError        ConnStatus = "error"
	StatusSyncing      ConnStatus = "syncing"
)

type RunStatus string

const (
	RunRunning    RunStatus = "running"
	RunOK         RunStatus = "ok"
	RunError      RunStatus = "error"
	RunConflicts  RunStatus = "conflicts"
)

type Direction string

const (
	DirectionPull          Direction = "pull"
	DirectionPush          Direction = "push"
	DirectionBidirectional Direction = "bidirectional"
)

// Connection binds a cloud workbook to a branch entity type.
type Connection struct {
	ID              uuid.UUID
	BranchID        uuid.UUID
	Provider        Provider
	DisplayName     string
	RemotePath      string
	EntityType      string
	SourceOfTruth   SourceOfTruth
	ConflictPolicy  ConflictPolicy
	Enabled         bool
	Status          ConnStatus
	LastSyncAt      *time.Time
	LastError       string
	ConfigJSON      json.RawMessage
	CreatedBy       *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (c *Connection) Normalize() error {
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	c.RemotePath = strings.TrimSpace(c.RemotePath)
	c.EntityType = strings.TrimSpace(c.EntityType)
	if c.EntityType == "" {
		c.EntityType = "customers"
	}
	if !ValidProvider(c.Provider) {
		return shared.NewValidation("invalid provider")
	}
	if !ValidSourceOfTruth(c.SourceOfTruth) {
		return shared.NewValidation("invalid source_of_truth")
	}
	if !ValidConflictPolicy(c.ConflictPolicy) {
		return shared.NewValidation("invalid conflict_policy")
	}
	if c.DisplayName == "" {
		c.DisplayName = string(c.Provider) + " workbook"
	}
	if len(c.ConfigJSON) == 0 {
		c.ConfigJSON = json.RawMessage(`{}`)
	}
	return nil
}

// Run is one sync attempt audit record.
type Run struct {
	ID           uuid.UUID
	ConnectionID uuid.UUID
	BranchID     uuid.UUID
	ActorID      *uuid.UUID
	Direction    Direction
	Status       RunStatus
	RowsRead     int
	RowsApplied  int
	Conflicts    int
	SummaryJSON  json.RawMessage
	ErrorMessage string
	StartedAt    time.Time
	FinishedAt   *time.Time
}

// FieldConflict is a pure-domain clash between platform and file values.
type FieldConflict struct {
	Key           string
	PlatformValue string
	FileValue     string
}

// Resolution is the outcome of ConflictPolicy (pure).
type Resolution struct {
	Chosen  string
	Flagged bool
	Reason  string
}

// CloudFileProvider is the DIP port for OneDrive/SharePoint adapters.
type CloudFileProvider interface {
	Name() Provider
	Probe(ctx context.Context, remotePath string, cfg json.RawMessage) (ConnStatus, string, error)
	PullSample(ctx context.Context, remotePath string, cfg json.RawMessage) (rowsRead int, sample []map[string]string, err error)
}

// Repository persistence port.
type Repository interface {
	ListConnections(ctx context.Context, branchID uuid.UUID) ([]Connection, error)
	GetConnection(ctx context.Context, branchID, id uuid.UUID) (*Connection, error)
	InsertConnection(ctx context.Context, c *Connection) error
	UpdateConnection(ctx context.Context, c *Connection) error
	DeleteConnection(ctx context.Context, branchID, id uuid.UUID) error
	InsertRun(ctx context.Context, run *Run) error
	UpdateRun(ctx context.Context, run *Run) error
	ListRuns(ctx context.Context, branchID uuid.UUID, connectionID *uuid.UUID, limit int) ([]Run, error)
}
