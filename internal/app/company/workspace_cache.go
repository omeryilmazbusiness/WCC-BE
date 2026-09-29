package company

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
)

// WorkspaceSource loads a tenant from storage.
type WorkspaceSource interface {
	WorkspaceOf(ctx context.Context, branchID uuid.UUID) (*domain.Workspace, error)
}

// WorkspaceCache memoizes tenant lookups made on every authenticated request.
// Changes made through this process invalidate at once; other API instances
// converge within ttl.
type WorkspaceCache struct {
	src WorkspaceSource
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[uuid.UUID]cacheEntry
}

type cacheEntry struct {
	ws      *domain.Workspace
	expires time.Time
}

func NewWorkspaceCache(src WorkspaceSource, ttl time.Duration) *WorkspaceCache {
	return &WorkspaceCache{src: src, ttl: ttl, now: time.Now, entries: map[uuid.UUID]cacheEntry{}}
}

func (c *WorkspaceCache) Workspace(ctx context.Context, branchID uuid.UUID) (*domain.Workspace, error) {
	now := c.now()
	c.mu.Lock()
	if e, ok := c.entries[branchID]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		return e.ws, nil
	}
	c.mu.Unlock()

	ws, err := c.src.WorkspaceOf(ctx, branchID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.entries[branchID] = cacheEntry{ws: ws, expires: now.Add(c.ttl)}
	c.mu.Unlock()
	return ws, nil
}

// Invalidate drops every cached branch of a company.
func (c *WorkspaceCache) Invalidate(companyID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, e := range c.entries {
		if e.ws.Company.ID == companyID {
			delete(c.entries, id)
		}
	}
}
