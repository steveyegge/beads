//go:build !cgo

package main

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/storage/doltdriver"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
)

// isEmbeddedMode returns false in non-CGO builds since embedded Dolt
// requires CGO. Only server mode is available.
func isEmbeddedMode() bool {
	return false
}

// newDoltStore creates a server-mode storage backend. Embedded Dolt is not
// available without CGO.
func newDoltStore(ctx context.Context, cfg *dolt.Config) (storage.Storage, error) {
	if !cfg.ServerMode {
		return nil, fmt.Errorf("%s", doltdriver.NoCGOEmbeddedErrMsg)
	}
	return dolt.New(ctx, cfg)
}

// acquireEmbeddedLock returns a no-op lock in non-CGO builds.
func acquireEmbeddedLock(_ string, _ bool) (embeddeddolt.Unlocker, error) {
	return embeddeddolt.NoopLock{}, nil
}

// newDoltStoreFromConfig opens a Dolt store using metadata.json under
// beadsDir. Thin wrapper over the registry — non-CGO builds reach
// doltdriver's nocgo dispatcher which returns the install/server-mode hint
// for non-server-mode configs.
func newDoltStoreFromConfig(ctx context.Context, beadsDir string) (storage.Storage, error) {
	return storage.Open(ctx, storage.BackendDolt, storage.ConnectionConfig{BeadsDir: beadsDir})
}

// newReadOnlyStoreFromConfig is the read-only counterpart for non-CGO builds.
func newReadOnlyStoreFromConfig(ctx context.Context, beadsDir string) (storage.Storage, error) {
	return storage.Open(ctx, storage.BackendDolt, storage.ConnectionConfig{BeadsDir: beadsDir, ReadOnly: true})
}
