package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// BackendType names a registered storage driver. Drivers self-register at
// init() time via RegisterDriver; consumers select one with Open.
//
// The set is intentionally small. New backends ship as their own packages
// blank-imported by callers — see internal/storage/teststub for an example.
type BackendType string

// Built-in backend names. Drivers that implement these self-register from
// internal/storage/doltdriver and internal/storage/postgres respectively.
const (
	BackendDolt     BackendType = "dolt"
	BackendPostgres BackendType = "postgres"
)

// ConnectionConfig is the small, stable configuration surface accepted by
// every driver. Drivers interpret the fields they care about and ignore the
// rest:
//
//   - Dolt reads BeadsDir + Database + ReadOnly; ignores DSN.
//   - Postgres reads DSN + ReadOnly; ignores BeadsDir + Database.
//
// There is no DriverOptions escape hatch — backend-specific tuning lives
// inside the driver (parsed from DSN query parameters, environment, or
// metadata.json) so the registry interface stays narrow and stable.
type ConnectionConfig struct {
	// BeadsDir is the path to the .beads directory. Used by file-system-rooted
	// backends (Dolt) to locate metadata.json and on-disk state.
	BeadsDir string
	// Database is the logical database name. Empty string means "use the
	// driver's configured default" (typically read from metadata.json).
	Database string
	// ReadOnly skips schema initialization and refuses mutating operations.
	ReadOnly bool
	// DSN carries connection-string-shaped configuration for network-rooted
	// backends. Format is driver-specific (e.g. postgres:// URI for the
	// Postgres backend); ignored by file-system-rooted drivers.
	DSN string
}

// Factory opens a Storage instance for a registered backend. Factories are
// supplied to RegisterDriver and dispatched from Open.
type Factory func(ctx context.Context, cfg ConnectionConfig) (Storage, error)

// ErrUnknownBackend is returned by Open when the requested backend has not
// been registered. Available enumerates the registered set (sorted) so
// callers can produce a helpful error message.
type ErrUnknownBackend struct {
	Name      string
	Available []BackendType
}

func (e *ErrUnknownBackend) Error() string {
	if len(e.Available) == 0 {
		return fmt.Sprintf("unknown storage backend %q (no backends registered)", e.Name)
	}
	names := make([]string, len(e.Available))
	for i, b := range e.Available {
		names[i] = string(b)
	}
	return fmt.Sprintf("unknown storage backend %q (available: %v)", e.Name, names)
}

var (
	registryMu      sync.RWMutex
	registryEntries = map[BackendType]Factory{}
)

// RegisterDriver associates a backend name with a factory. Drivers call this
// from init() so consumers can select them by name without enumerating
// backends. Panics if the same name is registered twice — duplicate
// registration almost always means two implementations conflicting at
// link-time, which is a programming error worth surfacing loudly.
func RegisterDriver(name BackendType, factory Factory) {
	if name == "" {
		panic("storage.RegisterDriver: empty backend name")
	}
	if factory == nil {
		panic(fmt.Sprintf("storage.RegisterDriver: nil factory for %q", name))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registryEntries[name]; dup {
		panic(fmt.Sprintf("storage.RegisterDriver: backend %q already registered", name))
	}
	registryEntries[name] = factory
}

// Open dispatches to the factory registered for name. Returns ErrUnknownBackend
// if no driver is registered under that name.
func Open(ctx context.Context, name BackendType, cfg ConnectionConfig) (Storage, error) {
	registryMu.RLock()
	factory, ok := registryEntries[name]
	registryMu.RUnlock()
	if !ok {
		return nil, &ErrUnknownBackend{Name: string(name), Available: RegisteredBackends()}
	}
	return factory(ctx, cfg)
}

// RegisteredBackends returns the names of all currently registered backends
// in sorted order. Useful for error messages and capability discovery.
func RegisteredBackends() []BackendType {
	registryMu.RLock()
	names := make([]BackendType, 0, len(registryEntries))
	for n := range registryEntries {
		names = append(names, n)
	}
	registryMu.RUnlock()
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}
