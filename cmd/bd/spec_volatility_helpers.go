package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
)

type specVolatilitySummary struct {
	ChangeCount   int
	LastChangedAt *time.Time
	OpenIssues    int
}

type specVolatilityLevel string

const (
	specVolatilityHigh   specVolatilityLevel = "HIGH"
	specVolatilityMedium specVolatilityLevel = "MEDIUM"
	specVolatilityLow    specVolatilityLevel = "LOW"
	specVolatilityStable specVolatilityLevel = "STABLE"
)

func classifySpecVolatility(changeCount, openIssues int) specVolatilityLevel {
	if changeCount >= 5 || (changeCount >= 3 && openIssues >= 3) {
		return specVolatilityHigh
	}
	if changeCount >= 2 && openIssues > 0 {
		return specVolatilityMedium
	}
	if changeCount >= 1 || openIssues > 0 {
		return specVolatilityLow
	}
	return specVolatilityStable
}

func openVolatilityStores(ctx context.Context) (storage.Storage, spec.SpecRegistryStore, func(), error) {
	if store != nil {
		specStore, ok := store.(spec.SpecRegistryStore)
		if !ok {
			return nil, nil, nil, fmt.Errorf("storage backend does not support spec registry")
		}
		return store, specStore, func() {}, nil
	}
	if daemonClient == nil {
		return nil, nil, nil, fmt.Errorf("storage not available")
	}
	localStore, err := factory.NewFromConfig(ctx, filepath.Dir(dbPath))
	if err != nil {
		return nil, nil, nil, err
	}
	specStore, ok := localStore.(spec.SpecRegistryStore)
	if !ok {
		_ = localStore.Close()
		return nil, nil, nil, fmt.Errorf("storage backend does not support spec registry")
	}
	previousStore := store
	store = localStore
	cleanup := func() {
		store = previousStore
		_ = localStore.Close()
	}
	return localStore, specStore, cleanup, nil
}

func getSpecVolatilitySummary(ctx context.Context, specID string, since time.Time) (*specVolatilitySummary, error) {
	summaries, err := getSpecVolatilitySummaries(ctx, []string{specID}, since)
	if err != nil {
		return nil, err
	}
	if summary, ok := summaries[specID]; ok {
		return &summary, nil
	}
	return nil, nil
}

func getSpecVolatilitySummaries(ctx context.Context, specIDs []string, since time.Time) (map[string]specVolatilitySummary, error) {
	results := make(map[string]specVolatilitySummary)
	if len(specIDs) == 0 {
		return results, nil
	}

	specIDSet := make(map[string]struct{})
	for _, specID := range specIDs {
		if specID == "" || !spec.IsScannableSpecID(specID) {
			continue
		}
		specIDSet[specID] = struct{}{}
	}
	if len(specIDSet) == 0 {
		return results, nil
	}

	issueStore, specStore, cleanup, err := openVolatilityStores(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if daemonClient == nil {
		if err := ensureDatabaseFresh(ctx); err != nil {
			return nil, err
		}
	}

	openFilter := types.IssueFilter{
		ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
	}
	issues, err := issueStore.SearchIssues(ctx, "", openFilter)
	if err != nil {
		return nil, err
	}

	openIssues := make(map[string]int)
	for _, issue := range issues {
		if issue.SpecID == "" {
			continue
		}
		if _, ok := specIDSet[issue.SpecID]; !ok {
			continue
		}
		openIssues[issue.SpecID]++
	}

	for specID := range specIDSet {
		entry, err := specStore.GetSpecRegistry(ctx, specID)
		if err != nil {
			return nil, err
		}
		if entry == nil || entry.MissingAt != nil {
			continue
		}
		events, err := specStore.ListSpecScanEvents(ctx, specID, since)
		if err != nil {
			return nil, err
		}
		changeCount, lastChangedAt := spec.SummarizeScanEvents(events, time.Time{})
		results[specID] = specVolatilitySummary{
			ChangeCount:   changeCount,
			LastChangedAt: lastChangedAt,
			OpenIssues:    openIssues[specID],
		}
	}
	return results, nil
}
