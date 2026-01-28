package spec

import (
	"context"
	"testing"
	"time"
)

// mockStore implements SpecRegistryStore for testing
type mockStore struct {
	entries       []SpecRegistryEntry
	markedChanged []string
	markedMissing []string
	clearedIDs    []string
}

func (m *mockStore) ListSpecRegistry(ctx context.Context) ([]SpecRegistryEntry, error) {
	return m.entries, nil
}

func (m *mockStore) GetSpecRegistry(ctx context.Context, specID string) (*SpecRegistryEntry, error) {
	for _, e := range m.entries {
		if e.SpecID == specID {
			return &e, nil
		}
	}
	return nil, nil
}

func (m *mockStore) ListSpecRegistryWithCounts(ctx context.Context) ([]SpecRegistryCount, error) {
	var results []SpecRegistryCount
	for _, e := range m.entries {
		results = append(results, SpecRegistryCount{Spec: e})
	}
	return results, nil
}

func (m *mockStore) UpsertSpecRegistry(ctx context.Context, specs []SpecRegistryEntry) error {
	// Replace existing entries with same ID, add new ones
	byID := make(map[string]int)
	for i, e := range m.entries {
		byID[e.SpecID] = i
	}
	for _, s := range specs {
		if idx, ok := byID[s.SpecID]; ok {
			m.entries[idx] = s
		} else {
			m.entries = append(m.entries, s)
		}
	}
	return nil
}

func (m *mockStore) MarkSpecsMissing(ctx context.Context, specIDs []string, missingAt time.Time) error {
	m.markedMissing = append(m.markedMissing, specIDs...)
	return nil
}

func (m *mockStore) ClearSpecsMissing(ctx context.Context, specIDs []string) error {
	m.clearedIDs = append(m.clearedIDs, specIDs...)
	return nil
}

func (m *mockStore) MarkSpecChangedBySpecIDs(ctx context.Context, specIDs []string, changedAt time.Time) (int, error) {
	m.markedChanged = append(m.markedChanged, specIDs...)
	return len(specIDs), nil
}

func TestUpdateRegistry_Add(t *testing.T) {
	store := &mockStore{}
	now := time.Now().UTC().Truncate(time.Second)

	scanned := []ScannedSpec{
		{SpecID: "specs/login.md", Title: "Login", SHA256: "abc123", Mtime: now},
		{SpecID: "specs/signup.md", Title: "Signup", SHA256: "def456", Mtime: now},
	}

	result, err := UpdateRegistry(context.Background(), store, scanned, now)
	if err != nil {
		t.Fatalf("UpdateRegistry() error: %v", err)
	}

	if result.Added != 2 {
		t.Errorf("Added = %d, want 2", result.Added)
	}
	if result.Updated != 0 {
		t.Errorf("Updated = %d, want 0", result.Updated)
	}
	if result.Scanned != 2 {
		t.Errorf("Scanned = %d, want 2", result.Scanned)
	}
	if len(result.ChangedSpecIDs) != 0 {
		t.Errorf("ChangedSpecIDs = %v, want empty", result.ChangedSpecIDs)
	}
}

func TestUpdateRegistry_Change(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	earlier := now.Add(-time.Hour)

	store := &mockStore{
		entries: []SpecRegistryEntry{
			{SpecID: "specs/login.md", Title: "Login", SHA256: "old-hash", DiscoveredAt: earlier, LastScannedAt: earlier},
		},
	}

	scanned := []ScannedSpec{
		{SpecID: "specs/login.md", Title: "Login v2", SHA256: "new-hash", Mtime: now},
	}

	result, err := UpdateRegistry(context.Background(), store, scanned, now)
	if err != nil {
		t.Fatalf("UpdateRegistry() error: %v", err)
	}

	if result.Added != 0 {
		t.Errorf("Added = %d, want 0", result.Added)
	}
	if result.Updated != 1 {
		t.Errorf("Updated = %d, want 1", result.Updated)
	}
	if len(result.ChangedSpecIDs) != 1 || result.ChangedSpecIDs[0] != "specs/login.md" {
		t.Errorf("ChangedSpecIDs = %v, want [specs/login.md]", result.ChangedSpecIDs)
	}
	if len(store.markedChanged) != 1 {
		t.Errorf("markedChanged = %v, want 1 item", store.markedChanged)
	}
}

func TestUpdateRegistry_Missing(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	earlier := now.Add(-time.Hour)

	store := &mockStore{
		entries: []SpecRegistryEntry{
			{SpecID: "specs/login.md", SHA256: "abc", DiscoveredAt: earlier},
			{SpecID: "specs/deleted.md", SHA256: "xyz", DiscoveredAt: earlier},
		},
	}

	// Only login.md is still on disk
	scanned := []ScannedSpec{
		{SpecID: "specs/login.md", SHA256: "abc", Mtime: now},
	}

	result, err := UpdateRegistry(context.Background(), store, scanned, now)
	if err != nil {
		t.Fatalf("UpdateRegistry() error: %v", err)
	}

	if result.Missing != 1 {
		t.Errorf("Missing = %d, want 1", result.Missing)
	}
	if len(store.markedMissing) != 1 || store.markedMissing[0] != "specs/deleted.md" {
		t.Errorf("markedMissing = %v, want [specs/deleted.md]", store.markedMissing)
	}
}

func TestUpdateRegistry_Unchanged(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	earlier := now.Add(-time.Hour)

	store := &mockStore{
		entries: []SpecRegistryEntry{
			{SpecID: "specs/login.md", SHA256: "same-hash", DiscoveredAt: earlier},
		},
	}

	scanned := []ScannedSpec{
		{SpecID: "specs/login.md", SHA256: "same-hash", Mtime: now},
	}

	result, err := UpdateRegistry(context.Background(), store, scanned, now)
	if err != nil {
		t.Fatalf("UpdateRegistry() error: %v", err)
	}

	if result.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", result.Unchanged)
	}
	if result.Updated != 0 {
		t.Errorf("Updated = %d, want 0", result.Updated)
	}
	if len(result.ChangedSpecIDs) != 0 {
		t.Errorf("ChangedSpecIDs = %v, want empty", result.ChangedSpecIDs)
	}
}
