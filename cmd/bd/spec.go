package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var specCmd = &cobra.Command{
	Use:   "spec",
	Short: "Spec registry commands",
}

var specScanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan specs and update registry",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("spec scan")
		specPath, _ := cmd.Flags().GetString("path")
		if len(args) > 0 {
			if specPath != "" && specPath != args[0] {
				FatalErrorRespectJSON("spec path set twice (argument %q and --path %q)", args[0], specPath)
			}
			specPath = args[0]
		}
		if specPath == "" {
			specPath = "specs"
		}

		if daemonClient != nil {
			resp, err := daemonClient.SpecScan(&rpc.SpecScanArgs{Path: specPath})
			if err != nil {
				FatalErrorRespectJSON("spec scan failed: %v", err)
			}
			var result spec.SpecScanResult
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				FatalErrorRespectJSON("invalid spec scan response: %v", err)
			}
			if jsonOutput {
				outputJSON(result)
				return
			}
			fmt.Printf("%s Scanned %d specs (added=%d updated=%d missing=%d marked=%d)\n",
				ui.RenderPass("✓"), result.Scanned, result.Added, result.Updated, result.Missing, result.MarkedBeads)
			return
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		store, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			FatalErrorRespectJSON("no .beads directory found")
		}
		repoRoot := filepath.Dir(beadsDir)

		scanned, err := spec.Scan(repoRoot, specPath)
		if err != nil {
			FatalErrorRespectJSON("scan specs: %v", err)
		}
		result, err := spec.UpdateRegistry(rootCtx, store, scanned, time.Now().UTC().Truncate(time.Second))
		if err != nil {
			FatalErrorRespectJSON("update spec registry: %v", err)
		}

		markDirtyAndScheduleFlush()

		if jsonOutput {
			outputJSON(result)
			return
		}
		fmt.Printf("%s Scanned %d specs (added=%d updated=%d missing=%d marked=%d)\n",
			ui.RenderPass("✓"), result.Scanned, result.Added, result.Updated, result.Missing, result.MarkedBeads)
		fmt.Println("● Note: Spec registry is local-only (not synced via git)")
	},
}

var specCompactCmd = &cobra.Command{
	Use:   "compact <spec_id>",
	Short: "Archive a spec with a summary",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		specID := args[0]
		summary, _ := cmd.Flags().GetString("summary")
		summaryFile, _ := cmd.Flags().GetString("summary-file")
		lifecycle, _ := cmd.Flags().GetString("lifecycle")

		if summary == "" && summaryFile != "" {
			data, err := os.ReadFile(summaryFile)
			if err != nil {
				FatalErrorRespectJSON("read summary file: %v", err)
			}
			summary = strings.TrimSpace(string(data))
		}

		if strings.TrimSpace(summary) == "" {
			FatalErrorRespectJSON("summary is required (use --summary or --summary-file)")
		}

		if lifecycle == "" {
			lifecycle = "archived"
		}

		now := time.Now().UTC().Truncate(time.Second)
		summaryTokens := len(strings.Fields(summary))

		if daemonClient != nil {
			resp, err := daemonClient.SpecCompact(&rpc.SpecCompactArgs{
				SpecID:        specID,
				Lifecycle:     lifecycle,
				Summary:       summary,
				SummaryTokens: summaryTokens,
				ArchivedAt:    &now,
			})
			if err != nil {
				FatalErrorRespectJSON("spec compact failed: %v", err)
			}
			if jsonOutput {
				var entry spec.SpecRegistryEntry
				if err := json.Unmarshal(resp.Data, &entry); err != nil {
					FatalErrorRespectJSON("invalid spec compact response: %v", err)
				}
				outputJSON(entry)
				return
			}
			var entry spec.SpecRegistryEntry
			if err := json.Unmarshal(resp.Data, &entry); err != nil {
				FatalErrorRespectJSON("invalid spec compact response: %v", err)
			}
			fmt.Printf("%s Archived spec: %s\n", ui.RenderPass("✓"), entry.SpecID)
			return
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		store, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		update := spec.SpecRegistryUpdate{
			Lifecycle:     &lifecycle,
			Summary:       &summary,
			SummaryTokens: &summaryTokens,
			ArchivedAt:    &now,
		}
		if err := store.UpdateSpecRegistry(rootCtx, specID, update); err != nil {
			FatalErrorRespectJSON("update spec registry: %v", err)
		}

		if jsonOutput {
			entry, err := store.GetSpecRegistry(rootCtx, specID)
			if err != nil {
				FatalErrorRespectJSON("get spec: %v", err)
			}
			outputJSON(entry)
			return
		}

		fmt.Printf("%s Archived spec: %s\n", ui.RenderPass("✓"), specID)
	},
}

