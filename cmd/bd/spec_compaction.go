package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

type specCompactionCandidate struct {
	SpecID         string                `json:"spec_id"`
	Title          string                `json:"title"`
	Score          float64               `json:"score"`
	Recommendation string                `json:"recommendation"`
	Factors        specCompactionFactors `json:"factors"`
}

type specCompactionFactors struct {
	AllIssuesClosed   bool `json:"all_issues_closed"`
	SpecUnchangedDays int  `json:"spec_unchanged_days"`
	CodeActivityDays  int  `json:"code_activity_days"`
	IsSuperseded      bool `json:"is_superseded"`
}

var (
	specCandidatesThreshold  float64
	specCandidatesAutoMark   bool
	specCandidatesArchive    bool
	specAutoCompactThreshold float64
	specAutoCompactExecute   bool
	specAutoCompactDryRun    bool
	specAutoCompactArchive   bool
)

var specCandidatesCmd = &cobra.Command{
	Use:   "candidates",
	Short: "Find specs that may be ready to mark as complete",
	Long: `Analyze specs and suggest which ones are ready to be marked complete.

Scoring algorithm:
  +0.4 - All linked issues are closed
  +0.3 - Spec unchanged for 30+ days
  +0.2 - Has at least one linked issue
  +0.1 - Title suggests completion (contains "complete", "done", "finished")

Specs with score >= 0.6 are shown as candidates.`,
	Run: runSpecCompletionCandidates,
}

var specAutoCompactCmd = &cobra.Command{
	Use:   "auto-compact",
	Short: "Auto-compact specs that meet compaction threshold",
	Run: func(cmd *cobra.Command, _ []string) {
		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		specStore, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		if specAutoCompactDryRun && specAutoCompactExecute {
			FatalErrorRespectJSON("cannot combine --dry-run and --execute")
		}
		execute := specAutoCompactExecute
		if !specAutoCompactDryRun && !specAutoCompactExecute {
			specAutoCompactDryRun = true
		}

		threshold := specAutoCompactThreshold
		if threshold < 0 {
			threshold = 0.8
		}

		candidates, err := buildSpecCompactionCandidates(rootCtx, store, specStore)
		if err != nil {
			FatalErrorRespectJSON("spec auto-compact failed: %v", err)
		}
		selected := filterSpecCompactionCandidates(candidates, threshold)

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"threshold":  threshold,
				"execute":    execute,
				"dry_run":    specAutoCompactDryRun,
				"candidates": selected,
			})
			return
		}

		if specAutoCompactDryRun {
			fmt.Printf("%s Auto-compaction DRY RUN (threshold: %.2f)\n\n", ui.RenderWarn("○"), threshold)
		} else {
			fmt.Printf("%s Auto-compaction EXECUTE (threshold: %.2f)\n\n", ui.RenderPass("✓"), threshold)
		}

		if len(selected) == 0 {
			fmt.Println("No specs meet the compaction threshold.")
			return
		}

		for _, candidate := range selected {
			if specAutoCompactDryRun {
				fmt.Printf("○ %s (score %.2f)\n", candidate.SpecID, candidate.Score)
				continue
			}
			if err := compactSpecCandidate(rootCtx, specStore, candidate); err != nil {
				fmt.Fprintf(os.Stderr, "spec compact failed for %s: %v\n", candidate.SpecID, err)
				continue
			}
			fmt.Printf("%s %s (score %.2f)\n", ui.RenderPass("✓"), candidate.SpecID, candidate.Score)
		}
	},
}

func init() {
	specCandidatesCmd.Flags().BoolVar(&specCandidatesAutoMark, "auto", false, "Auto-mark specs with score >= 0.8")
	specCandidatesCmd.Flags().BoolVar(&specCandidatesArchive, "archive", false, "Archive specs when auto-marking")
	specAutoCompactCmd.Flags().Float64Var(&specAutoCompactThreshold, "threshold", 0.8, "Minimum score required for auto-compaction")
	specAutoCompactCmd.Flags().BoolVar(&specAutoCompactExecute, "execute", false, "Apply compaction (default: dry-run)")
	specAutoCompactCmd.Flags().BoolVar(&specAutoCompactDryRun, "dry-run", false, "Preview compactions without modifying specs")
	specAutoCompactCmd.Flags().BoolVar(&specAutoCompactArchive, "archive", true, "Move archived specs to specs/archive")

	specCmd.AddCommand(specCandidatesCmd)
	specCmd.AddCommand(specAutoCompactCmd)
}

