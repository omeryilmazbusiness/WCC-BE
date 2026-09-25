package extint

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/extint"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// AdapterRegistry resolves stub probes (DIP).
type AdapterRegistry interface {
	Get(kind domain.Kind, providerKey string) (domain.Adapter, bool)
	Probe(ctx context.Context, kind domain.Kind, providerKey string, cfg json.RawMessage) (domain.Health, string, error)
}

type EnableInput struct {
	BranchID    uuid.UUID       `json:"-"`
	Kind        domain.Kind     `json:"kind"`
	ProviderKey string          `json:"provider_key"`
	DisplayName string          `json:"display_name"`
	ConfigJSON  json.RawMessage `json:"config_json"`
}

type PatchInput struct {
	DisplayName *string          `json:"display_name"`
	Status      *domain.Status   `json:"status"`
	ConfigJSON  json.RawMessage  `json:"config_json"`
	Disabled    *bool            `json:"disabled"`
}

type Service struct {
	repo     domain.Repository
	registry AdapterRegistry
	catalog  []domain.CatalogEntry
}

func NewService(repo domain.Repository, registry AdapterRegistry, catalog []domain.CatalogEntry) *Service {
	return &Service{repo: repo, registry: registry, catalog: catalog}
}

func (s *Service) Catalog() []domain.CatalogEntry {
	return s.catalog
}

func (s *Service) List(ctx context.Context, branchID uuid.UUID) ([]domain.Integration, error) {
	return s.repo.List(ctx, branchID)
}

func (s *Service) Get(ctx context.Context, branchID, id uuid.UUID) (*domain.Integration, error) {
	i, err := s.repo.Get(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("external_integration")
	}
	return i, nil
}

func (s *Service) Enable(ctx context.Context, in EnableInput) (*domain.Integration, error) {
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	if !domain.ValidKind(in.Kind) {
		return nil, shared.NewValidation("invalid kind")
	}
	if _, ok := s.registry.Get(in.Kind, in.ProviderKey); !ok {
		// still allow known catalog keys
		found := false
		for _, e := range s.catalog {
			if e.Kind == in.Kind && e.ProviderKey == in.ProviderKey {
				found = true
				if in.DisplayName == "" {
					in.DisplayName = e.DisplayName
				}
				break
			}
		}
		if !found {
			return nil, shared.NewValidation("unknown provider_key for kind")
		}
	} else if in.DisplayName == "" {
		if a, ok := s.registry.Get(in.Kind, in.ProviderKey); ok {
			in.DisplayName = a.DisplayName()
		}
	}
	now := time.Now().UTC()
	i := &domain.Integration{
		ID: uuid.New(), BranchID: in.BranchID, Kind: in.Kind,
		ProviderKey: in.ProviderKey, DisplayName: in.DisplayName,
		Status: domain.StatusStub, Health: domain.HealthUnknown,
		ConfigJSON: in.ConfigJSON, CreatedAt: now, UpdatedAt: now,
	}
	if err := i.Normalize(); err != nil {
		return nil, err
	}
	if err := s.repo.Upsert(ctx, i); err != nil {
		return nil, err
	}
	return i, nil
}

func (s *Service) Disable(ctx context.Context, branchID, id uuid.UUID) (*domain.Integration, error) {
	i, err := s.repo.Get(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("external_integration")
	}
	i.Status = domain.StatusDisabled
	i.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, i); err != nil {
		return nil, err
	}
	return i, nil
}

func (s *Service) Patch(ctx context.Context, branchID, id uuid.UUID, in PatchInput) (*domain.Integration, error) {
	i, err := s.repo.Get(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("external_integration")
	}
	if in.Disabled != nil && *in.Disabled {
		i.Status = domain.StatusDisabled
	}
	if in.DisplayName != nil {
		i.DisplayName = *in.DisplayName
	}
	if in.Status != nil {
		i.Status = *in.Status
	}
	if len(in.ConfigJSON) > 0 {
		i.ConfigJSON = in.ConfigJSON
	}
	if err := i.Normalize(); err != nil {
		return nil, err
	}
	i.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, i); err != nil {
		return nil, err
	}
	return i, nil
}

func (s *Service) Probe(ctx context.Context, branchID, id uuid.UUID) (*domain.Integration, error) {
	i, err := s.repo.Get(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("external_integration")
	}
	now := time.Now().UTC()
	health, msg, probeErr := s.registry.Probe(ctx, i.Kind, i.ProviderKey, i.ConfigJSON)
	i.LastCheckedAt = &now
	i.UpdatedAt = now
	if probeErr != nil {
		i.Health = domain.HealthDown
		i.LastError = msg
		i.Status = domain.StatusError
	} else {
		i.Health = health
		i.LastError = ""
		if i.Status == domain.StatusStub || i.Status == domain.StatusError {
			i.Status = domain.StatusConfigured
		}
		_ = msg
	}
	if err := s.repo.Update(ctx, i); err != nil {
		return nil, err
	}
	return i, nil
}

func (s *Service) Delete(ctx context.Context, branchID, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, branchID, id); err != nil {
		return shared.NewNotFound("external_integration")
	}
	return nil
}
