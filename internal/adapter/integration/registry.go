package integration

import (
	"fmt"
	"sync"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
)

// Registry maps channel names to ChannelProvider implementations (DIP).
type Registry struct {
	mu   sync.RWMutex
	by   map[domain.Channel]domain.ChannelProvider
	list []domain.ChannelProvider
}

func NewRegistry(providers ...domain.ChannelProvider) *Registry {
	r := &Registry{by: make(map[domain.Channel]domain.ChannelProvider, len(providers))}
	for _, p := range providers {
		r.Register(p)
	}
	return r
}

func (r *Registry) Register(p domain.ChannelProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := p.Name()
	if _, exists := r.by[name]; exists {
		// Replace existing in list.
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

func (r *Registry) Get(channel domain.Channel) (domain.ChannelProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.by[channel]
	if !ok {
		return nil, fmt.Errorf("channel provider %q not registered", channel)
	}
	return p, nil
}

func (r *Registry) All() []domain.ChannelProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.ChannelProvider, len(r.list))
	copy(out, r.list)
	return out
}

var _ domain.Registry = (*Registry)(nil)