var specListCmd = &cobra.Command{
	Use:   "list",
	Short: "List specs in the registry",
	Run: func(cmd *cobra.Command, _ []string) {
		prefix, _ := cmd.Flags().GetString("prefix")
		includeMissing, _ := cmd.Flags().GetBool("include-missing")

		if daemonClient != nil {
			resp, err := daemonClient.SpecList(&rpc.SpecListArgs{
				Prefix:         prefix,
				IncludeMissing: includeMissing,
			})
			if err != nil {
				FatalErrorRespectJSON("spec list failed: %v", err)
			}
			var entries []spec.SpecRegistryCount
			if err := json.Unmarshal(resp.Data, &entries); err != nil {
				FatalErrorRespectJSON("invalid spec list response: %v", err)
			}
			renderSpecList(entries)
			return
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		store, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		entries, err := store.ListSpecRegistryWithCounts(rootCtx)
		if err != nil {
			FatalErrorRespectJSON("list spec registry: %v", err)
		}

		filtered := make([]spec.SpecRegistryCount, 0, len(entries))
		for _, entry := range entries {
			if !includeMissing && entry.Spec.MissingAt != nil {
				continue
			}
			if prefix != "" && !strings.HasPrefix(entry.Spec.SpecID, prefix) {
				continue
			}
			filtered = append(filtered, entry)
		}
		renderSpecList(filtered)
	},
}

var specShowCmd = &cobra.Command{
	Use:   "show <spec_id>",
	Short: "Show a spec and its linked beads",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		specID := strings.TrimSpace(args[0])
		if specID == "" {
			FatalErrorRespectJSON("spec_id is required")
		}

		if daemonClient != nil {
			resp, err := daemonClient.SpecShow(&rpc.SpecShowArgs{SpecID: specID})
			if err != nil {
				FatalErrorRespectJSON("spec show failed: %v", err)
			}
			var result rpc.SpecShowResult
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				FatalErrorRespectJSON("invalid spec show response: %v", err)
			}
			renderSpecShow(result)
			return
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		specStore, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		entry, err := specStore.GetSpecRegistry(rootCtx, specID)
		if err != nil {
			FatalErrorRespectJSON("get spec: %v", err)
		}
		if entry == nil {
			FatalErrorRespectJSON("spec not found: %s", specID)
		}

		filter := types.IssueFilter{SpecID: &specID}
		beads, err := store.SearchIssues(rootCtx, "", filter)
		if err != nil {
			FatalErrorRespectJSON("list beads for spec: %v", err)
		}

		result := rpc.SpecShowResult{Spec: entry, Beads: beads}
		renderSpecShow(result)
	},
}

var specCoverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Show spec coverage metrics",
	Run: func(cmd *cobra.Command, _ []string) {
		prefix, _ := cmd.Flags().GetString("prefix")
		includeMissing, _ := cmd.Flags().GetBool("include-missing")

		if daemonClient != nil {
			resp, err := daemonClient.SpecCoverage(&rpc.SpecCoverageArgs{
				Prefix:         prefix,
				IncludeMissing: includeMissing,
			})
			if err != nil {
				FatalErrorRespectJSON("spec coverage failed: %v", err)
			}
			var result rpc.SpecCoverageResult
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				FatalErrorRespectJSON("invalid spec coverage response: %v", err)
			}
			renderSpecCoverage(result)
			return
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		store, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		entries, err := store.ListSpecRegistryWithCounts(rootCtx)
		if err != nil {
			FatalErrorRespectJSON("list spec registry: %v", err)
		}

		result := rpc.SpecCoverageResult{}
		for _, entry := range entries {
			if !includeMissing && entry.Spec.MissingAt != nil {
				continue
			}
			if prefix != "" && !strings.HasPrefix(entry.Spec.SpecID, prefix) {
				continue
			}
			result.Total++
			if entry.Spec.MissingAt != nil {
				result.Missing++
			}
			if entry.BeadCount > 0 {
				result.WithBeads++
			} else {
				result.WithoutBeads++
			}
			if entry.ChangedBeadCount > 0 {
				result.WithChangedBeads++
			}
		}
		renderSpecCoverage(result)
	},
}

var specAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit all specs with completion status",
	Long: `Show all specs with linked issues and completion percentage.

Output columns:
  - Status icon: ○ pending, ◐ in-progress, ✓ complete, ❄ stale
  - Spec path
  - Issues: closed/total
  - Completion percentage`,
	Run: runSpecAudit,
}

func runSpecAudit(cmd *cobra.Command, args []string) {
	prefix, _ := cmd.Flags().GetString("prefix")
	includeMissing, _ := cmd.Flags().GetBool("include-missing")

	if daemonClient != nil {
		resp, err := daemonClient.SpecAudit(&rpc.SpecAuditArgs{
			Prefix:         prefix,
			IncludeMissing: includeMissing,
		})
		if err != nil {
			FatalErrorRespectJSON("spec audit failed: %v", err)
		}
		var result rpc.SpecAuditResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			FatalErrorRespectJSON("invalid spec audit response: %v", err)
		}
		renderSpecAudit(result)
		return
	}

	if err := ensureDatabaseFresh(rootCtx); err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	specStore, err := getSpecRegistryStore()
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	entries, err := specStore.ListSpecRegistry(rootCtx)
	if err != nil {
		FatalErrorRespectJSON("list spec registry: %v", err)
	}

	// Get all issues to count open/closed per spec
	allIssues, err := store.SearchIssues(rootCtx, "", types.IssueFilter{
		IncludeTombstones: false,
	})
	if err != nil {
		FatalErrorRespectJSON("list issues: %v", err)
	}

	// Build issue counts per spec
	openCounts := make(map[string]int)
	closedCounts := make(map[string]int)
	for _, issue := range allIssues {
		if issue.SpecID == "" {
			continue
		}
		if issue.Status == types.StatusClosed {
			closedCounts[issue.SpecID]++
		} else {
			openCounts[issue.SpecID]++
		}
	}

	now := time.Now()
	staleDays := 30

	result := rpc.SpecAuditResult{}

	for _, entry := range entries {
		if !includeMissing && entry.MissingAt != nil {
			continue
		}
		if prefix != "" && !strings.HasPrefix(entry.SpecID, prefix) {
			continue
		}

		openCount := openCounts[entry.SpecID]
		closedCount := closedCounts[entry.SpecID]
		totalCount := openCount + closedCount

		// Calculate completion percentage
		var completion float64
		if totalCount > 0 {
			completion = float64(closedCount) / float64(totalCount) * 100
		}

		// Determine status
		var status string
		isStale := false

		// Check for stale: not modified in 30+ days
		lastMod := entry.Mtime
		if !entry.LastScannedAt.IsZero() && entry.LastScannedAt.After(lastMod) {
			lastMod = entry.LastScannedAt
		}
		if now.Sub(lastMod) > time.Duration(staleDays)*24*time.Hour {
			isStale = true
		}

		// Determine status based on lifecycle or issue counts
		if entry.Lifecycle == "complete" || entry.Lifecycle == "archived" {
			status = "complete"
		} else if isStale && totalCount > 0 && openCount > 0 {
			status = "stale"
		} else if totalCount == 0 {
			status = "pending"
		} else if openCount > 0 {
			status = "in-progress"
		} else {
			status = "complete"
		}

		auditEntry := rpc.SpecAuditEntry{
			SpecID:       entry.SpecID,
			Title:        entry.Title,
			Path:         entry.Path,
			Lifecycle:    entry.Lifecycle,
			OpenIssues:   openCount,
			ClosedIssues: closedCount,
			TotalIssues:  totalCount,
			Completion:   completion,
			Status:       status,
			Stale:        isStale,
		}
		if !lastMod.IsZero() {
			auditEntry.LastModified = lastMod.Format(time.RFC3339)
		}

		result.Entries = append(result.Entries, auditEntry)

		// Update summary
		result.Summary.TotalSpecs++
		switch status {
		case "pending":
			result.Summary.PendingSpecs++
		case "in-progress":
			result.Summary.InProgressSpecs++
		case "complete":
			result.Summary.CompleteSpecs++
		case "stale":
			result.Summary.StaleSpecs++
		}
	}

	renderSpecAudit(result)
}

