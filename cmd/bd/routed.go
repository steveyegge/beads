package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

// RoutedResult contains the result of a routed issue lookup
type RoutedResult struct {
	Issue      *types.Issue
	Store      storage.Storage // The store that contains this issue (may be routed)
	Routed     bool            // true if the issue was found via routing
	ResolvedID string          // The resolved (full) issue ID
	closeFn    func()          // Function to close routed storage (if any)
}

// Close closes any routed storage. Safe to call if Routed is false.
func (r *RoutedResult) Close() {
	if r.closeFn != nil {
		r.closeFn()
	}
}

// resolveAndGetIssueWithRouting resolves a partial ID and gets the issue,
// using routes.jsonl for prefix-based routing if needed.
// This enables cross-repo issue lookups (e.g., `bd show gt-xyz` from ~/gt).
//
// The resolution happens in the correct store based on the ID prefix.
// Returns a RoutedResult containing the issue, resolved ID, and the store to use.
// The caller MUST call result.Close() when done to release any routed storage.
func resolveAndGetIssueWithRouting(ctx context.Context, localStore storage.Storage, id string) (*RoutedResult, error) {
	// When BD_DAEMON_HOST is set, the remote daemon handles all IDs centrally.
	// Skip local routing to avoid "direct database access blocked" errors (bd-ma0s.1).
	if isRemoteDaemon() {
		return resolveAndGetFromStore(ctx, localStore, id, false)
	}

	// Step 1: Check if routing is needed based on ID prefix
	// Find the .beads metadata directory (not the database path, which may be external with Dolt)
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		// No beads directory found - use local store
		return resolveAndGetFromStore(ctx, localStore, id, false)
	}

	// Use factory.NewFromConfig as the storage opener to respect backend configuration
	routedStorage, err := routing.GetRoutedStorageWithOpener(ctx, id, beadsDir, factory.NewFromConfig)
	if err != nil {
		return nil, err
	}

	if routedStorage != nil {
		// Step 2: Resolve and get from routed store
		result, err := resolveAndGetFromStore(ctx, routedStorage.Storage, id, true)
		if err != nil {
			_ = routedStorage.Close()
			return nil, err
		}
		if result != nil {
			result.closeFn = func() { _ = routedStorage.Close() }
			return result, nil
		}
		_ = routedStorage.Close()
	}

	// Step 3: Fall back to local store
	return resolveAndGetFromStore(ctx, localStore, id, false)
}

// resolveAndGetFromStore resolves a partial ID and gets the issue from a specific store.
func resolveAndGetFromStore(ctx context.Context, s storage.Storage, id string, routed bool) (*RoutedResult, error) {
	// First, resolve the partial ID
	resolvedID, err := utils.ResolvePartialID(ctx, s, id)
	if err != nil {
		return nil, err
	}

	// Then get the issue
	issue, err := s.GetIssue(ctx, resolvedID)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, nil
	}

	return &RoutedResult{
		Issue:      issue,
		Store:      s,
		Routed:     routed,
		ResolvedID: resolvedID,
	}, nil
}

// getIssueWithRouting tries to get an issue from the local store first,
// then falls back to checking routes.jsonl for prefix-based routing.
// This enables cross-repo issue lookups (e.g., `bd show gt-xyz` from ~/gt).
//
// Returns a RoutedResult containing the issue and the store to use for related queries.
// The caller MUST call result.Close() when done to release any routed storage.
func getIssueWithRouting(ctx context.Context, localStore storage.Storage, id string) (*RoutedResult, error) {
	// Step 1: Try local store first (current behavior)
	issue, err := localStore.GetIssue(ctx, id)
	if err == nil && issue != nil {
		return &RoutedResult{
			Issue:      issue,
			Store:      localStore,
			Routed:     false,
			ResolvedID: id,
		}, nil
	}

	// When BD_DAEMON_HOST is set, skip local routing - remote daemon handles all IDs (bd-ma0s.1).
	if isRemoteDaemon() {
		return &RoutedResult{
			Issue:      issue,
			Store:      localStore,
			Routed:     false,
			ResolvedID: id,
		}, err
	}

	// Step 2: Check routes.jsonl for prefix-based routing
	// Find the .beads metadata directory (not the database path, which may be external with Dolt)
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		// No beads directory found - return original result
		return &RoutedResult{
			Issue:      issue,
			Store:      localStore,
			Routed:     false,
			ResolvedID: id,
		}, err
	}

	// Use GetRoutedStorageWithOpener with factory to respect backend configuration (bd-m2jr)
	routedStorage, routeErr := routing.GetRoutedStorageWithOpener(ctx, id, beadsDir, factory.NewFromConfig)
	if routeErr != nil || routedStorage == nil {
		// No routing found or error - return original result
		return &RoutedResult{
			Issue:      issue,
			Store:      localStore,
			Routed:     false,
			ResolvedID: id,
		}, err
	}

	// Step 3: Try the routed storage
	routedIssue, routedErr := routedStorage.Storage.GetIssue(ctx, id)
	if routedErr != nil || routedIssue == nil {
		_ = routedStorage.Close()
		// Return the original error if routing also failed
		if err != nil {
			return nil, err
		}
		return nil, routedErr
	}

	// Return the issue with the routed store
	return &RoutedResult{
		Issue:      routedIssue,
		Store:      routedStorage.Storage,
		Routed:     true,
		ResolvedID: id,
		closeFn: func() {
			_ = routedStorage.Close()
		},
	}, nil
}

