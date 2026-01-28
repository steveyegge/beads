package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var specSuggestCmd = &cobra.Command{
	Use:   "suggest <issue-id>",
	Short: "Suggest specs for an issue by title match",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		issueID := strings.TrimSpace(args[0])
		if issueID == "" {
			FatalErrorRespectJSON("issue_id is required")
		}
		limit, _ := cmd.Flags().GetInt("limit")
		threshold, _ := cmd.Flags().GetInt("threshold")
		validatePercent(threshold, "threshold")

		if daemonClient != nil {
			resp, err := daemonClient.SpecSuggest(&rpc.SpecSuggestArgs{
				IssueID:   issueID,
				Limit:     limit,
				Threshold: threshold,
			})
			if err != nil {
				FatalErrorRespectJSON("spec suggest failed: %v", err)
			}
			var result rpc.SpecSuggestResult
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				FatalErrorRespectJSON("invalid spec suggest response: %v", err)
			}
			renderSpecSuggest(result, threshold)
			return
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		specStore, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		issue, err := store.GetIssue(rootCtx, issueID)
		if err != nil {
			FatalErrorRespectJSON("get issue: %v", err)
		}
		if issue == nil {
			FatalErrorRespectJSON("issue not found: %s", issueID)
		}

		entries, err := specStore.ListSpecRegistry(rootCtx)
		if err != nil {
			FatalErrorRespectJSON("list spec registry: %v", err)
		}
		specs := make([]spec.SpecRegistryEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.MissingAt != nil {
				continue
			}
			specs = append(specs, entry)
		}

		result := rpc.SpecSuggestResult{
			IssueID:     issue.ID,
			IssueTitle:  issue.Title,
			CurrentSpec: issue.SpecID,
		}
		if issue.SpecID == "" {
			minScore := float64(threshold) / 100.0
			result.Suggestions = spec.SuggestSpecs(issue.Title, specs, limit, minScore)
		}
		renderSpecSuggest(result, threshold)
	},
}

var specLinkAutoCmd = &cobra.Command{
	Use:   "link --auto",
	Short: "Suggest or apply spec links for unlinked issues",
	Run: func(cmd *cobra.Command, _ []string) {
		auto, _ := cmd.Flags().GetBool("auto")
		if !auto {
			FatalErrorRespectJSON("use --auto to enable matching")
		}
		threshold, _ := cmd.Flags().GetInt("threshold")
		confirm, _ := cmd.Flags().GetBool("confirm")
		includeClosed, _ := cmd.Flags().GetBool("include-closed")
		maxIssues, _ := cmd.Flags().GetInt("max-issues")
		showSize, _ := cmd.Flags().GetBool("show-size")
		format, _ := cmd.Flags().GetString("format")
		if format == "" {
			format = "list"
		}
		format = strings.ToLower(format)
		if format != "list" && format != "table" {
			FatalErrorRespectJSON("--format must be one of: list, table")
		}
		validatePercent(threshold, "threshold")

		if daemonClient != nil {
			resp, err := daemonClient.SpecLinkAuto(&rpc.SpecLinkAutoArgs{
				Threshold:     threshold,
				Confirm:       confirm,
				IncludeClosed: includeClosed,
				MaxIssues:     maxIssues,
			})
			if err != nil {
				FatalErrorRespectJSON("spec link auto failed: %v", err)
			}
			var result rpc.SpecLinkAutoResult
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				FatalErrorRespectJSON("invalid spec link auto response: %v", err)
			}
			renderSpecLinkAuto(result, threshold, confirm, showSize, format)
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
		specs := make([]spec.SpecRegistryEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.MissingAt != nil {
				continue
			}
			specs = append(specs, entry)
		}

		filter := types.IssueFilter{NoSpec: true}
		if !includeClosed {
			filter.ExcludeStatus = []types.Status{types.StatusClosed}
		}
		if maxIssues > 0 {
			filter.Limit = maxIssues
		}
		issues, err := store.SearchIssues(rootCtx, "", filter)
		if err != nil {
			FatalErrorRespectJSON("list issues: %v", err)
		}

		minScore := float64(threshold) / 100.0
		result := rpc.SpecLinkAutoResult{
			TotalIssues: len(issues),
		}
		actor := getActor()

		for _, issue := range issues {
			if strings.TrimSpace(issue.Title) == "" {
				result.SkippedNoTitle++
				continue
			}
			match, ok := spec.BestSpecMatch(issue.Title, specs, minScore)
			if !ok {
				result.SkippedLowScore++
				continue
			}
			result.Matched++
			suggestion := rpc.SpecLinkAutoSuggestion{
				IssueID:    issue.ID,
				IssueTitle: issue.Title,
				SpecID:     match.SpecID,
				SpecTitle:  match.Title,
				Score:      match.Score,
			}
			if confirm {
				updates := map[string]interface{}{
					"spec_id": match.SpecID,
				}
				if err := store.UpdateIssue(rootCtx, issue.ID, updates, actor); err != nil {
					suggestion.Error = err.Error()
				} else {
					suggestion.Applied = true
					result.Applied++
				}
			}
			result.Suggestions = append(result.Suggestions, suggestion)
		}

		renderSpecLinkAuto(result, threshold, confirm, showSize, format)
	},
}

