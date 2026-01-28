package memory

import (
	"context"
	"time"

	"github.com/steveyegge/beads/internal/spec"
)

func (m *MemoryStorage) UpsertSpecRegistry(_ context.Context, specs []spec.SpecRegistryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range specs {
		m.specRegistry[entry.SpecID] = entry
	}
	return nil
}

func (m *MemoryStorage) ListSpecRegistry(_ context.Context) ([]spec.SpecRegistryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]spec.SpecRegistryEntry, 0, len(m.specRegistry))
	for _, entry := range m.specRegistry {
		results = append(results, entry)
	}
	return results, nil
}

func (m *MemoryStorage) GetSpecRegistry(_ context.Context, specID string) (*spec.SpecRegistryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.specRegistry[specID]
	if !ok {
		return nil, nil
	}
	return &entry, nil
}

func (m *MemoryStorage) ListSpecRegistryWithCounts(_ context.Context) ([]spec.SpecRegistryCount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]spec.SpecRegistryCount, 0, len(m.specRegistry))
	for _, entry := range m.specRegistry {
		count := 0
		changed := 0
		for _, issue := range m.issues {
			if issue.SpecID == entry.SpecID {
				count++
				if issue.SpecChangedAt != nil {
					changed++
				}
			}
		}
		results = append(results, spec.SpecRegistryCount{
			Spec:             entry,
			BeadCount:        count,
			ChangedBeadCount: changed,
		})
	}
	return results, nil
}

func (m *MemoryStorage) MarkSpecsMissing(_ context.Context, specIDs []string, missingAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range specIDs {
		entry, ok := m.specRegistry[id]
		if !ok {
			continue
		}
		entry.MissingAt = &missingAt
		m.specRegistry[id] = entry
	}
	return nil
}

func (m *MemoryStorage) ClearSpecsMissing(_ context.Context, specIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range specIDs {
		entry, ok := m.specRegistry[id]
		if !ok {
			continue
		}
		entry.MissingAt = nil
		m.specRegistry[id] = entry
	}
	return nil
}

func (m *MemoryStorage) MarkSpecChangedBySpecIDs(_ context.Context, specIDs []string, changedAt time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	specSet := make(map[string]struct{}, len(specIDs))
	for _, id := range specIDs {
		specSet[id] = struct{}{}
	}

	updated := 0
	for _, issue := range m.issues {
		if _, ok := specSet[issue.SpecID]; !ok {
			continue
		}
		issue.SpecChangedAt = &changedAt
		m.dirty[issue.ID] = true
		updated++
	}
	return updated, nil
}
