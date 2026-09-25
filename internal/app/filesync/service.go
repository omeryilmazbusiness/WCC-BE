package filesync

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/filesync"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// ProviderRegistry resolves cloud file adapters (DIP).
type ProviderRegistry interface {
	Get(p domain.Provider) (domain.CloudFileProvider, bool)
}

type CreateInput struct {
	BranchID       uuid.UUID            `json:"-"`
	ActorID        uuid.UUID            `json:"-"`
	Provider       domain.Provider      `json:"provider"`
	DisplayName    string               `json:"display_name"`
	RemotePath     string               `json:"remote_path"`
	EntityType     string               `json:"entity_type"`
	SourceOfTruth  domain.SourceOfTruth `json:"source_of_truth"`
	ConflictPolicy domain.ConflictPolicy `json:"conflict_policy"`
	Enabled        *bool                `json:"enabled"`
	ConfigJSON     json.RawMessage      `json:"config_json"`
}

type UpdateInput struct {
	DisplayName    *string               `json:"display_name"`
	RemotePath     *string               `json:"remote_path"`
	EntityType     *string               `json:"entity_type"`
	SourceOfTruth  *domain.SourceOfTruth `json:"source_of_truth"`
	ConflictPolicy *domain.ConflictPolicy `json:"conflict_policy"`
	Enabled        *bool                 `json:"enabled"`
	ConfigJSON     json.RawMessage       `json:"config_json"`
}

type Service struct {
	repo     domain.Repository
	registry ProviderRegistry
}

func NewService(repo domain.Repository, registry ProviderRegistry) *Service {
	return &Service{repo: repo, registry: registry}
}

func (s *Service) List(ctx context.Context, branchID uuid.UUID) ([]domain.Connection, error) {
	return s.repo.ListConnections(ctx, branchID)
}

