package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var gitCmd = &cobra.Command{
	Use:     "git",
	GroupID: "advanced",
	Short:   "Git commit tracking commands",
	Long: `Commands for linking git commits to issues.

Track which commits are associated with which issues:
  bd git link <issue-id> <commit-sha>    Link a commit to an issue
  bd git unlink <issue-id> <commit-sha>  Remove a commit link
  bd git scan <issue-id>                 Auto-link commits mentioning [issue-id]`,
}

var gitLinkCmd = &cobra.Command{
	Use:   "link <issue-id> <commit-sha>",
	Short: "Link a git commit to an issue",
	Long: `Link a git commit SHA to an issue.

This creates a record that the specified commit is associated with the issue.
Useful for tracking which commits implement which issues.

Examples:
  bd git link bd-abc 7a1b2c3       # Link commit to issue
  bd git link bd-abc HEAD         # Link current HEAD commit
  bd git link bd-abc HEAD~1       # Link parent of HEAD`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("git link")

		issueID := args[0]
		commitRef := args[1]

		// Resolve commit reference to full SHA
		commitSHA, err := resolveCommitRef(commitRef)
		if err != nil {
			FatalErrorRespectJSON("resolving commit ref %q: %v", commitRef, err)
		}

		// Validate SHA format (must be at least 7 hex chars)
		if !isValidCommitSHA(commitSHA) {
			FatalErrorRespectJSON("invalid commit SHA: %s", commitSHA)
		}

		ctx := rootCtx

		// Handle daemon mode
		if daemonClient != nil {
			resolveArgs := &rpc.ResolveIDArgs{ID: issueID}
			resp, err := daemonClient.ResolveID(resolveArgs)
			if err != nil {
				FatalErrorRespectJSON("resolving ID %s: %v", issueID, err)
			}
			var resolvedID string
			if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
				FatalErrorRespectJSON("unmarshaling resolved ID: %v", err)
			}

			// Get current issue to read existing commits
			showArgs := &rpc.ShowArgs{ID: resolvedID}
			showResp, err := daemonClient.Show(showArgs)
			if err != nil {
				FatalErrorRespectJSON("getting issue %s: %v", resolvedID, err)
			}
			var issue types.Issue
			if err := json.Unmarshal(showResp.Data, &issue); err != nil {
				FatalErrorRespectJSON("unmarshaling issue: %v", err)
			}

			// Check if already linked
			if slices.Contains(issue.Commits, commitSHA) {
				if jsonOutput {
					outputJSON(map[string]interface{}{
						"issue_id": resolvedID,
						"commit":   commitSHA,
						"status":   "already_linked",
					})
				} else {
					fmt.Printf("Commit %s is already linked to %s\n", commitSHA[:7], resolvedID)
				}
				return
			}

			// Add commit and update
			newCommits := append(issue.Commits, commitSHA)
			commitsJSON, _ := json.Marshal(newCommits)

			updateArgs := &rpc.UpdateArgs{ID: resolvedID}
			commits := string(commitsJSON)
			updateArgs.Commits = &commits

			_, err = daemonClient.Update(updateArgs)
			if err != nil {
				FatalErrorRespectJSON("updating issue %s: %v", resolvedID, err)
			}

			if jsonOutput {
				outputJSON(map[string]interface{}{
					"issue_id": resolvedID,
					"commit":   commitSHA,
					"status":   "linked",
				})
			} else {
				fmt.Printf("%s Linked commit %s to %s\n", ui.RenderPass("✓"), commitSHA[:7], resolvedID)
			}
			return
		}

		// Direct mode
		result, err := resolveAndGetIssueWithRouting(ctx, store, issueID)
		if err != nil {
			FatalErrorRespectJSON("resolving %s: %v", issueID, err)
		}
		if result == nil || result.Issue == nil {
			FatalErrorRespectJSON("issue %s not found", issueID)
		}
		defer result.Close()

		issue := result.Issue
		resolvedID := result.ResolvedID
		issueStore := result.Store

		// Check if already linked
		if slices.Contains(issue.Commits, commitSHA) {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"issue_id": resolvedID,
					"commit":   commitSHA,
					"status":   "already_linked",
				})
			} else {
				fmt.Printf("Commit %s is already linked to %s\n", commitSHA[:7], resolvedID)
			}
			return
		}

		// Add commit and update
		newCommits := append(issue.Commits, commitSHA)
		commitsJSON, _ := json.Marshal(newCommits)

		updates := map[string]interface{}{
			"commits": string(commitsJSON),
		}
		if err := issueStore.UpdateIssue(ctx, resolvedID, updates, actor); err != nil {
			FatalErrorRespectJSON("updating issue %s: %v", resolvedID, err)
		}

		markDirtyAndScheduleFlush()

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"issue_id": resolvedID,
				"commit":   commitSHA,
				"status":   "linked",
			})
		} else {
			fmt.Printf("%s Linked commit %s to %s\n", ui.RenderPass("✓"), commitSHA[:7], resolvedID)
		}
	},
}

