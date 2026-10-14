package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/metrics"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/versioncontrolops"
)

func runCompactProxiedServer(ctx context.Context) error {
	evt := metrics.NewCommandEvent("compact")
	defer func() {
		if c := metrics.Global(); c != nil {
			c.CloseEventAndAdd(evt)
		}
	}()

	if !compactDoltDryRun {
		CheckReadonly("compact")
	}
	start := time.Now()

	if compactDoltDays < 0 {
		return HandleError("--days must be non-negative")
	}

	// Read main's head before the log: a commit that lands between the two
	// reads is then in the log but not at plannedHead, so Compact refuses
	// instead of resetting main past it, and a refusal names the true head.
	var plannedHead string
	var logEntries []storage.CommitInfo
	if err := runProxiedNonTx(ctx, func(ctx context.Context, conn *sql.Conn) error {
		var err error
		if plannedHead, err = versioncontrolops.MainHead(ctx, conn); err != nil {
			return fmt.Errorf("reading the head of main: %w", err)
		}
		logEntries, err = versioncontrolops.Log(ctx, conn, 0)
		return err
	}); err != nil {
		return HandleError("failed to read commit log: %v", err)
	}

	totalCommits := len(logEntries)
	if totalCommits <= 1 {
		if jsonOutput {
			return outputJSON(map[string]interface{}{
				"success":       true,
				"message":       "nothing to compact",
				"total_commits": totalCommits,
			})
		}
		fmt.Printf("Only %d commit(s). Nothing to compact.\n", totalCommits)
		return nil
	}

	cutoff := time.Now().AddDate(0, 0, -compactDoltDays)

	plan := planCompaction(logEntries, cutoff)
	oldCommits, recentHashes := plan.oldCommits, plan.recentHashes
	initialHash, boundaryHash := plan.initialHash, plan.boundaryHash

	recentCommits := len(recentHashes)

	if compactDoltDryRun {
		if jsonOutput {
			return outputJSON(map[string]interface{}{
				"dry_run":        true,
				"total_commits":  totalCommits,
				"old_commits":    oldCommits,
				"recent_commits": recentCommits,
				"cutoff_days":    compactDoltDays,
				"cutoff_date":    cutoff.Format("2006-01-02"),
				"initial_hash":   initialHash,
				"boundary_hash":  boundaryHash,
				"planned_head":   plannedHead,
			})
		}
		fmt.Printf("DRY RUN — Compact preview\n\n")
		fmt.Printf("  Total commits:  %d\n", totalCommits)
		fmt.Printf("  Old (>%d days): %d (would be squashed into 1)\n", compactDoltDays, oldCommits)
		fmt.Printf("  Recent:         %d (preserved)\n", recentCommits)
		fmt.Printf("  Cutoff date:    %s\n", cutoff.Format("2006-01-02"))
		if oldCommits <= 1 {
			fmt.Printf("\n  Nothing to compact (0-1 old commits).\n")
		} else {
			fmt.Printf("\n  Result: %d commits → %d commit%s\n", totalCommits, recentCommits+1, plural(recentCommits+1))
			fmt.Printf("  Run with --force to proceed.\n")
		}
		return nil
	}

	if oldCommits <= 1 {
		if jsonOutput {
			return outputJSON(map[string]interface{}{
				"success":       true,
				"message":       "nothing to compact",
				"total_commits": totalCommits,
				"old_commits":   oldCommits,
			})
		}
		fmt.Printf("Only %d old commit(s). Nothing to compact.\n", oldCommits)
		return nil
	}

	if boundaryHash == "" {
		return HandleError("could not find boundary commit for compaction")
	}

	if !compactDoltForce {
		return HandleErrorWithHint(
			fmt.Sprintf("would squash %d old commits into 1, preserving %d recent commits",
				oldCommits, recentCommits),
			"Use --force to confirm or --dry-run to preview.")
	}

	if !jsonOutput {
		fmt.Printf("Compacting: %d old commits → 1, preserving %d recent\n",
			oldCommits, recentCommits)
	}

	var pruned, tags []string
	// compactedHead is main after the compaction. changedTables names the
	// tables whose data differs between plannedHead and the squashed head,
	// so it answers whether bd's squash and replay changed any data. Rows
	// main held outside the plan (a commit that landed after the last head
	// check, or rows a writer left uncommitted) are committed above the
	// squashed head as keptRowsCommit and are not in changedTables. All of it
	// is read before GC, which may remove the planned head's chain.
	// commitsAfter counts main's log the same way commits_before does; 0
	// means the count could not be read.
	var compactedHead, keptRowsCommit, changedErr string
	var changedTables []string
	var commitsAfter int
	if err := runProxiedNonTx(ctx, func(ctx context.Context, conn *sql.Conn) error {
		res, err := versioncontrolops.CompactAtHead(ctx, conn, plannedHead, initialHash, boundaryHash, oldCommits, recentHashes)
		if err != nil {
			return err
		}
		keptRowsCommit = res.KeptRowsCommit
		var perr error
		if compactedHead, perr = versioncontrolops.MainHead(ctx, conn); perr != nil {
			WarnError("reading the head of main after compact: %v", perr)
		}
		if changedTables, perr = versioncontrolops.TablesChangedBetween(ctx, conn, plannedHead, res.SquashedHead); perr != nil {
			changedErr = fmt.Sprintf("comparing the squashed head with the planned head: %v", perr)
			WarnError("%s", changedErr)
		}
		if after, perr := versioncontrolops.Log(ctx, conn, 0); perr != nil {
			WarnError("counting commits after compact: %v", perr)
		} else {
			commitsAfter = len(after)
		}
		if pruned, perr = versioncontrolops.PruneRemoteRefs(ctx, conn); perr != nil {
			WarnError("pruning remote-tracking refs before GC: %v (GC may reclaim little)", perr)
		}
		if tags, perr = versioncontrolops.ListTags(ctx, conn); perr != nil {
			WarnError("listing tags before GC: %v", perr)
		}
		// Full pass: the squash orphans a commit chain that any earlier GC
		// already promoted to the old generation, which a default pass never
		// revisits.
		if !jsonOutput {
			fmt.Println("  Running full Dolt GC (all generations; can take minutes on large stores)...")
		}
		if perr := versioncontrolops.DoltGCFull(ctx, conn); perr != nil {
			WarnError("dolt gc after compact failed: %v", perr)
		}
		return nil
	}); err != nil {
		return HandleError("compact failed: %v", err)
	}

	elapsed := time.Since(start)

	if !jsonOutput {
		printPruneReport(pruned, tags)
	}

	if jsonOutput {
		result := map[string]interface{}{
			"success":            true,
			"commits_before":     totalCommits,
			"old_squashed":       oldCommits,
			"recent_kept":        recentCommits,
			"remote_refs_pruned": pruned,
			"tags_anchoring":     tags,
			"gc_mode":            gcModeFull,
			"elapsed_ms":         elapsed.Milliseconds(),
			"planned_head":       plannedHead,
		}
		// Absent means unknown: commits_after and compacted_head are written
		// only when they were read.
		if commitsAfter > 0 {
			result["commits_after"] = commitsAfter
		}
		if compactedHead != "" {
			result["compacted_head"] = compactedHead
		}
		if keptRowsCommit != "" {
			result["kept_rows_commit"] = keptRowsCommit
		}
		// Absent means unknown, never "nothing changed": only a completed
		// comparison writes tables_changed_by_compact, and [] is its clean answer.
		if changedErr != "" {
			result["tables_changed_error"] = changedErr
		} else {
			result["tables_changed_by_compact"] = changedTables
		}
		return outputJSON(result)
	}

	if commitsAfter > 0 {
		fmt.Printf("✓ Compacted %d commits → %d\n", totalCommits, commitsAfter)
	} else {
		fmt.Printf("✓ Compacted %d commits\n", totalCommits)
	}
	fmt.Printf("  Squashed: %d old commits → 1 base\n", oldCommits)
	fmt.Printf("  Preserved: %d recent commits\n", recentCommits)
	if keptRowsCommit != "" {
		fmt.Printf("  Kept: rows main held outside the plan, committed as %s\n", keptRowsCommit)
	}
	switch {
	case changedErr != "":
		fmt.Printf("  Data: not compared with the planned head %s\n", plannedHead)
	case len(changedTables) == 0:
		fmt.Printf("  Data: same as the planned head %s\n", plannedHead)
	default:
		fmt.Printf("  Data: differs from the planned head %s in %s\n", plannedHead, strings.Join(changedTables, ", "))
	}
	fmt.Printf("  Time: %v\n", elapsed.Round(time.Millisecond))
	return nil
}
