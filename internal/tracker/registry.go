package tracker

import (
	"fmt"
	"sort"
	"sync"
)

// TrackerFactory creates a new IssueTracker instance.
type TrackerFactory func() IssueTracker

var (
	registryMu sync.RWMutex
	registry   = make(map[string]TrackerFactory)
)

// Register adds a tracker factory to the global registry.
// Typically called from an init() function in each tracker adapter package.
func Register(name string, factory TrackerFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// Get returns the factory for the named tracker, or nil if not registered.
func Get(name string) TrackerFactory {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[name]
}

// List returns the names of all registered trackers, sorted alphabetically.
func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewTracker creates a new instance of the named tracker.
// Returns an error if the tracker is not registered.
func NewTracker(name string) (IssueTracker, error) {
	factory := Get(name)
	if factory == nil {
		return nil, fmt.Errorf("tracker %q not registered; available: %v", name, List())
	}
	return factory(), nil
}
