package extint

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/secretcfg"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/extint"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
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
	DisplayName *string         `json:"display_name"`
	Status      *domain.Status  `json:"status"`
	ConfigJSON  json.RawMessage `json:"config_json"`
	Disabled    *bool           `json:"disabled"`
}

type Service struct {
	repo     domain.Repository
	registry AdapterRegistry
	catalog  []domain.CatalogEntry
	vault    secretcfg.Vault
	outbox   events.Outbox
}

func (s *Service) SetOutbox(o events.Outbox) { s.outbox = o }

func NewService(repo domain.Repository, registry AdapterRegistry, catalog []domain.CatalogEntry, secrets crypto.SecretSealer) *Service {
	return &Service{repo: repo, registry: registry, catalog: catalog, vault: secretcfg.NewVault(secrets)}
}

func binding(i *domain.Integration) crypto.Binding {
	return crypto.Binding{Table: domain.SecretsTable, RowID: i.ID, BranchID: i.BranchID}
}

func (s *Service) config(i *domain.Integration) (secretcfg.Config, error) {
	return s.vault.Load(i.ConfigJSON, i.SecretsEnc, binding(i))
}

// store applies incoming config to i and seals its secrets for i's row.
func (s *Service) store(i *domain.Integration, current secretcfg.Config, incoming json.RawMessage) error {
	cfg, err := s.vault.Apply(current, incoming)
	if err != nil {
		return err
	}
	sealed, err := s.vault.Seal(cfg, binding(i))
	if err != nil {
		return err
	}
	i.ConfigJSON, i.SecretsEnc = cfg.Public, sealed
	return nil
}

// redact strips credentials from i for API responses, leaving hints.
func (s *Service) redact(i *domain.Integration) error {
	cfg, err := s.config(i)
	if err != nil {
		return err
	}
	i.ConfigJSON, i.SecretsEnc, i.SecretHints = cfg.Public, "", cfg.Hints()
	return nil
}

func (s *Service) Catalog() []domain.CatalogEntry {
	return s.catalog
}

func (s *Service) List(ctx context.Context, branchID uuid.UUID) ([]domain.Integration, error) {
	items, err := s.repo.List(ctx, branchID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if err := s.redact(&items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, branchID, id uuid.UUID) (*domain.Integration, error) {
	i, err := s.repo.Get(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("external_integration")
	}
	return i, s.redact(i)
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
		CreatedAt: now, UpdatedAt: now,
	}
	if err := i.Normalize(); err != nil {
		return nil, err
	}
	current, err := s.existingConfig(ctx, i)
	if err != nil {
		return nil, err
	}
	if err := s.store(i, current, in.ConfigJSON); err != nil {
		return nil, err
	}
	if err := s.repo.Upsert(ctx, i); err != nil {
		return nil, err
	}
	return i, s.redact(i)
}

// existingConfig reuses the row (and its secrets) when a kind/provider is
// enabled again: sealed secrets are bound to the row id.
func (s *Service) existingConfig(ctx context.Context, i *domain.Integration) (secretcfg.Config, error) {
	items, err := s.repo.List(ctx, i.BranchID)
	if err != nil {
		return secretcfg.Config{}, err
	}
	for k := range items {
		if items[k].Kind == i.Kind && items[k].ProviderKey == i.ProviderKey {
			i.ID, i.CreatedAt = items[k].ID, items[k].CreatedAt
			return s.config(&items[k])
		}
	}
	return secretcfg.Config{}, nil
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
	return i, s.redact(i)
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
		current, err := s.config(i)
		if err != nil {
			return nil, err
		}
		if err := s.store(i, current, in.ConfigJSON); err != nil {
			return nil, err
		}
	}
	if err := i.Normalize(); err != nil {
		return nil, err
	}
	i.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, i); err != nil {
		return nil, err
	}
	return i, s.redact(i)
}

func (s *Service) Probe(ctx context.Context, branchID, id uuid.UUID) (*domain.Integration, error) {
	i, err := s.repo.Get(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("external_integration")
	}
	cfg, err := s.config(i)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	health, msg, probeErr := s.registry.Probe(ctx, i.Kind, i.ProviderKey, cfg.Merged())
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
	}
	if err := s.repo.Update(ctx, i); err != nil {
		return nil, err
	}
	if probeErr != nil {
		intID := i.ID
		if err := events.Record(ctx, s.outbox, events.Event{Name: events.IntegrationFailed, Payload: events.IntegrationFailedPayload{
			BranchID: i.BranchID, Source: "external", Provider: i.ProviderKey, AccountID: &intID, Error: msg, At: now,
		}}); err != nil {
			return nil, err
		}
	}
	return i, s.redact(i)
}

func (s *Service) Delete(ctx context.Context, branchID, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, branchID, id); err != nil {
		return shared.NewNotFound("external_integration")
	}
	return nil
}