func init() {
	specSuggestCmd.Flags().Int("limit", 3, "Max suggestions to return")
	specSuggestCmd.Flags().Int("threshold", 40, "Minimum match score percent (0-100)")

	specLinkAutoCmd.Flags().Bool("auto", false, "Enable auto matching (required)")
	specLinkAutoCmd.Flags().Bool("confirm", false, "Apply suggested links")
	specLinkAutoCmd.Flags().Int("threshold", 80, "Minimum match score percent (0-100)")
	specLinkAutoCmd.Flags().Bool("include-closed", false, "Include closed issues")
	specLinkAutoCmd.Flags().Int("max-issues", 0, "Limit number of issues to process (0 = no limit)")
	specLinkAutoCmd.Flags().Bool("show-size", false, "Show spec size (lines/tokens) for matches")
	specLinkAutoCmd.Flags().String("format", "list", "Output format: list or table")

	specCmd.AddCommand(specSuggestCmd)
	specCmd.AddCommand(specLinkAutoCmd)
}

func validatePercent(value int, name string) {
	if value < 0 || value > 100 {
		FatalErrorRespectJSON("%s must be between 0 and 100", name)
	}
}

func renderSpecSuggest(result rpc.SpecSuggestResult, threshold int) {
	if jsonOutput {
		outputJSON(result)
		return
	}

	fmt.Printf("Issue: %s\n", result.IssueID)
	if result.IssueTitle != "" {
		fmt.Printf("Title: %s\n", result.IssueTitle)
	}
	if result.CurrentSpec != "" {
		fmt.Printf("Already linked to: %s\n", result.CurrentSpec)
		return
	}

	if len(result.Suggestions) == 0 {
		fmt.Printf("No suggestions >= %d%%\n", threshold)
		return
	}

	fmt.Printf("\nSuggestions (>= %d%%):\n", threshold)
	for i, match := range result.Suggestions {
		percent := int(match.Score * 100)
		title := match.Title
		if title == "" {
			title = "(no title)"
		}
		fmt.Printf("  %d. %s (%d%%) - %s\n", i+1, match.SpecID, percent, title)
	}
}

type specSize struct {
	Lines  int
	Tokens int
}