func runSpecCompletionCandidates(cmd *cobra.Command, args []string) {
	autoMark := specCandidatesAutoMark
	archiveRequested := specCandidatesArchive || archiveHintFromArgs(args) || autoMark

	if daemonClient != nil && !autoMark && !archiveRequested {
		resp, err := daemonClient.SpecCandidates(&rpc.SpecCandidatesArgs{
			Auto: autoMark,
		})
		if err != nil {
			FatalErrorRespectJSON("spec candidates failed: %v", err)
		}
		var result rpc.SpecCandidatesResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			FatalErrorRespectJSON("invalid spec candidates response: %v", err)
		}
		renderSpecCompletionCandidates(result)
		return
	}

	if err := ensureDatabaseFresh(rootCtx); err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	specStore, err := getSpecRegistryStore()
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	entries, err := specStore.ListSpecRegistryWithCounts(rootCtx)
	if err != nil {
		FatalErrorRespectJSON("list spec registry: %v", err)
	}

	now := time.Now()
	candidates := make([]rpc.SpecCandidateEntry, 0)

	for _, entry := range entries {
		// Skip missing, complete, or archived specs
		if entry.Spec.MissingAt != nil {
			continue
		}
		if entry.Spec.Lifecycle == "complete" || entry.Spec.Lifecycle == "archived" {
			continue
		}

		// Get issues linked to this spec
		specID := entry.Spec.SpecID
		filter := types.IssueFilter{SpecID: &specID}
		issues, err := store.SearchIssues(rootCtx, "", filter)
		if err != nil {
			FatalErrorRespectJSON("search issues for spec %s: %v", specID, err)
		}

		// Count open vs closed
		openCount := 0
		closedCount := 0
		for _, issue := range issues {
			if issue.Status == types.StatusClosed || issue.Status == types.StatusTombstone {
				closedCount++
			} else {
				openCount++
			}
		}

		// Calculate score
		score := 0.0
		reasons := make([]string, 0)

		// +0.4 - All linked issues are closed
		if len(issues) > 0 && openCount == 0 {
			score += 0.4
			reasons = append(reasons, fmt.Sprintf("All %d issues closed", closedCount))
		}

		// +0.3 - Spec unchanged for 30+ days
		daysOld := 0
		if !entry.Spec.Mtime.IsZero() {
			daysOld = int(now.Sub(entry.Spec.Mtime).Hours() / 24)
			if daysOld >= 30 {
				score += 0.3
				reasons = append(reasons, fmt.Sprintf("%d days old", daysOld))
			}
		}

		// +0.2 - Has at least one linked issue
		if len(issues) > 0 {
			score += 0.2
		}

		// +0.1 - Title suggests completion
		titleLower := strings.ToLower(entry.Spec.Title)
		if strings.Contains(titleLower, "complete") ||
			strings.Contains(titleLower, "done") ||
			strings.Contains(titleLower, "finished") {
			score += 0.1
			reasons = append(reasons, "Title suggests completion")
		}

		// Only include if score >= 0.6
		if score >= 0.6 {
			action := "SUGGEST"
			if score >= 0.8 {
				action = "MARK"
			}

			reason := strings.Join(reasons, ", ")
			if reason == "" {
				reason = "No issues linked"
				if daysOld > 0 {
					reason = fmt.Sprintf("No issues linked, %d days old", daysOld)
				}
			}

			candidates = append(candidates, rpc.SpecCandidateEntry{
				SpecID:      entry.Spec.SpecID,
				Title:       entry.Spec.Title,
				Score:       score,
				Action:      action,
				Reason:      reason,
				OpenIssues:  openCount,
				ClosedCount: closedCount,
				DaysOld:     daysOld,
			})
		}
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	result := rpc.SpecCandidatesResult{
		Candidates: candidates,
	}

	// Auto-mark if requested
	if autoMark {
		for i := range result.Candidates {
			c := &result.Candidates[i]
			if c.Score >= 0.8 {
				specText := readSpecContent(c.SpecID)
				if archiveRequested || specHasArchiveDirective(specText) {
					entry, err := specStore.GetSpecRegistry(rootCtx, c.SpecID)
					if err != nil || entry == nil {
						c.Error = fmt.Sprintf("spec missing: %v", err)
						continue
					}
					beadsForSpec, err := store.SearchIssues(rootCtx, "", types.IssueFilter{SpecID: &c.SpecID})
					if err != nil {
						c.Error = fmt.Sprintf("beads lookup failed: %v", err)
						continue
					}
					summary := buildAutoSpecSummary(entry, specText, beadsForSpec)
					summaryTokens := len(strings.Fields(summary))
					newSpecID, err := archiveSpecWithSummary(rootCtx, c.SpecID, summary, summaryTokens, store, specStore, true)
					if err != nil {
						c.Error = fmt.Sprintf("archive failed: %v", err)
						continue
					}
					c.SpecID = newSpecID
					c.Action = "ARCHIVED"
					c.Marked = true
					result.Marked++
					continue
				}

				lifecycle := "complete"
				completedAt := time.Now().UTC().Truncate(time.Second)
				update := spec.SpecRegistryUpdate{
					Lifecycle:   &lifecycle,
					CompletedAt: &completedAt,
				}
				if err := specStore.UpdateSpecRegistry(rootCtx, c.SpecID, update); err != nil {
					c.Error = fmt.Sprintf("failed to mark: %v", err)
				} else {
					c.Marked = true
					result.Marked++
				}
			}
		}
		markDirtyAndScheduleFlush()
	}

	renderSpecCompletionCandidates(result)
}

