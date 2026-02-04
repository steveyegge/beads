package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
)

type specVolatilitySummary struct {
	ChangeCount         int
	WeightedChangeCount float64
	LastChangedAt       *time.Time
	OpenIssues          int
}

type specVolatilityLevel string

const (
	specVolatilityHigh   specVolatilityLevel = "HIGH"
	specVolatilityMedium specVolatilityLevel = "MEDIUM"
	specVolatilityLow    specVolatilityLevel = "LOW"
	specVolatilityStable specVolatilityLevel = "STABLE"
)

func classifySpecVolatility(changeCount, openIssues int) specVolatilityLevel {
	highChanges := config.GetInt("volatility.high_changes")
	highMixedChanges := config.GetInt("volatility.high_mixed_changes")
	highOpenIssues := config.GetInt("volatility.high_open_issues")
	mediumChanges := config.GetInt("volatility.medium_changes")
	lowChanges := config.GetInt("volatility.low_changes")
	if highChanges == 0 {
		highChanges = 5
	}
	if highMixedChanges == 0 {
		highMixedChanges = 3
	}
	if highOpenIssues == 0 {
		highOpenIssues = 3
	}
	if mediumChanges == 0 {
		mediumChanges = 2
	}
	if lowChanges == 0 {
		lowChanges = 1
	}

	if changeCount >= highChanges || (changeCount >= highMixedChanges && openIssues >= highOpenIssues) {
		return specVolatilityHigh
	}
	if changeCount >= mediumChanges && openIssues > 0 {
		return specVolatilityMedium
	}
	if changeCount >= lowChanges || openIssues > 0 {
		return specVolatilityLow
	}
	return specVolatilityStable
}

func volatilityWindow() time.Duration {
	window := config.GetString("volatility.window")
	if window == "" {
		window = "30d"
	}
	duration, err := parseDurationString(window)
	if err != nil {
		return 30 * 24 * time.Hour
	}
	return duration
}

func volatilityHalfLife() time.Duration {
	halfLife := config.GetString("volatility.decay.half_life")
	if strings.TrimSpace(halfLife) == "" {
		return 0
	}
	duration, err := parseDurationString(halfLife)
	if err != nil {
		return 0
	}
	return duration
}

func effectiveVolatilityChanges(summary specVolatilitySummary) int {
	if summary.WeightedChangeCount > 0 {
		return int(summary.WeightedChangeCount + 0.5)
	}
	return summary.ChangeCount
}

func effectiveRiskChanges(entry spec.SpecRiskEntry) int {
	if entry.WeightedChangeCount > 0 {
		return int(entry.WeightedChangeCount + 0.5)
	}
	return entry.ChangeCount
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

	now := time.Now().UTC()
	halfLife := volatilityHalfLife()
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
		rawChangeCount, lastChangedAt := spec.SummarizeScanEvents(events, time.Time{})
		weighted := 0.0
		if halfLife > 0 {
			weighted, lastChangedAt = spec.SummarizeScanEventsWeighted(events, time.Time{}, now, halfLife)
		}
		results[specID] = specVolatilitySummary{
			ChangeCount:         rawChangeCount,
			WeightedChangeCount: weighted,
			LastChangedAt:       lastChangedAt,
			OpenIssues:          openIssues[specID],
		}
	}
	return results, nil
}
