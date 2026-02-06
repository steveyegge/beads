package memory

import (
	"context"
	"sort"
	"strings"
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

func (m *MemoryStorage) UpdateSpecRegistry(_ context.Context, specID string, updates spec.SpecRegistryUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.specRegistry[specID]
	if !ok {
		return nil
	}
	if updates.Lifecycle != nil {
		entry.Lifecycle = *updates.Lifecycle
	}
	if updates.CompletedAt != nil {
		entry.CompletedAt = updates.CompletedAt
	}
	if updates.Summary != nil {
		entry.Summary = *updates.Summary
	}
	if updates.SummaryTokens != nil {
		entry.SummaryTokens = *updates.SummaryTokens
	}
	if updates.ArchivedAt != nil {
		entry.ArchivedAt = updates.ArchivedAt
	}
	m.specRegistry[specID] = entry
	return nil
}

func (m *MemoryStorage) MoveSpecRegistry(_ context.Context, fromSpecID, toSpecID, toPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(fromSpecID) == "" || strings.TrimSpace(toSpecID) == "" {
		return nil
	}
	if fromSpecID == toSpecID {
		return nil
	}
	if _, exists := m.specRegistry[toSpecID]; exists {
		return nil
	}
	entry, ok := m.specRegistry[fromSpecID]
	if !ok {
		return nil
	}
	if strings.TrimSpace(toPath) == "" {
		toPath = toSpecID
	}
	entry.SpecID = toSpecID
	entry.Path = toPath
	delete(m.specRegistry, fromSpecID)
	m.specRegistry[toSpecID] = entry

	if events, ok := m.specScanEvents[fromSpecID]; ok {
		delete(m.specScanEvents, fromSpecID)
		m.specScanEvents[toSpecID] = events
	}
	return nil
}

func (m *MemoryStorage) DeleteSpecRegistryByIDs(_ context.Context, specIDs []string) (int, error) {
	if len(specIDs) == 0 {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	deleted := 0
	for _, id := range specIDs {
		if _, ok := m.specRegistry[id]; ok {
			delete(m.specRegistry, id)
			delete(m.specScanEvents, id)
			deleted++
		}
	}
	return deleted, nil
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

func (m *MemoryStorage) AddSpecScanEvents(_ context.Context, events []spec.SpecScanEvent) error {
	if len(events) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, event := range events {
		byTime, ok := m.specScanEvents[event.SpecID]
		if !ok {
			byTime = make(map[int64]spec.SpecScanEvent)
			m.specScanEvents[event.SpecID] = byTime
		}
		key := event.ScannedAt.UnixNano()
		byTime[key] = event
	}
	return nil
}

func (m *MemoryStorage) ListSpecScanEvents(_ context.Context, specID string, since time.Time) ([]spec.SpecScanEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	byTime, ok := m.specScanEvents[specID]
	if !ok {
		return nil, nil
	}
	results := make([]spec.SpecScanEvent, 0, len(byTime))
	for _, event := range byTime {
		if !since.IsZero() && event.ScannedAt.Before(since) {
			continue
		}
		results = append(results, event)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].ScannedAt.Before(results[j].ScannedAt)
	})
	return results, nil
}