func renderSpecCompletionCandidates(result rpc.SpecCandidatesResult) {
	if jsonOutput {
		outputJSON(result)
		return
	}

	if len(result.Candidates) == 0 {
		fmt.Println("No spec completion candidates found.")
		return
	}

	fmt.Println("Spec Completion Candidates")
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SPEC PATH\tSCORE\tACTION\tREASON")

	for _, c := range result.Candidates {
		scoreStr := fmt.Sprintf("%.2f", c.Score)
		action := c.Action
		if c.Marked {
			if c.Action == "ARCHIVED" {
				action = ui.RenderPass("ARCHIVED")
			} else {
				action = ui.RenderPass("MARKED")
			}
		} else if c.Error != "" {
			action = ui.RenderFail("ERROR")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.SpecID, scoreStr, action, c.Reason)
	}
	_ = w.Flush()

	fmt.Println()
	if result.Marked > 0 {
		fmt.Printf("%s Marked %d specs as complete\n", ui.RenderPass("*"), result.Marked)
	} else {
		fmt.Println("Run 'bd spec mark-done <path>' to mark as complete")
	}
}

func filterSpecCompactionCandidates(candidates []specCompactionCandidate, threshold float64) []specCompactionCandidate {
	if threshold <= 0 {
		return candidates
	}
	filtered := make([]specCompactionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Score >= threshold {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func renderSpecCompactionCandidates(candidates []specCompactionCandidate, threshold float64) {
	if len(candidates) == 0 {
		fmt.Printf("No specs meet the compaction threshold (%.2f).\n", threshold)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SPEC ID\tSCORE\tRECOMMENDATION\tCLOSED\tSPEC_DAYS\tCODE_DAYS\tSUPERSEDED")
	for _, candidate := range candidates {
		closed := renderBool(candidate.Factors.AllIssuesClosed)
		superseded := renderBool(candidate.Factors.IsSuperseded)
		fmt.Fprintf(w, "%s\t%.2f\t%s\t%s\t%d\t%d\t%s\n",
			candidate.SpecID,
			candidate.Score,
			strings.ToUpper(candidate.Recommendation),
			closed,
			candidate.Factors.SpecUnchangedDays,
			candidate.Factors.CodeActivityDays,
			superseded,
		)
	}
	_ = w.Flush()
}

func renderBool(v bool) string {
	if v {
		return ui.RenderPass("✓")
	}
	return ui.RenderWarn("○")
}

func buildSpecCompactionCandidates(ctx context.Context, store storage.Storage, specStore spec.SpecRegistryStore) ([]specCompactionCandidate, error) {
	if store == nil || specStore == nil {
		return nil, fmt.Errorf("storage not available")
	}
	entries, err := specStore.ListSpecRegistry(ctx)
	if err != nil {
		return nil, err
	}

	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return nil, fmt.Errorf("no .beads directory found")
	}
	repoRoot := filepath.Dir(beadsDir)

	candidates := make([]specCompactionCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry.MissingAt != nil {
			continue
		}
		if !spec.IsScannableSpecID(entry.SpecID) {
			continue
		}
		if entry.Lifecycle == "archived" && strings.TrimSpace(entry.Summary) != "" {
			continue
		}

		beads, err := store.SearchIssues(ctx, "", types.IssueFilter{SpecID: &entry.SpecID})
		if err != nil {
			continue
		}
		factors := specCompactionFactors{
			AllIssuesClosed:   !hasOpenBeads(beads),
			SpecUnchangedDays: specUnchangedDays(repoRoot, entry),
			CodeActivityDays:  gitActivityDays(repoRoot, entry.SpecID),
			IsSuperseded:      isSpecSuperseded(repoRoot, entry.SpecID),
		}
		score, recommendation := scoreSpecCompactionCandidate(factors)
		candidates = append(candidates, specCompactionCandidate{
			SpecID:         entry.SpecID,
			Title:          entry.Title,
			Score:          score,
			Recommendation: recommendation,
			Factors:        factors,
		})
	}

	return candidates, nil
}

func compactSpecCandidate(ctx context.Context, specStore spec.SpecRegistryStore, candidate specCompactionCandidate) error {
	specID := candidate.SpecID
	entry, err := specStore.GetSpecRegistry(ctx, specID)
	if err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("spec not found")
	}
	specText := readSpecContent(specID)
	if strings.TrimSpace(specText) == "" {
		return fmt.Errorf("spec content missing")
	}

	beadsForSpec, err := store.SearchIssues(ctx, "", types.IssueFilter{SpecID: &specID})
	if err != nil {
		return err
	}

	summary := buildAutoSpecSummary(entry, specText, beadsForSpec)
	summaryTokens := len(strings.Fields(summary))
	_, err = archiveSpecWithSummary(ctx, specID, summary, summaryTokens, store, specStore, specAutoCompactArchive)
	if err != nil {
		return err
	}
	markDirtyAndScheduleFlush()
	return nil
}

func scoreSpecCompactionCandidate(factors specCompactionFactors) (float64, string) {
	score := 0.0
	if factors.AllIssuesClosed {
		score += 0.4
	}
	if factors.SpecUnchangedDays >= 30 {
		score += 0.2
	}
	if factors.CodeActivityDays >= 45 {
		score += 0.2
	}
	if factors.IsSuperseded {
		score += 0.2
	}

	switch {
	case score >= 0.8:
		return score, "compact"
	case score >= 0.6:
		return score, "review"
	default:
		return score, "keep"
	}
}

func specUnchangedDays(repoRoot string, entry spec.SpecRegistryEntry) int {
	if !entry.Mtime.IsZero() {
		return daysSince(entry.Mtime)
	}
	path := filepath.Join(repoRoot, entry.SpecID)
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return daysSince(info.ModTime())
}

func daysSince(t time.Time) int {
	if t.IsZero() {
		return -1
	}
	return int(time.Since(t).Hours() / 24)
}

func gitActivityDays(repoRoot, specID string) int {
	cmd := exec.Command("git", "log", "-1", "--format=%ct", "--", specID)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return 999
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 999
	}
	seconds, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 999
	}
	return int(time.Since(time.Unix(seconds, 0)).Hours() / 24)
}

func isSpecSuperseded(repoRoot, specID string) bool {
	path := filepath.Join(repoRoot, specID)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := strings.ToLower(string(data))
	return strings.Contains(text, "supersedes:") ||
		strings.Contains(text, "status: archived") ||
		strings.Contains(text, "status: deprecated")
}