var specMarkDoneCmd = &cobra.Command{
	Use:   "mark-done <spec_id>",
	Short: "Mark a spec as complete",
	Args:  cobra.ExactArgs(1),
	Run:   runSpecMarkDone,
}

func runSpecMarkDone(cmd *cobra.Command, args []string) {
	specID := args[0]

	if daemonClient != nil {
		resp, err := daemonClient.SpecMarkDone(&rpc.SpecMarkDoneArgs{
			SpecID: specID,
		})
		if err != nil {
			FatalErrorRespectJSON("spec mark-done failed: %v", err)
		}
		var entry spec.SpecRegistryEntry
		if err := json.Unmarshal(resp.Data, &entry); err != nil {
			FatalErrorRespectJSON("invalid spec mark-done response: %v", err)
		}
		if jsonOutput {
			outputJSON(entry)
			return
		}
		fmt.Printf("%s Marked spec as complete: %s\n", ui.RenderPass("✓"), entry.SpecID)
		return
	}

	if err := ensureDatabaseFresh(rootCtx); err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	specStore, err := getSpecRegistryStore()
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	// Verify spec exists
	entry, err := specStore.GetSpecRegistry(rootCtx, specID)
	if err != nil {
		FatalErrorRespectJSON("get spec: %v", err)
	}
	if entry == nil {
		FatalErrorRespectJSON("spec not found: %s", specID)
	}

	now := time.Now().UTC().Truncate(time.Second)
	lifecycle := "complete"

	update := spec.SpecRegistryUpdate{
		Lifecycle:   &lifecycle,
		CompletedAt: &now,
	}

	if err := specStore.UpdateSpecRegistry(rootCtx, specID, update); err != nil {
		FatalErrorRespectJSON("update spec registry: %v", err)
	}

	markDirtyAndScheduleFlush()

	if jsonOutput {
		entry, _ = specStore.GetSpecRegistry(rootCtx, specID)
		outputJSON(entry)
		return
	}

	fmt.Printf("%s Marked spec as complete: %s\n", ui.RenderPass("✓"), specID)
}

func init() {
	specScanCmd.Flags().String("path", "", "Directory to scan (default: specs/)")
	specListCmd.Flags().String("prefix", "", "Filter by spec ID prefix")
	specListCmd.Flags().Bool("include-missing", false, "Include missing specs")
	specCoverageCmd.Flags().String("prefix", "", "Filter by spec ID prefix")
	specCoverageCmd.Flags().Bool("include-missing", false, "Include missing specs")
	specCompactCmd.Flags().String("summary", "", "Summary text for the spec")
	specCompactCmd.Flags().String("summary-file", "", "Read summary text from a file")
	specCompactCmd.Flags().String("lifecycle", "archived", "Lifecycle state to set (default: archived)")
	specAuditCmd.Flags().String("prefix", "", "Filter by spec ID prefix")
	specAuditCmd.Flags().Bool("include-missing", false, "Include missing specs")

	specCmd.AddCommand(specScanCmd)
	specCmd.AddCommand(specListCmd)
	specCmd.AddCommand(specShowCmd)
	specCmd.AddCommand(specCoverageCmd)
	specCmd.AddCommand(specCompactCmd)
	specCmd.AddCommand(specAuditCmd)
	specCmd.AddCommand(specMarkDoneCmd)
	rootCmd.AddCommand(specCmd)
}

func getSpecRegistryStore() (spec.SpecRegistryStore, error) {
	if store == nil {
		return nil, fmt.Errorf("storage not available")
	}
	specStore, ok := store.(spec.SpecRegistryStore)
	if !ok {
		return nil, fmt.Errorf("storage backend does not support spec registry")
	}
	return specStore, nil
}