func renderSpecLinkAuto(result rpc.SpecLinkAutoResult, threshold int, confirm bool, showSize bool, format string) {
	if jsonOutput {
		outputJSON(result)
		return
	}

	action := "Preview"
	if confirm {
		action = "Applied"
	}
	fmt.Printf("%s: %d issues checked, %d matches >= %d%%\n", action, result.TotalIssues, result.Matched, threshold)
	if result.SkippedNoTitle > 0 || result.SkippedLowScore > 0 {
		fmt.Printf("Skipped: %d no title, %d below threshold\n", result.SkippedNoTitle, result.SkippedLowScore)
	}
	if len(result.Suggestions) == 0 {
		if !confirm {
			fmt.Println("No matches. Try lowering --threshold.")
		}
		return
	}

	repoRoot := ""
	if showSize {
		repoRoot = findSpecRepoRoot()
	}
	sizeCache := make(map[string]specSize)
	totalLines := 0
	totalTokens := 0

	fmt.Println()
	switch format {
	case "table":
		renderSpecLinkAutoTable(result, showSize, repoRoot, sizeCache, &totalLines, &totalTokens)
	default:
		for _, suggestion := range result.Suggestions {
			percent := int(suggestion.Score * 100)
			status := "○"
			if suggestion.Applied {
				status = ui.RenderPass("✓")
			}
			line := fmt.Sprintf("%s %s -> %s (%d%%)", status, suggestion.IssueID, suggestion.SpecID, percent)
			if suggestion.IssueTitle != "" {
				line = fmt.Sprintf("%s \"%s\"", line, suggestion.IssueTitle)
			}
			if suggestion.Error != "" {
				line = fmt.Sprintf("%s [error: %s]", line, suggestion.Error)
			}
			if showSize {
				if size, ok := getSpecSize(repoRoot, suggestion.SpecID, sizeCache); ok {
					line = fmt.Sprintf("%s | %d lines | ~%dk tokens", line, size.Lines, size.Tokens/1000)
					totalLines += size.Lines
					totalTokens += size.Tokens
				}
			}
			fmt.Fprintln(os.Stdout, line)
		}
	}

	if showSize && totalLines > 0 {
		linked := result.Matched
		if confirm {
			linked = result.Applied
		}
		fmt.Printf("\nSummary: %d specs linked, total %d lines (~%dk tokens)\n", linked, totalLines, totalTokens/1000)
	}

	if !confirm {
		fmt.Println("\nRun with --confirm to apply these links.")
	}
}

func renderSpecLinkAutoTable(result rpc.SpecLinkAutoResult, showSize bool, repoRoot string, cache map[string]specSize, totalLines *int, totalTokens *int) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showSize {
		fmt.Fprintln(w, "STATUS\tISSUE ID\tSPEC ID\tSCORE%\tLINES\tTOKENS\tTITLE")
	} else {
		fmt.Fprintln(w, "STATUS\tISSUE ID\tSPEC ID\tSCORE%\tTITLE")
	}
	for _, suggestion := range result.Suggestions {
		status := "○"
		if suggestion.Applied {
			status = ui.RenderPass("✓")
		}
		score := int(suggestion.Score * 100)
		title := suggestion.IssueTitle
		if title == "" {
			title = "(no title)"
		}
		if showSize {
			lines := ""
			tokens := ""
			if size, ok := getSpecSize(repoRoot, suggestion.SpecID, cache); ok {
				lines = fmt.Sprintf("%d", size.Lines)
				tokens = fmt.Sprintf("~%dk", size.Tokens/1000)
				*totalLines += size.Lines
				*totalTokens += size.Tokens
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n", status, suggestion.IssueID, suggestion.SpecID, score, lines, tokens, title)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", status, suggestion.IssueID, suggestion.SpecID, score, title)
		}
	}
	_ = w.Flush()
}

func findSpecRepoRoot() string {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return ""
	}
	return filepath.Dir(beadsDir)
}

func getSpecSize(repoRoot, specID string, cache map[string]specSize) (specSize, bool) {
	if !spec.IsScannableSpecID(specID) {
		return specSize{}, false
	}
	if size, ok := cache[specID]; ok {
		return size, true
	}
	if repoRoot == "" {
		return specSize{}, false
	}
	path := specID
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, specID)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return specSize{}, false
	}
	lines := 0
	if len(content) > 0 {
		lines = len(strings.Split(string(content), "\n"))
	}
	tokens := len(strings.Fields(string(content)))
	size := specSize{Lines: lines, Tokens: tokens}
	cache[specID] = size
	return size, true
}
