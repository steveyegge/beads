package memory

import (
	"context"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func (m *MemoryStorage) resourceMatchesFilter(r *types.Resource, filter types.ResourceFilter) bool {
	if filter.Type != nil && r.Type != *filter.Type {
		return false
	}
	if filter.Source != nil && r.Source != *filter.Source {
		return false
	}
	if len(filter.Tags) > 0 {
		tagSet := make(map[string]bool)
		for _, t := range r.Tags {
			tagSet[t] = true
		}
		for _, tag := range filter.Tags {
			if !tagSet[tag] {
				return false
			}
		}
	}
	return true
}

func (m *MemoryStorage) SaveResource(ctx context.Context, r *types.Resource) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now

	_, ok := m.resources[r.Identifier]
	if !ok {
		m.resourceOrder = append(m.resourceOrder, r.Identifier)
	}
	m.resources[r.Identifier] = r
	_ = ok
	return nil
}

func (m *MemoryStorage) GetResource(ctx context.Context, identifier string) (*types.Resource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if r, ok := m.resources[identifier]; ok {
		return r, nil
	}
	return nil, nil
}

func (m *MemoryStorage) ListResources(ctx context.Context, filter types.ResourceFilter) ([]*types.Resource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*types.Resource
	for _, id := range m.resourceOrder {
		r, ok := m.resources[id]
		if !ok {
			continue
		}
		if !r.IsActive {
			continue
		}
		if m.resourceMatchesFilter(r, filter) {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *MemoryStorage) DeleteResource(ctx context.Context, identifier string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r, ok := m.resources[identifier]; ok {
		r.IsActive = false
	}
	return nil
}

func (m *MemoryStorage) SyncResources(ctx context.Context, source string, resources []*types.Resource) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	seen := make(map[string]bool)
	for _, r := range resources {
		seen[r.Identifier] = true
		now := time.Now().UTC()
		if r.CreatedAt.IsZero() {
			r.CreatedAt = now
		}
		r.UpdatedAt = now

		_, ok := m.resources[r.Identifier]
		if !ok {
			m.resourceOrder = append(m.resourceOrder, r.Identifier)
		}
		m.resources[r.Identifier] = r
		_ = ok
	}

	for _, id := range m.resourceOrder {
		r, ok := m.resources[id]
		if !ok {
			continue
		}
		if r.Source == source && !seen[id] {
			r.IsActive = false
		}
	}
	return nil
}
