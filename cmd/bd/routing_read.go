package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/storage"
)

// routingConfigKeys is the fixed set of config keys the routing subsystem
// reads on every command. Loading them all in one GetAllConfig call avoids
// six sequential round trips against a remote Dolt server.
var routingConfigKeys = []string{
	"routing.mode",
	"routing.contributor",
	"routing.default",
	"routing.maintainer",
	"contributor.auto_route",
	"contributor.planning_repo",
}

// resolveRoutingConfigValue mirrors getRoutingConfigValue but reads the DB
// value from a pre-fetched map instead of issuing a per-key SQL query.
func resolveRoutingConfigValue(key string, dbValues map[string]string) string {
	// Only trust YAML/env values that were explicitly set, not Viper defaults.
	if src := config.GetValueSource(key); src != config.SourceDefault {
		value := strings.TrimSpace(config.GetString(key))
		if value != "" {
			return value
		}
	}
	if dbValues == nil {
		return ""
	}
	return strings.TrimSpace(dbValues[key])
}

// getRoutingConfigValue resolves routing config from YAML/env first, then DB config.
// Only uses the YAML value if it was explicitly set (not a Viper default), so that
// DB-stored values aren't shadowed by defaults like "~/.beads-planning".
//
// Deprecated for the bd-list hot path: see resolveRoutingConfigValue, which
// reads from a pre-fetched map. Kept for callers that only need one key.
func getRoutingConfigValue(ctx context.Context, store storage.DoltStorage, key string) string {
	// Only trust YAML/env values that were explicitly set, not Viper defaults.
	if src := config.GetValueSource(key); src != config.SourceDefault {
		value := strings.TrimSpace(config.GetString(key))
		if value != "" {
			return value
		}
	}

	if store == nil {
		return ""
	}

	dbValue, err := store.GetConfig(ctx, key)
	if err != nil {
		debug.Logf("DEBUG: failed to read config %q from store: %v\n", key, err)
		return ""
	}
	return strings.TrimSpace(dbValue)
}

// determineAutoRoutedRepoPath returns the repository path that should be used for
// issue reads when contributor auto-routing is enabled.
func determineAutoRoutedRepoPath(ctx context.Context, store storage.DoltStorage) string {
	userRole, err := routing.DetectUserRole(".")
	if err != nil {
		debug.Logf("Warning: failed to detect user role: %v\n", err)
	}

	// Batch-fetch the routing keys in a single SQL query. The previous
	// implementation issued one GetConfig per key (6 round trips); on a
	// 200ms-RTT remote Dolt connection that alone cost ~1.2s per command.
	var dbValues map[string]string
	if store != nil {
		all, allErr := store.GetAllConfig(ctx)
		if allErr != nil {
			debug.Logf("DEBUG: failed to read routing config from store: %v\n", allErr)
		} else {
			dbValues = make(map[string]string, len(routingConfigKeys))
			for _, k := range routingConfigKeys {
				if v, ok := all[k]; ok {
					dbValues[k] = v
				}
			}
		}
	}

	// Build routing config with backward compatibility for legacy contributor.* keys.
	routingMode := resolveRoutingConfigValue("routing.mode", dbValues)
	contributorRepo := resolveRoutingConfigValue("routing.contributor", dbValues)

	// Backward compatibility - fall back to legacy contributor.* keys
	if routingMode == "" {
		if resolveRoutingConfigValue("contributor.auto_route", dbValues) == "true" {
			routingMode = "auto"
		}
	}
	if contributorRepo == "" {
		contributorRepo = resolveRoutingConfigValue("contributor.planning_repo", dbValues)
	}

	routingConfig := &routing.RoutingConfig{
		Mode:             routingMode,
		DefaultRepo:      resolveRoutingConfigValue("routing.default", dbValues),
		MaintainerRepo:   resolveRoutingConfigValue("routing.maintainer", dbValues),
		ContributorRepo:  contributorRepo,
		ExplicitOverride: "",
	}

	return routing.DetermineTargetRepo(routingConfig, userRole, ".")
}

// openRoutedReadStore opens the auto-routed target store for read commands.
// Returns routed=false when reads should stay in the current store.
func openRoutedReadStore(ctx context.Context, store storage.DoltStorage) (storage.DoltStorage, bool, error) {
	repoPath := determineAutoRoutedRepoPath(ctx, store)
	if repoPath == "" || repoPath == "." {
		return nil, false, nil
	}

	targetRepoPath := routing.ExpandPath(repoPath)
	targetBeadsDir := filepath.Join(targetRepoPath, ".beads")
	targetStore, err := newReadOnlyStoreFromConfig(ctx, targetBeadsDir)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open routed store at %s: %w", targetRepoPath, err)
	}
	return targetStore, true, nil
}
