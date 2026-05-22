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

// routingConfigKeys lists every config key consulted by routing resolution.
// Used to batch-load DB values in one query (GH read-path optimization).
var routingConfigKeys = []string{
	"routing.mode",
	"routing.contributor",
	"routing.default",
	"routing.maintainer",
	"contributor.auto_route",
	"contributor.planning_repo",
}

// resolveRoutingConfigValue applies the standard YAML/env-first, then-DB
// precedence using a preloaded DB-config map. dbValues may be nil (treated as
// empty) when the store lookup failed; resolution still works from YAML/env.
func resolveRoutingConfigValue(key string, dbValues map[string]string) string {
	// Only trust YAML/env values that were explicitly set, not Viper defaults.
	if src := config.GetValueSource(key); src != config.SourceDefault {
		if value := strings.TrimSpace(config.GetString(key)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(dbValues[key])
}

// getRoutingConfigValue is the single-key compatibility shim retained for
// callers (e.g. write paths in bd create) that do not yet batch their reads.
// New code should prefer resolveRoutingConfigValue with a preloaded map.
func getRoutingConfigValue(ctx context.Context, store storage.DoltStorage, key string) string {
	if src := config.GetValueSource(key); src != config.SourceDefault {
		if value := strings.TrimSpace(config.GetString(key)); value != "" {
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

	// Load all routing-related config keys in a single DB transaction rather
	// than issuing one tx per key. The full config table is small in
	// practice (well under a thousand rows on real projects).
	var dbValues map[string]string
	if store != nil {
		all, allErr := store.GetAllConfig(ctx)
		if allErr != nil {
			debug.Logf("DEBUG: failed to read config from store: %v\n", allErr)
		} else {
			dbValues = make(map[string]string, len(routingConfigKeys))
			for _, key := range routingConfigKeys {
				if v, ok := all[key]; ok {
					dbValues[key] = v
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
