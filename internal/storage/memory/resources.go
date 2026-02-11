package memory

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// SaveResource upserts a resource and its tags
func (m *MemoryStorage) SaveResource(ctx context.Context, r *types.Resource) error {
	return fmt.Errorf("resource management not implemented for Memory backend yet")
}

// GetResource retrieves a resource by identifier
func (m *MemoryStorage) GetResource(ctx context.Context, identifier string) (*types.Resource, error) {
	return nil, fmt.Errorf("resource management not implemented for Memory backend yet")
}

// ListResources queries resources
func (m *MemoryStorage) ListResources(ctx context.Context, filter types.ResourceFilter) ([]*types.Resource, error) {
	return nil, fmt.Errorf("resource management not implemented for Memory backend yet")
}

// DeleteResource removes a resource
func (m *MemoryStorage) DeleteResource(ctx context.Context, identifier string) error {
	return fmt.Errorf("resource management not implemented for Memory backend yet")
}

// SyncResources bulk upserts resources and deactivates missing ones for a source
func (m *MemoryStorage) SyncResources(ctx context.Context, source string, resources []*types.Resource) error {
	return fmt.Errorf("resource management not implemented for Memory backend yet")
}
