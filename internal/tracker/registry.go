package tracker

import (
	"fmt"
	"sort"
	"sync"
)

// TrackerFactory is a function that creates a new IssueTracker instance.
type TrackerFactory func() IssueTracker

// Registry manages registered issue tracker plugins.
// Tracker plugins register themselves at init time, and the registry
// provides access to them by name.
type Registry struct {
	mu       sync.RWMutex
	trackers map[string]TrackerFactory
}

// globalRegistry is the default registry used by Register and Get.
var globalRegistry = &Registry{
	trackers: make(map[string]TrackerFactory),
}

// Register adds a tracker factory to the global registry.
// This is typically called from tracker plugin init() functions.
// The name should be lowercase (e.g., "linear", "jira", "azuredevops").
func Register(name string, factory TrackerFactory) {
	globalRegistry.Register(name, factory)
}

// Get retrieves a tracker factory from the global registry.
// Returns nil if no tracker with that name is registered.
func Get(name string) TrackerFactory {
	return globalRegistry.Get(name)
}

// List returns the names of all registered trackers.
func List() []string {
	return globalRegistry.List()
}

// NewTracker creates a new instance of the named tracker.
// Returns an error if no tracker with that name is registered.
func NewTracker(name string) (IssueTracker, error) {
	return globalRegistry.NewTracker(name)
}

// Register adds a tracker factory to this registry.
func (r *Registry) Register(name string, factory TrackerFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trackers[name] = factory
}

// Get retrieves a tracker factory from this registry.
func (r *Registry) Get(name string) TrackerFactory {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.trackers[name]
}

// List returns the names of all registered trackers, sorted alphabetically.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.trackers))
	for name := range r.trackers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewTracker creates a new instance of the named tracker.
func (r *Registry) NewTracker(name string) (IssueTracker, error) {
	factory := r.Get(name)
	if factory == nil {
		available := r.List()
		return nil, fmt.Errorf("unknown tracker %q (available: %v)", name, available)
	}
	return factory(), nil
}

// IsRegistered checks if a tracker with the given name is registered.
func (r *Registry) IsRegistered(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.trackers[name]
	return ok
}

// Clear removes all registered trackers. Used primarily for testing.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trackers = make(map[string]TrackerFactory)
}

// FindTrackerForRef attempts to find a registered tracker that recognizes
// the given external_ref URL. Returns the tracker name and true if found,
// or empty string and false if no tracker claims the URL.
func FindTrackerForRef(externalRef string) (string, bool) {
	return globalRegistry.FindTrackerForRef(externalRef)
}

// FindTrackerForRef attempts to find a tracker that recognizes the external_ref.
func (r *Registry) FindTrackerForRef(externalRef string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, factory := range r.trackers {
		tracker := factory()
		if tracker.IsExternalRef(externalRef) {
			return name, true
		}
	}
	return "", false
}
