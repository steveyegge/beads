package main

import (
	"context"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

// ImportOptions configures import behavior.
type ImportOptions struct {
	DryRun                     bool
	SkipUpdate                 bool
	Strict                     bool
	RenameOnImport             bool
	ClearDuplicateExternalRefs bool
	OrphanHandling             string
	DeletionIDs                []string
	SkipPrefixValidation       bool
	ProtectLocalExportIDs      map[string]time.Time
}

// ImportResult describes what an import operation did.
type ImportResult struct {
	Created             int
	Updated             int
	Unchanged           int
	Skipped             int
	Deleted             int
	Collisions          int
	IDMapping           map[string]string
	CollisionIDs        []string
	PrefixMismatch      bool
	ExpectedPrefix      string
	MismatchPrefixes    map[string]int
	SkippedDependencies []string
}

// importIssuesCore imports issues into the Dolt store.
// This is a bridge function that delegates to the Dolt store's batch creation.
func importIssuesCore(ctx context.Context, _ string, store *dolt.DoltStore, issues []*types.Issue, opts ImportOptions) (*ImportResult, error) {
	if opts.DryRun || len(issues) == 0 {
		return &ImportResult{Skipped: len(issues)}, nil
	}

	err := store.CreateIssuesWithFullOptions(ctx, issues, getActorWithGit(), storage.BatchCreateOptions{
		OrphanHandling:       storage.OrphanAllow,
		SkipPrefixValidation: opts.SkipPrefixValidation,
	})
	if err != nil {
		return nil, err
	}

	return &ImportResult{Created: len(issues)}, nil
}
