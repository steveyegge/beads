package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/ui"
)

// getDoltStore returns the store as a DoltStore, or an error if not using Dolt backend
func getDoltStore() (*dolt.DoltStore, error) {
	ds, ok := store.(*dolt.DoltStore)
	if !ok {
		return nil, fmt.Errorf("dolt commands require --backend dolt (current backend is sqlite)")
	}
	return ds, nil
}

var doltCmd = &cobra.Command{
	Use:     "dolt",
	GroupID: "sync",
	Short:   "Dolt version control operations",
	Long: `Manage Dolt database versioning.

Dolt provides Git-like version control for your beads database. You can
commit changes, create branches, merge, push to remotes, and view history.

Prerequisites:
  - Initialize with: bd init --backend dolt
  - Dolt must be installed (https://doltdb.com)

Examples:
  bd dolt status                    # Show pending changes
  bd dolt commit -m "Add features"  # Commit all changes
  bd dolt log                       # Show commit history
  bd dolt branch feature-1          # Create new branch
  bd dolt checkout feature-1        # Switch to branch
  bd dolt merge feature-1           # Merge branch`,
}

// ============================================================================
// Status Command
// ============================================================================

var doltStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show pending database changes",
	Run: func(_ *cobra.Command, _ []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		status, err := ds.Status(rootCtx)
		if err != nil {
			FatalError("failed to get status: %v", err)
		}

		if len(status.Staged) == 0 && len(status.Unstaged) == 0 {
			fmt.Println("No changes to commit")
			return
		}

		if len(status.Staged) > 0 {
			fmt.Println("Staged changes:")
			for _, e := range status.Staged {
				fmt.Printf("  %s %s\n", ui.RenderPass(e.Status), e.Table)
			}
		}

		if len(status.Unstaged) > 0 {
			if len(status.Staged) > 0 {
				fmt.Println()
			}
			fmt.Println("Unstaged changes:")
			for _, e := range status.Unstaged {
				fmt.Printf("  %s %s\n", ui.RenderWarn(e.Status), e.Table)
			}
		}
	},
}

// ============================================================================
// Commit Command
// ============================================================================

var doltCommitMessage string

var doltCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit all pending changes",
	Long: `Commit all pending changes to the Dolt database.

This stages all changes and creates a new commit. The commit message
is required and should describe what changed.

Examples:
  bd dolt commit -m "Add user authentication feature"
  bd dolt commit --message "Fix bug in issue creation"`,
	Run: func(_ *cobra.Command, _ []string) {
		if doltCommitMessage == "" {
			FatalError("commit message is required (-m)")
		}

		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		if err := ds.Commit(rootCtx, doltCommitMessage); err != nil {
			FatalError("failed to commit: %v", err)
		}

		fmt.Printf("%s Committed: %s\n", ui.RenderPass("✓"), doltCommitMessage)
	},
}

// ============================================================================
// Log Command
// ============================================================================

var doltLogLimit int

var doltLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show commit history",
	Long: `Show recent commit history for the Dolt database.

Use -n to limit the number of commits shown (default: 10).

Examples:
  bd dolt log        # Show last 10 commits
  bd dolt log -n 5   # Show last 5 commits
  bd dolt log -n 50  # Show last 50 commits`,
	Run: func(_ *cobra.Command, _ []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		commits, err := ds.Log(rootCtx, doltLogLimit)
		if err != nil {
			FatalError("failed to get log: %v", err)
		}

		if len(commits) == 0 {
			fmt.Println("No commits yet")
			return
		}

		for _, c := range commits {
			fmt.Printf("%s %s\n", ui.RenderAccent(c.Hash[:8]), c.Message)
			fmt.Printf("    Author: %s <%s>\n", c.Author, c.Email)
			fmt.Printf("    Date:   %s\n\n", c.Date.Format("2006-01-02 15:04:05"))
		}
	},
}

// ============================================================================
// Branch Commands
// ============================================================================

var doltBranchCmd = &cobra.Command{
	Use:   "branch [name]",
	Short: "List or create branches",
	Long: `List branches or create a new branch.

Without arguments, shows the current branch.
With a branch name, creates a new branch.

Examples:
  bd dolt branch              # Show current branch
  bd dolt branch feature-1    # Create new branch`,
	Run: func(_ *cobra.Command, args []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		if len(args) == 0 {
			// Show current branch
			branch, err := ds.CurrentBranch(rootCtx)
			if err != nil {
				FatalError("failed to get current branch: %v", err)
			}
			fmt.Printf("* %s\n", ui.RenderAccent(branch))
			return
		}

		// Create new branch
		branchName := args[0]
		if err := ds.Branch(rootCtx, branchName); err != nil {
			FatalError("failed to create branch: %v", err)
		}

		fmt.Printf("%s Created branch: %s\n", ui.RenderPass("✓"), branchName)
	},
}

