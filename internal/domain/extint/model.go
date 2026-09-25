package extint

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Kind of future external system (T-204 / T-207).
type Kind string

const (
	KindAccounting     Kind = "accounting"
	KindGDS            Kind = "gds"
	KindPaymentGateway Kind = "payment_gateway"
)

func ValidKind(k Kind) bool {
	switch k {
	case KindAccounting, KindGDS, KindPaymentGateway:
		return true
	default:
		return false
	}
}

type Status string

const (
	StatusStub        Status = "stub"
	StatusConfigured  Status = "configured"
	StatusDisabled    Status = "disabled"
	StatusError       Status = "error"
)

type Health string

const (
	HealthUnknown  Health = "unknown"
	HealthOK       Health = "ok"
	HealthDegraded Health = "degraded"
	HealthDown     Health = "down"
)

// Integration is a per-branch placeholder / stub binding.
type Integration struct {
	ID            uuid.UUID
	BranchID      uuid.UUID
	Kind          Kind
	ProviderKey   string
	DisplayName   string
	Status        Status
	Health        Health
	ConfigJSON    json.RawMessage
	LastCheckedAt *time.Time
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (i *Integration) Normalize() error {
	i.ProviderKey = strings.ToLower(strings.TrimSpace(i.ProviderKey))
	i.DisplayName = strings.TrimSpace(i.DisplayName)
	if !ValidKind(i.Kind) {
		return shared.NewValidation("invalid kind")
	}
	if i.ProviderKey == "" {
		return shared.NewValidation("provider_key is required")
	}
	if i.DisplayName == "" {
		i.DisplayName = i.ProviderKey
	}
	if i.Status == "" {
		i.Status = StatusStub
	}
	if i.Health == "" {
		i.Health = HealthUnknown
	}
	if len(i.ConfigJSON) == 0 {
		i.ConfigJSON = json.RawMessage(`{}`)
	}
	return nil
}

// Adapter is the DIP port for external system stubs (no live network in MVP).
type Adapter interface {
	Kind() Kind
	ProviderKey() string
	DisplayName() string
	Probe(ctx context.Context, cfg json.RawMessage) (Health, string, error)
}

// CatalogEntry is a known stub that can be enabled per branch.
type CatalogEntry struct {
	Kind        Kind   `json:"kind"`
	ProviderKey string `json:"provider_key"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// Repository persistence port.
type Repository interface {
	List(ctx context.Context, branchID uuid.UUID) ([]Integration, error)
	Get(ctx context.Context, branchID, id uuid.UUID) (*Integration, error)
	Upsert(ctx context.Context, i *Integration) error
	Update(ctx context.Context, i *Integration) error
	Delete(ctx context.Context, branchID, id uuid.UUID) error
}
