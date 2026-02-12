package main

import "fmt"

// ensureStoreActive checks that the storage backend is initialized.
// In daemon-only mode, the store is set up by PersistentPreRun (either via
// daemon RPC or direct storage fallback). This function validates that
// initialization succeeded.
func ensureStoreActive() error {
	// If daemon is connected, commands use daemon RPC, not direct storage.
	if getDaemonClient() != nil {
		return nil
	}

	lockStore()
	active := isStoreActive() && getStore() != nil
	unlockStore()
	if active {
		return nil
	}

	return fmt.Errorf("no storage backend available; ensure the daemon is running with 'bd daemon start'")
}