var doltCheckoutCmd = &cobra.Command{
	Use:   "checkout <branch>",
	Short: "Switch to a branch",
	Args:  cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		branch := args[0]
		if err := ds.Checkout(rootCtx, branch); err != nil {
			FatalError("failed to checkout: %v", err)
		}

		fmt.Printf("%s Switched to branch: %s\n", ui.RenderPass("✓"), branch)
	},
}

var doltMergeCmd = &cobra.Command{
	Use:   "merge <branch>",
	Short: "Merge a branch into current branch",
	Args:  cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		branch := args[0]
		if err := ds.Merge(rootCtx, branch); err != nil {
			FatalError("failed to merge: %v", err)
		}

		fmt.Printf("%s Merged branch: %s\n", ui.RenderPass("✓"), branch)
	},
}

// ============================================================================
// Remote Commands
// ============================================================================

var doltPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push commits to remote",
	Run: func(_ *cobra.Command, _ []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		if err := ds.Push(rootCtx); err != nil {
			FatalError("failed to push: %v", err)
		}

		fmt.Printf("%s Pushed to remote\n", ui.RenderPass("✓"))
	},
}

var doltPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull changes from remote",
	Run: func(_ *cobra.Command, _ []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		if err := ds.Pull(rootCtx); err != nil {
			FatalError("failed to pull: %v", err)
		}

		fmt.Printf("%s Pulled from remote\n", ui.RenderPass("✓"))
	},
}

var doltRemoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage remotes",
	Long: `Manage Dolt remotes for syncing with other databases.

Dolt remotes work like Git remotes, allowing you to push and pull
changes between databases.

Examples:
  bd dolt remote add origin https://doltremoteapi.dolthub.com/org/repo
  bd dolt remote add backup file:///backup/beads`,
}

var doltRemoteAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a remote",
	Args:  cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		name, url := args[0], args[1]
		if err := ds.AddRemote(rootCtx, name, url); err != nil {
			FatalError("failed to add remote: %v", err)
		}

		fmt.Printf("%s Added remote: %s -> %s\n", ui.RenderPass("✓"), name, url)
	},
}

// ============================================================================
// Diff Command
// ============================================================================

var doltDiffCmd = &cobra.Command{
	Use:   "diff [from-ref] [to-ref]",
	Short: "Show changes between commits",
	Long: `Show changes between two commits or refs.

Without arguments, shows uncommitted changes.
With one argument, shows changes between that ref and HEAD.
With two arguments, shows changes between the two refs.

Examples:
  bd dolt diff                        # Uncommitted changes
  bd dolt diff abc123                 # Changes from abc123 to HEAD
  bd dolt diff abc123 def456          # Changes between two commits`,
	Run: func(_ *cobra.Command, args []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		var fromRef, toRef string
		switch len(args) {
		case 0:
			fromRef, toRef = "HEAD", "WORKING"
		case 1:
			fromRef, toRef = args[0], "HEAD"
		case 2:
			fromRef, toRef = args[0], args[1]
		default:
			FatalError("too many arguments")
		}

		entries, err := ds.GetDiff(rootCtx, fromRef, toRef)
		if err != nil {
			FatalError("failed to get diff: %v", err)
		}

		if len(entries) == 0 {
			fmt.Println("No differences")
			return
		}

		for _, e := range entries {
			var symbol string
			switch e.DiffType {
			case "added":
				symbol = ui.RenderPass("+")
			case "modified":
				symbol = ui.RenderWarn("~")
			case "removed":
				symbol = ui.RenderFail("-")
			default:
				symbol = "?"
			}
			fmt.Printf("%s %s\n", symbol, e.TableName)
		}
	},
}

// ============================================================================
// History Command
// ============================================================================

var doltHistoryCmd = &cobra.Command{
	Use:   "history <issue-id>",
	Short: "Show change history for an issue",
	Long: `Show the complete change history for a specific issue.

This shows all commits that modified the issue, with the issue's
state at each point in time.

Examples:
  bd dolt history bd-123
  bd dolt history my-456`,
	Args: cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		issueID := args[0]
		history, err := ds.GetIssueHistory(rootCtx, issueID)
		if err != nil {
			FatalError("failed to get history: %v", err)
		}

		if len(history) == 0 {
			fmt.Printf("No history found for issue %s\n", issueID)
			return
		}

		for _, h := range history {
			fmt.Printf("%s %s\n", ui.RenderAccent(h.CommitHash[:8]), h.CommitDate.Format("2006-01-02 15:04:05"))
			fmt.Printf("    Status: %s, Priority: P%d\n", h.Issue.Status, h.Issue.Priority)
			if h.Issue.Title != "" {
				title := h.Issue.Title
				if len(title) > 60 {
					title = title[:57] + "..."
				}
				fmt.Printf("    Title: %s\n", title)
			}
			fmt.Println()
		}
	},
}

