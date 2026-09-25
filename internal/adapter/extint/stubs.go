package extint

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/extint"
)

// Catalog returns known stub providers that can be enabled per branch.
func Catalog() []domain.CatalogEntry {
	return []domain.CatalogEntry{
		{Kind: domain.KindAccounting, ProviderKey: "xero", DisplayName: "Xero", Description: "Accounting stub — chart of accounts sync (future)"},
		{Kind: domain.KindAccounting, ProviderKey: "quickbooks", DisplayName: "QuickBooks", Description: "Accounting stub — invoice export (future)"},
		{Kind: domain.KindGDS, ProviderKey: "amadeus", DisplayName: "Amadeus", Description: "GDS stub — PNR lookup (future)"},
		{Kind: domain.KindGDS, ProviderKey: "sabre", DisplayName: "Sabre", Description: "GDS stub — availability (future)"},
		{Kind: domain.KindPaymentGateway, ProviderKey: "stripe", DisplayName: "Stripe", Description: "Payment gateway stub — card capture (future)"},
		{Kind: domain.KindPaymentGateway, ProviderKey: "moyasar", DisplayName: "Moyasar", Description: "Payment gateway stub — SAR checkout (future)"},
	}
}

// Registry maps kind+provider_key → Adapter.
type Registry struct {
	mu sync.RWMutex
	by map[string]domain.Adapter
}

func NewRegistry(adapters ...domain.Adapter) *Registry {
	r := &Registry{by: make(map[string]domain.Adapter, len(adapters))}
	for _, a := range adapters {
		r.Register(a)
	}
	return r
}

func DefaultRegistry() *Registry {
	return NewRegistry(
		NewStub(domain.KindAccounting, "xero", "Xero"),
		NewStub(domain.KindAccounting, "quickbooks", "QuickBooks"),
		NewStub(domain.KindGDS, "amadeus", "Amadeus"),
		NewStub(domain.KindGDS, "sabre", "Sabre"),
		NewStub(domain.KindPaymentGateway, "stripe", "Stripe"),
		NewStub(domain.KindPaymentGateway, "moyasar", "Moyasar"),
	)
}

func key(kind domain.Kind, providerKey string) string {
	return string(kind) + ":" + providerKey
}

func (r *Registry) Register(a domain.Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.by[key(a.Kind(), a.ProviderKey())] = a
}

func (r *Registry) Get(kind domain.Kind, providerKey string) (domain.Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.by[key(kind, providerKey)]
	return a, ok
}

// Probe runs the stub adapter health check.
func (r *Registry) Probe(ctx context.Context, kind domain.Kind, providerKey string, cfg json.RawMessage) (domain.Health, string, error) {
	a, ok := r.Get(kind, providerKey)
	if !ok {
		return domain.HealthUnknown, "adapter not registered", fmt.Errorf("adapter %s/%s not registered", kind, providerKey)
	}
	return a.Probe(ctx, cfg)
}

// Stub is a no-network external system placeholder.
type Stub struct {
	kind        domain.Kind
	providerKey string
	displayName string
}

func NewStub(kind domain.Kind, providerKey, displayName string) *Stub {
	return &Stub{kind: kind, providerKey: providerKey, displayName: displayName}
}

func (s *Stub) Kind() domain.Kind        { return s.kind }
func (s *Stub) ProviderKey() string      { return s.providerKey }
func (s *Stub) DisplayName() string      { return s.displayName }

func (s *Stub) Probe(ctx context.Context, cfg json.RawMessage) (domain.Health, string, error) {
	_ = ctx
	_ = cfg
	return domain.HealthOK, s.displayName + " stub: probe ok (no live network)", nil
}

var _ domain.Adapter = (*Stub)(nil)
