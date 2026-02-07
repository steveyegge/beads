package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// syncStoreAdapter wraps storage.Storage to implement tracker.SyncStore.
// The storage.Storage interface already has all the methods SyncStore needs,
// so this adapter provides a clean type conversion.
type syncStoreAdapter struct {
	storage.Storage
}

// Ensure syncStoreAdapter implements tracker.SyncStore at compile time.
var _ tracker.SyncStore = (*syncStoreAdapter)(nil)

// newSyncStoreAdapter creates a new adapter wrapping the given storage.
func newSyncStoreAdapter(s storage.Storage) *syncStoreAdapter {
	return &syncStoreAdapter{s}
}

// GetIssue retrieves an issue by ID.
func (a *syncStoreAdapter) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	return a.Storage.GetIssue(ctx, id)
}

// GetIssueByExternalRef retrieves an issue by its external reference URL.
func (a *syncStoreAdapter) GetIssueByExternalRef(ctx context.Context, externalRef string) (*types.Issue, error) {
	return a.Storage.GetIssueByExternalRef(ctx, externalRef)
}

// SearchIssues searches for issues matching the query and filter.
func (a *syncStoreAdapter) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	return a.Storage.SearchIssues(ctx, query, filter)
}

// CreateIssue creates a new issue.
func (a *syncStoreAdapter) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	return a.Storage.CreateIssue(ctx, issue, actor)
}

// UpdateIssue updates an existing issue.
func (a *syncStoreAdapter) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return a.Storage.UpdateIssue(ctx, id, updates, actor)
}

// AddDependency adds a dependency between issues.
func (a *syncStoreAdapter) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	return a.Storage.AddDependency(ctx, dep, actor)
}

// GetConfig retrieves a configuration value.
func (a *syncStoreAdapter) GetConfig(ctx context.Context, key string) (string, error) {
	return a.Storage.GetConfig(ctx, key)
}

// SetConfig stores a configuration value.
func (a *syncStoreAdapter) SetConfig(ctx context.Context, key, value string) error {
	return a.Storage.SetConfig(ctx, key, value)
}

// printSyncResult outputs the sync result in the appropriate format.
// If jsonOutput is true, outputs JSON. Otherwise outputs human-readable text.
func printSyncResult(result *tracker.SyncResult, dryRun bool, trackerName string) {
	if jsonOutput {
		outputJSON(result)
		return
	}

	if dryRun {
		fmt.Println("\n[DRY RUN] Sync preview complete (no changes made)")
		if result.Stats.Created > 0 {
			fmt.Printf("  Would create: %d issues\n", result.Stats.Created)
		}
		if result.Stats.Updated > 0 {
			fmt.Printf("  Would update: %d issues\n", result.Stats.Updated)
		}
		if result.Stats.Conflicts > 0 {
			fmt.Printf("  Would resolve: %d conflicts\n", result.Stats.Conflicts)
		}
		return
	}

	if result.Success {
		fmt.Printf("\n%s sync complete\n", trackerName)
	} else {
		fmt.Fprintf(os.Stderr, "\n%s sync failed: %s\n", trackerName, result.Error)
	}

	// Print warnings
	if len(result.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  - %s\n", w)
		}
	}
}

// outputSyncResultJSON outputs the sync result as JSON.
func outputSyncResultJSON(result *tracker.SyncResult) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

// configStoreAdapter wraps storage.Storage to implement tracker.ConfigStore.
// This is used for creating tracker.Config instances.
type configStoreAdapter struct {
	storage.Storage
}

// Ensure configStoreAdapter implements tracker.ConfigStore at compile time.
var _ tracker.ConfigStore = (*configStoreAdapter)(nil)

// newConfigStoreAdapter creates a new adapter wrapping the given storage.
func newConfigStoreAdapter(s storage.Storage) *configStoreAdapter {
	return &configStoreAdapter{s}
}

// GetConfig retrieves a configuration value.
func (a *configStoreAdapter) GetConfig(ctx context.Context, key string) (string, error) {
	return a.Storage.GetConfig(ctx, key)
}

// SetConfig stores a configuration value.
func (a *configStoreAdapter) SetConfig(ctx context.Context, key, value string) error {
	return a.Storage.SetConfig(ctx, key, value)
}

// GetAllConfig retrieves all configuration values.
func (a *configStoreAdapter) GetAllConfig(ctx context.Context) (map[string]string, error) {
	return a.Storage.GetAllConfig(ctx)
}
