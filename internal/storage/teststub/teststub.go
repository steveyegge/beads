// Package teststub is a minimal storage backend that exists solely to
// verify the driver registry is extensible from packages outside of
// internal/storage (FR-10 in be-l7t.2 ADR).
//
// Importing this package for side-effects registers the "teststub" backend.
// The returned Storage value satisfies the interface at the type level via
// embedding; method calls panic. Tests that import teststub must therefore
// only exercise the registration / dispatch path, never the Storage
// methods themselves.
package teststub

import (
	"context"

	"github.com/steveyegge/beads/internal/storage"
)

// Name is the BackendType this package registers.
const Name = storage.BackendType("teststub")

func init() {
	storage.RegisterDriver(Name, openTeststub)
}

func openTeststub(_ context.Context, cfg storage.ConnectionConfig) (storage.Storage, error) {
	return &Stub{Config: cfg}, nil
}

// Stub is the value returned by the teststub factory. It satisfies
// storage.Storage at compile time via the embedded interface; any actual
// method dispatch will panic with a nil-interface error. Config is exposed
// so tests can confirm the registry forwarded ConnectionConfig unchanged.
type Stub struct {
	storage.Storage
	Config storage.ConnectionConfig
}
