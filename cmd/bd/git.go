package main

import (
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
  bd git unlink <issue-id> <commit-sha>  Remove a commit link`,
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

func init() {
	gitCmd.AddCommand(gitLinkCmd)
	gitCmd.AddCommand(gitUnlinkCmd)
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
