package storage

import (
	"context"
	"fmt"
	"sync"
)

// Driver is the top-level backend abstraction. Every storage backend implements
// Driver; bd's command layer depends on Driver, never on a concrete type or
// backend-specific sub-interface.
//
// Backend-specific features (Dolt version-control, PG advisory locks) live in
// sub-interfaces. Callers acquire them via type-assertion after a Capabilities()
// check.
type Driver interface {
	Storage

	// Name returns the backend name, e.g. "dolt" or "postgres".
	Name() string

	// Capabilities returns the static set of capabilities for this backend.
	// The set is constant for the lifetime of the driver.
	Capabilities() CapabilitySet

	// Open initializes the backend connection using cfg.
	// It is called once after the driver is constructed; the driver is not
	// usable before Open returns nil.
	Open(ctx context.Context, cfg DriverConfig) error

	// Ping verifies the backend is reachable and responsive.
	Ping(ctx context.Context) error

	// SchemaVersion returns the current schema version stored in the backend.
	SchemaVersion(ctx context.Context) (int, error)

	// InitSchema creates the schema at version 0 in an empty backend.
	InitSchema(ctx context.Context) error

	// MigrateSchema advances the schema to targetVersion.
	// It is a no-op when the current version already equals targetVersion.
	MigrateSchema(ctx context.Context, targetVersion int) error
}

// DriverConfig is the backend-agnostic open configuration passed to DriverOpener
// and Driver.Open.
type DriverConfig struct {
	// BeadsDir is the path to the .beads directory.
	BeadsDir string

	// ReadOnly opens the backend in read-only mode when true.
	ReadOnly bool

	// Options holds driver-specific key-value pairs (e.g. DSN for postgres).
	Options map[string]string
}

// DriverOpener is the constructor function registered per backend.
// It must return a newly constructed Driver in a zero/default state.
// Open has NOT been called; OpenDriver calls it after construction.
type DriverOpener func() (Driver, error)

var (
	driverMu       sync.RWMutex
	driverRegistry = map[string]DriverOpener{}
)

// RegisterDriver registers opener under name. It panics if name is already
// registered, following the convention of database/sql.Register.
func RegisterDriver(name string, opener DriverOpener) {
	driverMu.Lock()
	defer driverMu.Unlock()
	if _, dup := driverRegistry[name]; dup {
		panic(fmt.Sprintf("storage: driver %q registered twice", name))
	}
	driverRegistry[name] = opener
}

// OpenDriver looks up the registered opener for name, constructs the driver,
// and calls Open(ctx, cfg) on it. The returned Driver is ready to use.
func OpenDriver(ctx context.Context, name string, cfg DriverConfig) (Driver, error) {
	driverMu.RLock()
	opener, ok := driverRegistry[name]
	driverMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("storage: no driver registered for %q", name)
	}
	d, err := opener()
	if err != nil {
		return nil, fmt.Errorf("storage: construct driver %q: %w", name, err)
	}
	if err := d.Open(ctx, cfg); err != nil {
		return nil, fmt.Errorf("storage: open driver %q: %w", name, err)
	}
	return d, nil
}