var gitUnlinkCmd = &cobra.Command{
	Use:   "unlink <issue-id> <commit-sha>",
	Short: "Remove a commit link from an issue",
	Long: `Remove a git commit link from an issue.

Examples:
  bd git unlink bd-abc 7a1b2c3    # Remove commit link`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("git unlink")

		issueID := args[0]
		commitRef := args[1]

		// Resolve commit reference to full SHA
		commitSHA, err := resolveCommitRef(commitRef)
		if err != nil {
			FatalErrorRespectJSON("resolving commit ref %q: %v", commitRef, err)
		}

		ctx := rootCtx

		// Handle daemon mode
		if daemonClient != nil {
			resolveArgs := &rpc.ResolveIDArgs{ID: issueID}
			resp, err := daemonClient.ResolveID(resolveArgs)
			if err != nil {
				FatalErrorRespectJSON("resolving ID %s: %v", issueID, err)
			}
			var resolvedID string
			if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
				FatalErrorRespectJSON("unmarshaling resolved ID: %v", err)
			}

			// Get current issue to read existing commits
			showArgs := &rpc.ShowArgs{ID: resolvedID}
			showResp, err := daemonClient.Show(showArgs)
			if err != nil {
				FatalErrorRespectJSON("getting issue %s: %v", resolvedID, err)
			}
			var issue types.Issue
			if err := json.Unmarshal(showResp.Data, &issue); err != nil {
				FatalErrorRespectJSON("unmarshaling issue: %v", err)
			}

			// Find commit (support partial SHA matching)
			matchedSHA := findMatchingCommit(issue.Commits, commitSHA)
			if matchedSHA == "" {
				if jsonOutput {
					outputJSON(map[string]interface{}{
						"issue_id": resolvedID,
						"commit":   commitSHA,
						"status":   "not_found",
					})
				} else {
					fmt.Printf("Commit %s is not linked to %s\n", commitSHA[:min(7, len(commitSHA))], resolvedID)
				}
				return
			}

			// Remove commit and update
			newCommits := slices.DeleteFunc(issue.Commits, func(c string) bool {
				return c == matchedSHA
			})
			commitsJSON, _ := json.Marshal(newCommits)

			updateArgs := &rpc.UpdateArgs{ID: resolvedID}
			commits := string(commitsJSON)
			updateArgs.Commits = &commits

			_, err = daemonClient.Update(updateArgs)
			if err != nil {
				FatalErrorRespectJSON("updating issue %s: %v", resolvedID, err)
			}

			if jsonOutput {
				outputJSON(map[string]interface{}{
					"issue_id": resolvedID,
					"commit":   matchedSHA,
					"status":   "unlinked",
				})
			} else {
				fmt.Printf("%s Unlinked commit %s from %s\n", ui.RenderPass("✓"), matchedSHA[:7], resolvedID)
			}
			return
		}

		// Direct mode
		result, err := resolveAndGetIssueWithRouting(ctx, store, issueID)
		if err != nil {
			FatalErrorRespectJSON("resolving %s: %v", issueID, err)
		}
		if result == nil || result.Issue == nil {
			FatalErrorRespectJSON("issue %s not found", issueID)
		}
		defer result.Close()

		issue := result.Issue
		resolvedID := result.ResolvedID
		issueStore := result.Store

		// Find commit (support partial SHA matching)
		matchedSHA := findMatchingCommit(issue.Commits, commitSHA)
		if matchedSHA == "" {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"issue_id": resolvedID,
					"commit":   commitSHA,
					"status":   "not_found",
				})
			} else {
				fmt.Printf("Commit %s is not linked to %s\n", commitSHA[:min(7, len(commitSHA))], resolvedID)
			}
			return
		}

		// Remove commit and update
		newCommits := slices.DeleteFunc(issue.Commits, func(c string) bool {
			return c == matchedSHA
		})
		commitsJSON, _ := json.Marshal(newCommits)

		updates := map[string]interface{}{
			"commits": string(commitsJSON),
		}
		if err := issueStore.UpdateIssue(ctx, resolvedID, updates, actor); err != nil {
			FatalErrorRespectJSON("updating issue %s: %v", resolvedID, err)
		}

		markDirtyAndScheduleFlush()

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"issue_id": resolvedID,
				"commit":   matchedSHA,
				"status":   "unlinked",
			})
		} else {
			fmt.Printf("%s Unlinked commit %s from %s\n", ui.RenderPass("✓"), matchedSHA[:7], resolvedID)
		}
	},
}