func (s *Service) Get(ctx context.Context, branchID, id uuid.UUID) (*domain.Connection, error) {
	c, err := s.repo.GetConnection(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("file_sync_connection")
	}
	return c, nil
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Connection, error) {
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	now := time.Now().UTC()
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	truth := in.SourceOfTruth
	if truth == "" {
		truth = domain.TruthPlatform
	}
	policy := in.ConflictPolicy
	if policy == "" {
		policy = domain.ConflictPreferPlatform
	}
	actor := in.ActorID
	c := &domain.Connection{
		ID: uuid.New(), BranchID: in.BranchID, Provider: in.Provider,
		DisplayName: in.DisplayName, RemotePath: in.RemotePath, EntityType: in.EntityType,
		SourceOfTruth: truth, ConflictPolicy: policy, Enabled: enabled,
		Status: domain.StatusDisconnected, ConfigJSON: in.ConfigJSON,
		CreatedBy: &actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := c.Normalize(); err != nil {
		return nil, err
	}
	if err := s.repo.InsertConnection(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) Update(ctx context.Context, branchID, id uuid.UUID, in UpdateInput) (*domain.Connection, error) {
	c, err := s.repo.GetConnection(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("file_sync_connection")
	}
	if in.DisplayName != nil {
		c.DisplayName = *in.DisplayName
	}
	if in.RemotePath != nil {
		c.RemotePath = *in.RemotePath
	}
	if in.EntityType != nil {
		c.EntityType = *in.EntityType
	}
	if in.SourceOfTruth != nil {
		c.SourceOfTruth = *in.SourceOfTruth
	}
	if in.ConflictPolicy != nil {
		c.ConflictPolicy = *in.ConflictPolicy
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	if len(in.ConfigJSON) > 0 {
		c.ConfigJSON = in.ConfigJSON
	}
	if err := c.Normalize(); err != nil {
		return nil, err
	}
	c.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateConnection(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) Delete(ctx context.Context, branchID, id uuid.UUID) error {
	if err := s.repo.DeleteConnection(ctx, branchID, id); err != nil {
		return shared.NewNotFound("file_sync_connection")
	}
	return nil
}

// Connect probes the cloud provider and marks the connection status.
func (s *Service) Connect(ctx context.Context, branchID, id, actorID uuid.UUID) (*domain.Connection, error) {
	c, err := s.repo.GetConnection(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("file_sync_connection")
	}
	p, ok := s.registry.Get(c.Provider)
	if !ok {
		return nil, shared.NewValidation("cloud provider adapter missing")
	}
	status, msg, probeErr := p.Probe(ctx, c.RemotePath, c.ConfigJSON)
	now := time.Now().UTC()
	c.UpdatedAt = now
	if probeErr != nil {
		c.Status = domain.StatusError
		c.LastError = msg
		_ = s.repo.UpdateConnection(ctx, c)
		return c, shared.NewValidation(msg)
	}
	c.Status = status
	c.LastError = ""
	if msg != "" {
		c.LastError = "" // clear; probe message is informational
	}
	_ = msg
	if err := s.repo.UpdateConnection(ctx, c); err != nil {
		return nil, err
	}
	_ = actorID
	return c, nil
}

// SyncNow pulls a sample workbook and applies conflict policy (platform DB authoritative messaging).
func (s *Service) SyncNow(ctx context.Context, branchID, id, actorID uuid.UUID) (*domain.Run, error) {
	c, err := s.repo.GetConnection(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("file_sync_connection")
	}
	if !c.Enabled {
		return nil, shared.NewInvalidState("connection is disabled")
	}
	p, ok := s.registry.Get(c.Provider)
	if !ok {
		return nil, shared.NewValidation("cloud provider adapter missing")
	}

	now := time.Now().UTC()
	c.Status = domain.StatusSyncing
	c.UpdatedAt = now
	_ = s.repo.UpdateConnection(ctx, c)

	actor := actorID
	run := &domain.Run{
		ID: uuid.New(), ConnectionID: c.ID, BranchID: branchID, ActorID: &actor,
		Direction: domain.DirectionPull, Status: domain.RunRunning,
		SummaryJSON: json.RawMessage(`{}`), StartedAt: now,
	}
	if err := s.repo.InsertRun(ctx, run); err != nil {
		return nil, err
	}

	rowsRead, sample, pullErr := p.PullSample(ctx, c.RemotePath, c.ConfigJSON)
	finished := time.Now().UTC()
	run.FinishedAt = &finished
	run.RowsRead = rowsRead

	if pullErr != nil {
		run.Status = domain.RunError
		run.ErrorMessage = pullErr.Error()
		c.Status = domain.StatusError
		c.LastError = pullErr.Error()
		c.UpdatedAt = finished
		_ = s.repo.UpdateRun(ctx, run)
		_ = s.repo.UpdateConnection(ctx, c)
		return run, nil
	}

	// Demo platform map — CRM DB is source of truth unless policy says otherwise.
	platformByKey := map[string]string{
		"cust-1": "Ahmed Al-Rashid",
		"cust-2": "Sara Khan",
	}
	applied, conflicts, details := domain.ApplySampleRows(
		c.ConflictPolicy, c.SourceOfTruth, platformByKey, sample, "id",
	)
	run.RowsApplied = applied
	run.Conflicts = conflicts
	if conflicts > 0 {
		run.Status = domain.RunConflicts
	} else {
		run.Status = domain.RunOK
	}
	summary, _ := json.Marshal(map[string]any{
		"message":            "Platform DB remains authoritative; file values applied only per conflict_policy.",
		"source_of_truth":    c.SourceOfTruth,
		"conflict_policy":    c.ConflictPolicy,
		"platform_authoritative": c.SourceOfTruth == domain.TruthPlatform || c.ConflictPolicy == domain.ConflictPreferPlatform,
		"resolutions":        details,
	})
	run.SummaryJSON = summary

	c.Status = domain.StatusConnected
	c.LastSyncAt = &finished
	c.LastError = ""
	c.UpdatedAt = finished
	_ = s.repo.UpdateRun(ctx, run)
	_ = s.repo.UpdateConnection(ctx, c)
	return run, nil
}

func (s *Service) ListRuns(ctx context.Context, branchID uuid.UUID, connectionID *uuid.UUID, limit int) ([]domain.Run, error) {
	return s.repo.ListRuns(ctx, branchID, connectionID, limit)
}
