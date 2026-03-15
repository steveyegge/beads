package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/utils"
)

// openReadOnlyStoreForDBPath reopens a read-only store from an existing dbPath
// while preserving repo-local metadata such as dolt_database and the resolved
// Dolt server port. Falls back to a raw path-only open when no matching
// metadata.json can be found.
func openReadOnlyStoreForDBPath(ctx context.Context, dbPath string) (*dolt.DoltStore, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("no database path available")
	}

	if beadsDir := resolveBeadsDirForDBPath(dbPath); beadsDir != "" {
		return dolt.NewFromConfigWithOptions(ctx, beadsDir, &dolt.Config{ReadOnly: true})
	}

	return dolt.New(ctx, &dolt.Config{Path: dbPath, ReadOnly: true})
}

// resolveBeadsDirForDBPath maps a database path back to its owning .beads
// directory when metadata.json is available. This is needed for repos that use
// non-default dolt_database names or custom dolt_data_dir locations.
func resolveBeadsDirForDBPath(dbPath string) string {
	actualDBPath := utils.CanonicalizePath(dbPath)
	candidates := []string{utils.CanonicalizePath(filepath.Dir(actualDBPath))}

	if found := beads.FindBeadsDir(); found != "" {
		found = utils.CanonicalizePath(found)
		if !slices.Contains(candidates, found) {
			candidates = append(candidates, found)
		}
	}

	for _, beadsDir := range candidates {
		cfg, err := configfile.Load(beadsDir)
		if err != nil || cfg == nil {
			continue
		}
		if utils.CanonicalizePath(cfg.DatabasePath(beadsDir)) == actualDBPath {
			return beadsDir
		}
	}

	return ""
}