// getRoutedStoreForID returns a storage connection for an issue ID if routing is needed.
// Returns nil if no routing is needed (issue should be in local store).
// The caller is responsible for closing the returned storage.
func getRoutedStoreForID(ctx context.Context, id string) (*routing.RoutedStorage, error) {
	// When BD_DAEMON_HOST is set, skip local routing - remote daemon handles all IDs (bd-ma0s.1).
	if isRemoteDaemon() {
		return nil, nil
	}

	// Find the .beads metadata directory (not the database path, which may be external with Dolt)
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return nil, nil
	}

	// Use GetRoutedStorageWithOpener with factory to respect backend configuration (bd-m2jr)
	return routing.GetRoutedStorageWithOpener(ctx, id, beadsDir, factory.NewFromConfig)
}

// isRemoteDaemon returns true if connected to a remote daemon via BD_DAEMON_HOST
// env var or daemon-host config key. When connected to a remote daemon, skip local
// filesystem routing - the remote daemon handles all IDs centrally. This fixes
// gt-57wsnm. Using rpc.GetDaemonHost() ensures config.yaml is also checked.
func isRemoteDaemon() bool {
	return rpc.GetDaemonHost() != ""
}

// needsRouting checks if an ID would be routed to a different beads directory.
// This is used to decide whether to bypass the daemon for cross-repo lookups.
func needsRouting(id string) bool {
	// Find the .beads metadata directory (not the database path, which may be external with Dolt)
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return false
	}

	targetDir, routed, err := routing.ResolveBeadsDirForID(context.Background(), id, beadsDir)
	if err != nil || !routed {
		return false
	}

	// Check if the routed directory is different from the current one
	return targetDir != beadsDir
}

// connectToRoutedDaemon connects to the daemon serving a routed issue ID (bd-6lp0).
// It resolves the route for the given ID, finds the target beads directory,
// and connects to the daemon running there via its Unix socket.
//
// Returns (client, nil) if a daemon is available at the routed target.
// Returns (nil, nil) if no routing is needed or no daemon is available.
// Returns (nil, error) on routing resolution failure.
//
// The caller is responsible for closing the returned client.
func connectToRoutedDaemon(id string) (*rpc.Client, error) {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return nil, nil
	}

	targetDir, routed, err := routing.ResolveBeadsDirForID(context.Background(), id, beadsDir)
	if err != nil {
		return nil, err
	}
	if !routed || targetDir == beadsDir {
		return nil, nil // No routing needed
	}

	// Connect to daemon at target beads dir
	// The socket path is determined by the workspace (parent of .beads)
	workspacePath := filepath.Dir(targetDir)
	socketPath := rpc.ShortSocketPath(workspacePath)
	client, err := rpc.TryConnect(socketPath)
	if err != nil || client == nil {
		return nil, nil // No daemon available at target - not an error
	}

	return client, nil
}

// resolveIDViaRoutedDaemon resolves a partial issue ID via a daemon at the routed
// target rig (bd-6lp0). This avoids opening direct Dolt storage connections that
// conflict with running daemons.
//
// Returns (resolvedID, client, nil) if a daemon is available and ID resolves.
// The caller is responsible for closing the returned client.
// Returns ("", nil, nil) if no daemon is available (caller should fall back).
// Returns ("", nil, error) on failure.
func resolveIDViaRoutedDaemon(id string) (string, *rpc.Client, error) {
	client, err := connectToRoutedDaemon(id)
	if err != nil || client == nil {
		return "", nil, err
	}

	// Resolve the partial ID via the routed daemon
	resp, err := client.ResolveID(&rpc.ResolveIDArgs{ID: id})
	if err != nil {
		client.Close()
		return "", nil, err
	}

	var resolvedID string
	if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
		client.Close()
		return "", nil, err
	}

	return resolvedID, client, nil
}

