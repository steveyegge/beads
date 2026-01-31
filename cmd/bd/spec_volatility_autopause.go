package main

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

func maybeAutoPauseVolatileSpecs(ctx context.Context, specIDs []string) (int, error) {
	if !config.GetBool("volatility.auto_pause") {
		return 0, nil
	}
	if len(specIDs) == 0 {
		return 0, nil
	}

	window, err := parseDurationString("30d")
	if err != nil {
		return 0, err
	}
	since := time.Now().UTC().Add(-window).Truncate(time.Second)
	summaries, err := getSpecVolatilitySummaries(ctx, specIDs, since)
	if err != nil {
		return 0, err
	}

	paused := 0
	for specID, summary := range summaries {
		level := classifySpecVolatility(summary.ChangeCount, summary.OpenIssues)
		if level != specVolatilityHigh {
			continue
		}
		count, err := pauseIssuesForSpec(ctx, specID)
		if err != nil {
			return paused, err
		}
		paused += count
	}
	return paused, nil
}

func pauseIssuesForSpec(ctx context.Context, specID string) (int, error) {
	issueStore, _, cleanup, err := openVolatilityStores(ctx)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	filter := types.IssueFilter{
		SpecID:        &specID,
		ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
	}
	issues, err := issueStore.SearchIssues(ctx, "", filter)
	if err != nil {
		return 0, err
	}

	paused := 0
	for _, issue := range issues {
		if issue.Status != types.StatusOpen && issue.Status != types.StatusInProgress {
			continue
		}
		if daemonClient != nil {
			status := string(types.StatusBlocked)
			_, err := daemonClient.Update(&rpc.UpdateArgs{
				ID:     issue.ID,
				Status: &status,
			})
			if err != nil {
				return paused, err
			}
		} else {
			updates := map[string]interface{}{
				"status": types.StatusBlocked,
			}
			if err := issueStore.UpdateIssue(ctx, issue.ID, updates, getActorWithGit()); err != nil {
				return paused, err
			}
		}
		paused++
	}

	if paused > 0 && !jsonOutput {
		fmt.Printf("%s Auto-paused %d issue(s) linked to %s (high volatility)\n",
			ui.RenderWarn("◐"), paused, specID)
	}
	return paused, nil
}