// ============================================================================
// Show Command (issue at ref)
// ============================================================================

var doltShowCmd = &cobra.Command{
	Use:   "show <issue-id> <ref>",
	Short: "Show an issue at a specific commit",
	Long: `Show an issue as it existed at a specific commit or branch.

This lets you see the historical state of an issue at any point
in the database's version history.

Examples:
  bd dolt show bd-123 abc123       # Issue at commit abc123
  bd dolt show bd-123 main         # Issue on main branch
  bd dolt show bd-123 feature-1    # Issue on feature branch`,
	Args: cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		issueID, ref := args[0], args[1]
		issue, err := ds.GetIssueAsOf(rootCtx, issueID, ref)
		if err != nil {
			FatalError("failed to get issue: %v", err)
		}

		if issue == nil {
			fmt.Printf("Issue %s not found at ref %s\n", issueID, ref)
			os.Exit(1)
		}

		// Display issue details
		fmt.Printf("%s · %s   [P%d · %s]\n", issue.ID, issue.Title, issue.Priority, strings.ToUpper(string(issue.Status)))
		if issue.Description != "" {
			fmt.Printf("\n%s\n", issue.Description)
		}
	},
}

// ============================================================================
// Conflicts Commands
// ============================================================================

var doltConflictsCmd = &cobra.Command{
	Use:   "conflicts",
	Short: "List merge conflicts",
	Run: func(_ *cobra.Command, _ []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		conflicts, err := ds.GetConflicts(rootCtx)
		if err != nil {
			FatalError("failed to get conflicts: %v", err)
		}

		if len(conflicts) == 0 {
			fmt.Println("No conflicts")
			return
		}

		fmt.Println("Merge conflicts:")
		for _, c := range conflicts {
			fmt.Printf("  %s: %d conflict(s)\n", c.TableName, c.NumConflicts)
		}
		fmt.Println()
		fmt.Println("Resolve with: bd dolt resolve <table> --ours|--theirs")
	},
}

var resolveOurs, resolveTheirs bool

var doltResolveCmd = &cobra.Command{
	Use:   "resolve <table>",
	Short: "Resolve merge conflicts",
	Long: `Resolve merge conflicts for a table using a strategy.

Strategies:
  --ours    Keep our version (current branch)
  --theirs  Keep their version (merged branch)

Examples:
  bd dolt resolve issues --ours
  bd dolt resolve issues --theirs`,
	Args: cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		ds, err := getDoltStore()
		if err != nil {
			FatalError("%v", err)
		}

		table := args[0]

		var strategy string
		if resolveOurs && resolveTheirs {
			FatalError("specify only one of --ours or --theirs")
		} else if resolveOurs {
			strategy = "ours"
		} else if resolveTheirs {
			strategy = "theirs"
		} else {
			FatalError("specify --ours or --theirs")
		}

		if err := ds.ResolveConflicts(rootCtx, table, strategy); err != nil {
			FatalError("failed to resolve conflicts: %v", err)
		}

		fmt.Printf("%s Resolved conflicts in %s using %s\n", ui.RenderPass("✓"), table, strategy)
	},
}

// ============================================================================
// Init
// ============================================================================

func init() {
	// Commit flags
	doltCommitCmd.Flags().StringVarP(&doltCommitMessage, "message", "m", "", "Commit message (required)")

	// Log flags
	doltLogCmd.Flags().IntVarP(&doltLogLimit, "number", "n", 10, "Number of commits to show")

	// Resolve flags
	doltResolveCmd.Flags().BoolVar(&resolveOurs, "ours", false, "Keep our version")
	doltResolveCmd.Flags().BoolVar(&resolveTheirs, "theirs", false, "Keep their version")

	// Register remote subcommands
	doltRemoteCmd.AddCommand(doltRemoteAddCmd)

	// Register all subcommands
	doltCmd.AddCommand(doltStatusCmd)
	doltCmd.AddCommand(doltCommitCmd)
	doltCmd.AddCommand(doltLogCmd)
	doltCmd.AddCommand(doltBranchCmd)
	doltCmd.AddCommand(doltCheckoutCmd)
	doltCmd.AddCommand(doltMergeCmd)
	doltCmd.AddCommand(doltPushCmd)
	doltCmd.AddCommand(doltPullCmd)
	doltCmd.AddCommand(doltDiffCmd)
	doltCmd.AddCommand(doltHistoryCmd)
	doltCmd.AddCommand(doltShowCmd)
	doltCmd.AddCommand(doltConflictsCmd)
	doltCmd.AddCommand(doltResolveCmd)
	doltCmd.AddCommand(doltRemoteCmd)

	rootCmd.AddCommand(doltCmd)
}
