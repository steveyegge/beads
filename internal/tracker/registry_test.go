package tracker

import (
	"testing"
)

func TestRegistry(t *testing.T) {
	// Clean registry for test isolation
	registryMu.Lock()
	savedRegistry := registry
	registry = make(map[string]TrackerFactory)
	registryMu.Unlock()
	defer func() {
		registryMu.Lock()
		registry = savedRegistry
		registryMu.Unlock()
	}()

	t.Run("empty registry", func(t *testing.T) {
		if got := List(); len(got) != 0 {
			t.Errorf("List() = %v, want empty", got)
		}
		if got := Get("linear"); got != nil {
			t.Error("Get() returned non-nil for unregistered tracker")
		}
		_, err := NewTracker("linear")
		if err == nil {
			t.Error("NewTracker() should fail for unregistered tracker")
		}
	})

	t.Run("register and retrieve", func(t *testing.T) {
		Register("mock", func() IssueTracker { return nil })

		if got := Get("mock"); got == nil {
			t.Error("Get() returned nil for registered tracker")
		}
		if got := Get("missing"); got != nil {
			t.Error("Get() returned non-nil for unregistered tracker")
		}
	})

	t.Run("list returns sorted names", func(t *testing.T) {
		Register("zebra", func() IssueTracker { return nil })
		Register("alpha", func() IssueTracker { return nil })

		got := List()
		if len(got) < 2 {
			t.Fatalf("List() returned %d items, want at least 2", len(got))
		}
		// Should be sorted
		for i := 1; i < len(got); i++ {
			if got[i] < got[i-1] {
				t.Errorf("List() not sorted: %v", got)
				break
			}
		}
	})

	t.Run("NewTracker returns new instance", func(t *testing.T) {
		callCount := 0
		Register("counter", func() IssueTracker {
			callCount++
			return nil
		})

		_, _ = NewTracker("counter")
		_, _ = NewTracker("counter")
		if callCount != 2 {
			t.Errorf("factory called %d times, want 2", callCount)
		}
	})
}