var gitScanCmd = &cobra.Command{
	Use:   "scan <issue-id>",
	Short: "Auto-link commits that reference the issue",
	Long: `Scan git history for commits that reference the issue ID in their message.

Searches for commits containing [issue-id] (e.g., [bd-abc]) in the commit
message and automatically links them to the issue.

Examples:
  bd git scan bd-abc           # Find and link commits mentioning [bd-abc]
  bd git scan bd-abc --dry-run # Show what would be linked without linking`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("git scan")

		issueID := args[0]
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		ctx := rootCtx

		// Resolve issue ID first
		var resolvedID string
		var issue *types.Issue
		var issueStore interface {
			UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error
		}
		var closeFunc func()

		if daemonClient != nil {
			resolveArgs := &rpc.ResolveIDArgs{ID: issueID}
			resp, err := daemonClient.ResolveID(resolveArgs)
			if err != nil {
				FatalErrorRespectJSON("resolving ID %s: %v", issueID, err)
			}
			if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
				FatalErrorRespectJSON("unmarshaling resolved ID: %v", err)
			}

			// Get current issue to read existing commits
			showArgs := &rpc.ShowArgs{ID: resolvedID}
			showResp, err := daemonClient.Show(showArgs)
			if err != nil {
				FatalErrorRespectJSON("getting issue %s: %v", resolvedID, err)
			}
			var issueData types.Issue
			if err := json.Unmarshal(showResp.Data, &issueData); err != nil {
				FatalErrorRespectJSON("unmarshaling issue: %v", err)
			}
			issue = &issueData
			closeFunc = func() {}
		} else {
			result, err := resolveAndGetIssueWithRouting(ctx, store, issueID)
			if err != nil {
				FatalErrorRespectJSON("resolving %s: %v", issueID, err)
			}
			if result == nil || result.Issue == nil {
				FatalErrorRespectJSON("issue %s not found", issueID)
			}
			issue = result.Issue
			resolvedID = result.ResolvedID
			issueStore = result.Store
			closeFunc = result.Close
		}
		defer closeFunc()

		// Search git log for commits mentioning this issue
		// Use --fixed-strings to match literal [issue-id] pattern
		pattern := fmt.Sprintf("[%s]", resolvedID)
		gitCmd := exec.Command("git", "log", "--all", "--format=%H", "--fixed-strings", "--grep="+pattern)
		out, err := gitCmd.Output()
		if err != nil {
			// git log returns non-zero if no matches found (with some versions)
			// Check if it's just empty output
			if len(out) == 0 {
				if jsonOutput {
					outputJSON(map[string]interface{}{
						"issue_id":    resolvedID,
						"found":       0,
						"linked":      0,
						"already":     0,
						"commits":     []string{},
					})
				} else {
					fmt.Printf("No commits found referencing [%s]\n", resolvedID)
				}
				return
			}
			FatalErrorRespectJSON("searching git log: %v", err)
		}

		// Parse the commit SHAs
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var foundSHAs []string
		for _, line := range lines {
			sha := strings.TrimSpace(line)
			if sha != "" && isValidCommitSHA(sha) {
				foundSHAs = append(foundSHAs, sha)
			}
		}

		if len(foundSHAs) == 0 {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"issue_id":    resolvedID,
					"found":       0,
					"linked":      0,
					"already":     0,
					"commits":     []string{},
				})
			} else {
				fmt.Printf("No commits found referencing [%s]\n", resolvedID)
			}
			return
		}

		// Determine which are new vs already linked
		var newSHAs []string
		var alreadyLinked []string
		for _, sha := range foundSHAs {
			if slices.Contains(issue.Commits, sha) {
				alreadyLinked = append(alreadyLinked, sha)
			} else {
				newSHAs = append(newSHAs, sha)
			}
		}

		if dryRun {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"issue_id":    resolvedID,
					"dry_run":     true,
					"found":       len(foundSHAs),
					"would_link":  len(newSHAs),
					"already":     len(alreadyLinked),
					"commits":     newSHAs,
				})
			} else {
				fmt.Printf("Found %d commit(s) referencing [%s]\n", len(foundSHAs), resolvedID)
				if len(alreadyLinked) > 0 {
					fmt.Printf("  Already linked: %d\n", len(alreadyLinked))
				}
				if len(newSHAs) > 0 {
					fmt.Printf("  Would link: %d\n", len(newSHAs))
					for _, sha := range newSHAs {
						// Get commit message for display
						msgCmd := exec.Command("git", "log", "-1", "--format=%s", sha)
						msgOut, _ := msgCmd.Output()
						msg := strings.TrimSpace(string(msgOut))
						if len(msg) > 60 {
							msg = msg[:57] + "..."
						}
						fmt.Printf("    %s %s\n", sha[:7], msg)
					}
				} else {
					fmt.Printf("  All commits already linked\n")
				}
			}
			return
		}

		// Link the new commits
		if len(newSHAs) == 0 {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"issue_id":    resolvedID,
					"found":       len(foundSHAs),
					"linked":      0,
					"already":     len(alreadyLinked),
					"commits":     []string{},
				})
			} else {
				fmt.Printf("Found %d commit(s), all already linked to %s\n", len(foundSHAs), resolvedID)
			}
			return
		}

		// Update the issue with new commits
		newCommits := append(issue.Commits, newSHAs...)
		commitsJSON, _ := json.Marshal(newCommits)

		if daemonClient != nil {
			updateArgs := &rpc.UpdateArgs{ID: resolvedID}
			commits := string(commitsJSON)
			updateArgs.Commits = &commits

			_, err = daemonClient.Update(updateArgs)
			if err != nil {
				FatalErrorRespectJSON("updating issue %s: %v", resolvedID, err)
			}
		} else {
			updates := map[string]interface{}{
				"commits": string(commitsJSON),
			}
			if err := issueStore.UpdateIssue(ctx, resolvedID, updates, actor); err != nil {
				FatalErrorRespectJSON("updating issue %s: %v", resolvedID, err)
			}
			markDirtyAndScheduleFlush()
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"issue_id":    resolvedID,
				"found":       len(foundSHAs),
				"linked":      len(newSHAs),
				"already":     len(alreadyLinked),
				"commits":     newSHAs,
			})
		} else {
			fmt.Printf("%s Linked %d commit(s) to %s\n", ui.RenderPass("✓"), len(newSHAs), resolvedID)
			for _, sha := range newSHAs {
				// Get commit message for display
				msgCmd := exec.Command("git", "log", "-1", "--format=%s", sha)
				msgOut, _ := msgCmd.Output()
				msg := strings.TrimSpace(string(msgOut))
				if len(msg) > 60 {
					msg = msg[:57] + "..."
				}
				fmt.Printf("  %s %s\n", sha[:7], msg)
			}
			if len(alreadyLinked) > 0 {
				fmt.Printf("  (%d already linked)\n", len(alreadyLinked))
			}
		}
	},
}

