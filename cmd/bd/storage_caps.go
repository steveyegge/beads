package main

import (
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
)

// capabilitySetForBackend returns the CapabilitySet and driver name for the
// currently active backend. This is the single point for capability-based
// dispatch in dolt/* commands.
//
// Lookup order:
//  1. If the store implements storage.Driver (e.g., DoltDriver, PGDriver), use
//     the driver's own Capabilities() and Name().
//  2. If the store is a non-Driver DoltStorage (legacy *dolt.DoltStore or
//     *embeddeddolt.EmbeddedDoltStore), return DoltCapabilities.
//  3. If no store is available (nil), read the backend name from metadata.json
//     and return the matching CapabilitySet. This covers PG workspaces where
//     the store has not been wired up yet.
func capabilitySetForBackend(beadsDir string) (storage.CapabilitySet, string) {
	st := getStore()
	if st != nil {
		inner := storage.UnwrapStore(st)
		if d, ok := inner.(storage.Driver); ok {
			return d.Capabilities(), d.Name()
		}
		return storage.DoltCapabilities, "dolt"
	}

	// No Dolt store — read backend from configfile.
	if beadsDir == "" {
		beadsDir = selectedDoltBeadsDir()
	}
	cfg, _ := configfile.Load(beadsDir)
	var backend string
	if cfg != nil {
		backend = cfg.GetBackend()
	}
	backendCaps := map[string]struct {
		caps storage.CapabilitySet
		name string
	}{
		configfile.BackendDolt:     {storage.DoltCapabilities, "dolt"},
		configfile.BackendPostgres: {storage.PostgresCapabilities, "postgres"},
	}
	if entry, ok := backendCaps[backend]; ok {
		return entry.caps, entry.name
	}
	return storage.DoltCapabilities, "dolt"
}

// requireDoltCap checks that the current backend supports cap and fatal-errors
// with a structured capability-not-supported message when it does not.
// Call this at the top of any bd dolt/* command handler that requires a
// Dolt-specific capability (push, pull, sync, version-control, etc.).
func requireDoltCap(beadsDir string, cap storage.Capability) {
	caps, name := capabilitySetForBackend(beadsDir)
	if err := caps.Require(cap, name); err != nil {
		FatalErrorRespectJSON("%v", err)
	}
}
