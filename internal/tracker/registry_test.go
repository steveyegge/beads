package tracker

import (
	"context"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// mockTracker is a minimal IssueTracker implementation for testing.
type mockTracker struct {
	name        string
	displayName string
	externalRef string
}

func (m *mockTracker) Name() string                                    { return m.name }
func (m *mockTracker) DisplayName() string                             { return m.displayName }
func (m *mockTracker) ConfigPrefix() string                            { return m.name }
func (m *mockTracker) Init(_ context.Context, _ *Config) error         { return nil }
func (m *mockTracker) Validate() error                                 { return nil }
func (m *mockTracker) Close() error                                    { return nil }
func (m *mockTracker) FetchIssues(_ context.Context, _ FetchOptions) ([]TrackerIssue, error) {
	return nil, nil
}
func (m *mockTracker) FetchIssue(_ context.Context, _ string) (*TrackerIssue, error) {
	return nil, nil
}
func (m *mockTracker) CreateIssue(_ context.Context, _ *types.Issue) (*TrackerIssue, error) {
	return nil, nil
}
func (m *mockTracker) UpdateIssue(_ context.Context, _ string, _ *types.Issue) (*TrackerIssue, error) {
	return nil, nil
}
func (m *mockTracker) FieldMapper() FieldMapper   { return nil }
func (m *mockTracker) IsExternalRef(ref string) bool {
	return len(ref) >= len(m.externalRef) && ref[:len(m.externalRef)] == m.externalRef
}
func (m *mockTracker) ExtractIdentifier(_ string) string { return "" }
func (m *mockTracker) BuildExternalRef(_ *TrackerIssue) string { return "" }
func (m *mockTracker) CanonicalizeRef(ref string) string { return ref }

func TestRegistry(t *testing.T) {
	// Create a new registry (not the global one)
	r := &Registry{trackers: make(map[string]TrackerFactory)}

	// Test registration
	r.Register("mock1", func() IssueTracker {
		return &mockTracker{name: "mock1", displayName: "Mock 1", externalRef: "mock1://"}
	})
	r.Register("mock2", func() IssueTracker {
		return &mockTracker{name: "mock2", displayName: "Mock 2", externalRef: "mock2://"}
	})

	// Test Get
	factory := r.Get("mock1")
	if factory == nil {
		t.Error("Get(\"mock1\") returned nil")
	}

	factory = r.Get("nonexistent")
	if factory != nil {
		t.Error("Get(\"nonexistent\") should return nil")
	}

	// Test List
	names := r.List()
	if len(names) != 2 {
		t.Errorf("List() returned %d items, want 2", len(names))
	}
	if names[0] != "mock1" || names[1] != "mock2" {
		t.Errorf("List() = %v, want [mock1, mock2]", names)
	}

	// Test NewTracker
	tracker, err := r.NewTracker("mock1")
	if err != nil {
		t.Errorf("NewTracker(\"mock1\") failed: %v", err)
	}
	if tracker.Name() != "mock1" {
		t.Errorf("tracker.Name() = %s, want mock1", tracker.Name())
	}

	_, err = r.NewTracker("nonexistent")
	if err == nil {
		t.Error("NewTracker(\"nonexistent\") should return error")
	}

	// Test IsRegistered
	if !r.IsRegistered("mock1") {
		t.Error("IsRegistered(\"mock1\") should return true")
	}
	if r.IsRegistered("nonexistent") {
		t.Error("IsRegistered(\"nonexistent\") should return false")
	}

	// Test FindTrackerForRef
	name, ok := r.FindTrackerForRef("mock1://test")
	if !ok || name != "mock1" {
		t.Errorf("FindTrackerForRef(\"mock1://test\") = (%s, %v), want (mock1, true)", name, ok)
	}

	name, ok = r.FindTrackerForRef("unknown://test")
	if ok {
		t.Errorf("FindTrackerForRef(\"unknown://test\") should return false")
	}

	// Test Clear
	r.Clear()
	if len(r.List()) != 0 {
		t.Error("Clear() should remove all trackers")
	}
}