// ResolvedBatch holds batch ID resolution results split by routing.
// Non-routed IDs are resolved (partial → full) and placed in ResolvedIDs.
// IDs needing cross-rig routing are collected in RoutedArgs for separate handling.
type ResolvedBatch struct {
	ResolvedIDs []string // IDs resolved via daemon or direct store
	RoutedArgs  []string // IDs needing cross-rig routing
}

// resolveIDsWithRouting resolves a batch of partial IDs, splitting them into
// locally-resolved IDs and routed IDs that need cross-rig handling.
// This replaces the identical resolution loop duplicated in show/close/update.
//
// When daemon is non-nil, resolution uses RPC. Otherwise, uses direct store lookup.
// Routed IDs (those needing a different beads directory) are collected separately.
// Returns error on first resolution failure.
func resolveIDsWithRouting(ctx context.Context, localStore storage.Storage,
	daemon *rpc.Client, ids []string,
) (*ResolvedBatch, error) {
	batch := &ResolvedBatch{}
	skipLocalRouting := isRemoteDaemon()

	for _, id := range ids {
		// Check if this ID needs routing to a different beads directory
		// Skip for remote daemons which handle all IDs centrally (gt-57wsnm)
		if !skipLocalRouting && needsRouting(id) {
			batch.RoutedArgs = append(batch.RoutedArgs, id)
			continue
		}

		if daemon != nil {
			// Daemon mode: resolve via RPC
			resp, err := daemon.ResolveID(&rpc.ResolveIDArgs{ID: id})
			if err != nil {
				return nil, fmt.Errorf("resolving ID %s: %w", id, err)
			}
			var resolvedID string
			if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
				return nil, fmt.Errorf("unmarshaling resolved ID: %w", err)
			}
			batch.ResolvedIDs = append(batch.ResolvedIDs, resolvedID)
		} else {
			// Direct mode: resolve via store, or mark as routed
			if needsRouting(id) {
				batch.RoutedArgs = append(batch.RoutedArgs, id)
			} else {
				resolvedID, err := utils.ResolvePartialID(ctx, localStore, id)
				if err != nil {
					return nil, fmt.Errorf("resolving ID %s: %w", id, err)
				}
				batch.ResolvedIDs = append(batch.ResolvedIDs, resolvedID)
			}
		}
	}

	return batch, nil
}

// RoutedIDAction is a callback for processing a single routed issue ID.
// It receives the resolved full ID and either a routedClient (daemon at target rig)
// or a directResult (direct storage fallback). Exactly one will be non-nil.
type RoutedIDAction func(resolvedID string, routedClient *rpc.Client, directResult *RoutedResult) error

// forEachRoutedID processes routed IDs by trying the daemon at the target rig first,
// falling back to direct storage resolution. For each ID, it calls the action callback
// with either a routed daemon client or a direct RoutedResult.
//
// The caller's action callback is responsible for closing the routedClient or using
// directResult.Close() as appropriate. forEachRoutedID handles cleanup on errors.
// Errors are logged to stderr and processing continues to the next ID.
func forEachRoutedID(ctx context.Context, localStore storage.Storage,
	routedArgs []string, action RoutedIDAction,
) {
	for _, id := range routedArgs {
		// Try daemon at the routed rig first to avoid direct Dolt access conflicts
		resolvedID, routedClient, routeErr := resolveIDViaRoutedDaemon(id)
		if routedClient != nil {
			if err := action(resolvedID, routedClient, nil); err != nil {
				fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", id, err)
			}
			continue
		}
		if routeErr != nil {
			fmt.Fprintf(os.Stderr, "Error routing %s: %v\n", id, routeErr)
			continue
		}

		// Fall back to direct storage (no daemon at target rig)
		result, err := resolveAndGetIssueWithRouting(ctx, localStore, id)
		if err != nil {
			if result != nil {
				result.Close()
			}
			fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", id, err)
			continue
		}
		if result == nil || result.Issue == nil {
			if result != nil {
				result.Close()
			}
			fmt.Fprintf(os.Stderr, "Issue %s not found\n", id)
			continue
		}

		if err := action(result.ResolvedID, nil, result); err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", id, err)
			result.Close()
			continue
		}
		result.Close()
	}
}