func renderSpecList(entries []spec.SpecRegistryCount) {
	if jsonOutput {
		outputJSON(entries)
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SPEC ID\tTITLE\tBEADS\tCHANGED\tMISSING")
	for _, entry := range entries {
		missing := ""
		if entry.Spec.MissingAt != nil {
			missing = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n",
			entry.Spec.SpecID, entry.Spec.Title, entry.BeadCount, entry.ChangedBeadCount, missing)
	}
	_ = w.Flush()
}

func renderSpecShow(result rpc.SpecShowResult) {
	if jsonOutput {
		outputJSON(result)
		return
	}

	fmt.Printf("Spec: %s\n", result.Spec.SpecID)
	if result.Spec.Title != "" {
		fmt.Printf("Title: %s\n", result.Spec.Title)
	}
	if result.Spec.SHA256 != "" {
		fmt.Printf("Hash: %s\n", result.Spec.SHA256)
	}
	if result.Spec.Lifecycle != "" {
		fmt.Printf("Lifecycle: %s\n", result.Spec.Lifecycle)
	}
	if result.Spec.CompletedAt != nil {
		fmt.Printf("Completed at: %s\n", result.Spec.CompletedAt.Local().Format("2006-01-02 15:04"))
	}
	if result.Spec.ArchivedAt != nil {
		fmt.Printf("Archived at: %s\n", result.Spec.ArchivedAt.Local().Format("2006-01-02 15:04"))
	}
	if result.Spec.Summary != "" {
		fmt.Printf("Summary: %s\n", result.Spec.Summary)
		if result.Spec.SummaryTokens > 0 {
			fmt.Printf("Summary tokens: %d\n", result.Spec.SummaryTokens)
		}
	}
	if !result.Spec.Mtime.IsZero() {
		fmt.Printf("Mtime: %s\n", result.Spec.Mtime.Local().Format("2006-01-02 15:04"))
	}
	if !result.Spec.LastScannedAt.IsZero() {
		fmt.Printf("Last scanned: %s\n", result.Spec.LastScannedAt.Local().Format("2006-01-02 15:04"))
	}
	if result.Spec.MissingAt != nil {
		fmt.Printf("Missing: yes (since %s)\n", result.Spec.MissingAt.Local().Format("2006-01-02 15:04"))
	}

	fmt.Printf("\nBeads (%d):\n", len(result.Beads))
	if len(result.Beads) == 0 {
		fmt.Println("  none")
		return
	}

	sort.Slice(result.Beads, func(i, j int) bool {
		return result.Beads[i].ID < result.Beads[j].ID
	})

	for _, issue := range result.Beads {
		statusIcon := ui.RenderStatusIcon(string(issue.Status))
		specChanged := ""
		if issue.SpecChangedAt != nil {
			specChanged = ui.RenderWarn("● [SPEC CHANGED]")
		}
		if specChanged != "" {
			fmt.Printf("  %s %s %s %s\n", statusIcon, ui.RenderID(issue.ID), issue.Title, specChanged)
		} else {
			fmt.Printf("  %s %s %s\n", statusIcon, ui.RenderID(issue.ID), issue.Title)
		}
	}
}

func renderSpecCoverage(result rpc.SpecCoverageResult) {
	if jsonOutput {
		outputJSON(result)
		return
	}
	fmt.Printf("Total specs: %d\n", result.Total)
	fmt.Printf("With beads: %d\n", result.WithBeads)
	fmt.Printf("Without beads: %d\n", result.WithoutBeads)
	fmt.Printf("Missing specs: %d\n", result.Missing)
	fmt.Printf("With spec changes: %d\n", result.WithChangedBeads)
}

func renderSpecAudit(result rpc.SpecAuditResult) {
	if jsonOutput {
		outputJSON(result)
		return
	}

	if len(result.Entries) == 0 {
		fmt.Println("No specs found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tSPEC PATH\tISSUES\tCOMPLETION")

	for _, e := range result.Entries {
		// Status icon based on status
		var statusIcon string
		switch e.Status {
		case "pending":
			statusIcon = ui.RenderMuted("○") // pending
		case "in-progress":
			statusIcon = ui.RenderWarn("◐") // in-progress
		case "complete":
			statusIcon = ui.RenderPass("✓") // complete
		case "stale":
			statusIcon = ui.RenderMuted("❄") // stale
		default:
			statusIcon = "?"
		}

		issues := fmt.Sprintf("%d/%d", e.ClosedIssues, e.TotalIssues)
		completion := fmt.Sprintf("%.0f%%", e.Completion)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", statusIcon, e.SpecID, issues, completion)
	}
	_ = w.Flush()

	// Summary
	fmt.Println()
	fmt.Printf("Total: %d  Pending: %d  In-Progress: %d  Complete: %d  Stale: %d\n",
		result.Summary.TotalSpecs,
		result.Summary.PendingSpecs,
		result.Summary.InProgressSpecs,
		result.Summary.CompleteSpecs,
		result.Summary.StaleSpecs,
	)
}
