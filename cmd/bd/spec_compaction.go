package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
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
	specAutoCompactThreshold float64
	specAutoCompactExecute   bool
	specAutoCompactDryRun    bool
)

var specCandidatesCmd = &cobra.Command{
	Use:   "candidates",
	Short: "List specs eligible for auto-compaction",
	Run: func(cmd *cobra.Command, _ []string) {
		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		specStore, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		candidates, err := buildSpecCompactionCandidates(rootCtx, store, specStore)
		if err != nil {
			FatalErrorRespectJSON("spec candidates failed: %v", err)
		}

		threshold := specCandidatesThreshold
		if threshold < 0 {
			threshold = 0.7
		}
		filtered := filterSpecCompactionCandidates(candidates, threshold)

		if jsonOutput {
			outputJSON(filtered)
			return
		}

		renderSpecCompactionCandidates(filtered, threshold)
	},
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
	specCandidatesCmd.Flags().Float64Var(&specCandidatesThreshold, "threshold", 0.7, "Minimum score to include in results")
	specAutoCompactCmd.Flags().Float64Var(&specAutoCompactThreshold, "threshold", 0.8, "Minimum score required for auto-compaction")
	specAutoCompactCmd.Flags().BoolVar(&specAutoCompactExecute, "execute", false, "Apply compaction (default: dry-run)")
	specAutoCompactCmd.Flags().BoolVar(&specAutoCompactDryRun, "dry-run", false, "Preview compactions without modifying specs")

	specCmd.AddCommand(specCandidatesCmd)
	specCmd.AddCommand(specAutoCompactCmd)
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
	now := time.Now().UTC().Truncate(time.Second)
	summaryTokens := len(strings.Fields(summary))

	update := spec.SpecRegistryUpdate{
		Lifecycle:     ptrString("archived"),
		Summary:       &summary,
		SummaryTokens: &summaryTokens,
		ArchivedAt:    &now,
	}
	if err := specStore.UpdateSpecRegistry(ctx, specID, update); err != nil {
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
