package gate

import (
	"fmt"
	"sync"
)

// Registry holds registered session gates, organized by hook type.
type Registry struct {
	mu    sync.RWMutex
	gates map[HookType][]*Gate
	byID  map[string]*Gate
}

// NewRegistry creates an empty gate registry.
func NewRegistry() *Registry {
	return &Registry{
		gates: make(map[HookType][]*Gate),
		byID:  make(map[string]*Gate),
	}
}

// Register adds a gate to the registry. Returns an error if a gate
// with the same ID is already registered.
func (r *Registry) Register(g *Gate) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byID[g.ID]; exists {
		return fmt.Errorf("gate %q already registered", g.ID)
	}

	r.gates[g.Hook] = append(r.gates[g.Hook], g)
	r.byID[g.ID] = g
	return nil
}

// Unregister removes a gate from the registry by ID.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	g, exists := r.byID[id]
	if !exists {
		return
	}
	delete(r.byID, id)

	// Remove from hook list
	hookGates := r.gates[g.Hook]
	for i, hg := range hookGates {
		if hg.ID == id {
			r.gates[g.Hook] = append(hookGates[:i], hookGates[i+1:]...)
			break
		}
	}
}

// Get returns a gate by ID, or nil if not found.
func (r *Registry) Get(id string) *Gate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[id]
}

// GatesForHook returns all gates registered for the given hook type.
func (r *Registry) GatesForHook(hookType HookType) []*Gate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Gate, len(r.gates[hookType]))
	copy(result, r.gates[hookType])
	return result
}

// AllGates returns all registered gates.
func (r *Registry) AllGates() []*Gate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Gate, 0, len(r.byID))
	for _, g := range r.byID {
		result = append(result, g)
	}
	return result
}

// Count returns the total number of registered gates.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byID)
}