func init() {
	gitScanCmd.Flags().Bool("dry-run", false, "Show what would be linked without linking")

	gitCmd.AddCommand(gitLinkCmd)
	gitCmd.AddCommand(gitUnlinkCmd)
	gitCmd.AddCommand(gitScanCmd)
	rootCmd.AddCommand(gitCmd)
}

// resolveCommitRef resolves a git commit reference (HEAD, branch name, partial SHA) to full SHA
func resolveCommitRef(ref string) (string, error) {
	// If it looks like a full SHA already, return it
	if isValidCommitSHA(ref) && len(ref) == 40 {
		return ref, nil
	}

	// Use git rev-parse to resolve the reference
	cmd := exec.Command("git", "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", fmt.Errorf("could not resolve ref %q", ref)
	}
	return sha, nil
}

// isValidCommitSHA checks if a string is a valid git commit SHA (7-40 hex chars)
func isValidCommitSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	matched, _ := regexp.MatchString("^[a-fA-F0-9]+$", s)
	return matched
}

// findMatchingCommit finds a commit SHA that matches the given prefix
func findMatchingCommit(commits []string, prefix string) string {
	prefix = strings.ToLower(prefix)
	for _, commit := range commits {
		if strings.HasPrefix(strings.ToLower(commit), prefix) {
			return commit
		}
	}
	return ""
}
