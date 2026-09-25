package filesync

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/filesync"
)

// Registry maps Provider → CloudFileProvider (OCP).
type Registry struct {
	mu   sync.RWMutex
	by   map[domain.Provider]domain.CloudFileProvider
	list []domain.CloudFileProvider
}

func NewRegistry(providers ...domain.CloudFileProvider) *Registry {
	r := &Registry{by: make(map[domain.Provider]domain.CloudFileProvider, len(providers))}
	for _, p := range providers {
		r.Register(p)
	}
	return r
}

func (r *Registry) Register(p domain.CloudFileProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := p.Name()
	if _, exists := r.by[name]; exists {
		for i, x := range r.list {
			if x.Name() == name {
				r.list[i] = p
				break
			}
		}
	} else {
		r.list = append(r.list, p)
	}
	r.by[name] = p
}

func (r *Registry) Get(p domain.Provider) (domain.CloudFileProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.by[p]
	return v, ok
}

func (r *Registry) All() []domain.CloudFileProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.CloudFileProvider, len(r.list))
	copy(out, r.list)
	return out
}

// --- OneDrive stub ---

type OneDrive struct{}

func NewOneDrive() *OneDrive { return &OneDrive{} }

func (o *OneDrive) Name() domain.Provider { return domain.ProviderOneDrive }

func (o *OneDrive) Probe(ctx context.Context, remotePath string, cfg json.RawMessage) (domain.ConnStatus, string, error) {
	_ = ctx
	_ = cfg
	if remotePath == "" {
		return domain.StatusError, "remote_path required", fmt.Errorf("remote_path required")
	}
	return domain.StatusConnected, "onedrive stub: path reachable (simulated)", nil
}

func (o *OneDrive) PullSample(ctx context.Context, remotePath string, cfg json.RawMessage) (int, []map[string]string, error) {
	_ = ctx
	_ = cfg
	_ = remotePath
	rows := []map[string]string{
		{"id": "cust-1", "value": "Ahmed Al-Rashid"},
		{"id": "cust-2", "value": "Sara Khan"},
		{"id": "cust-3", "value": "New From File"},
	}
	return len(rows), rows, nil
}

// --- SharePoint stub ---

type SharePoint struct{}

func NewSharePoint() *SharePoint { return &SharePoint{} }

func (s *SharePoint) Name() domain.Provider { return domain.ProviderSharePoint }

func (s *SharePoint) Probe(ctx context.Context, remotePath string, cfg json.RawMessage) (domain.ConnStatus, string, error) {
	_ = ctx
	_ = cfg
	if remotePath == "" {
		return domain.StatusError, "remote_path required", fmt.Errorf("remote_path required")
	}
	return domain.StatusConnected, "sharepoint stub: library reachable (simulated)", nil
}

func (s *SharePoint) PullSample(ctx context.Context, remotePath string, cfg json.RawMessage) (int, []map[string]string, error) {
	_ = ctx
	_ = cfg
	_ = remotePath
	rows := []map[string]string{
		{"id": "cust-1", "value": "Ahmed Updated"},
		{"id": "cust-4", "value": "SharePoint Only"},
	}
	return len(rows), rows, nil
}

var (
	_ domain.CloudFileProvider = (*OneDrive)(nil)
	_ domain.CloudFileProvider = (*SharePoint)(nil)
)
