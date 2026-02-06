package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/comment"
)

var codecommentCmd = &cobra.Command{
	Use:     "codecomment",
	Aliases: []string{"cc"},
	GroupID: "views",
	Short:   "First-class comment tracking: scan, drift detection, and link validation",
	Long: `Treat code comments as first-class tracked entities.

Scan Go source files to build a comment graph, detect broken references,
find stale comments where code changed but comments didn't, and track
expired TODOs.

Examples:
  bd codecomment scan                # Scan current repo
  bd codecomment scan ./internal/    # Scan specific directory
  bd codecomment drift               # Detect all comment drift
  bd codecomment links               # Show reference graph
  bd codecomment links --broken      # Show only broken references`,
}

var ccScanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan Go source files and build comment graph",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		root := "."
		if len(args) > 0 {
			root = args[0]
		}
		root, _ = filepath.Abs(root)

		// Scan.
		result, err := comment.ScanDir(root)
		if err != nil {
			FatalErrorRespectJSON("scanning comments: %v", err)
		}

		// Validate references.
		comment.ValidateReferences(root, result)

		// Store in graph DB.
		dbPath := filepath.Join(root, ".beads", "comments.db")
		graph, err := comment.OpenGraph(dbPath)
		if err != nil {
			FatalErrorRespectJSON("opening comment graph: %v", err)
		}
		defer graph.Close()

		if err := graph.StoreScanResult(result); err != nil {
			FatalErrorRespectJSON("storing scan result: %v", err)
		}

		if jsonOutput {
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return
		}

		// Human-readable output.
		fmt.Println("\nScanning comments...")
		fmt.Printf("  ├─ %d comments found", len(result.Nodes))
		if len(result.ByKind) > 0 {
			parts := []string{}
			for _, k := range []comment.Kind{comment.KindDoc, comment.KindTodo, comment.KindInvariant, comment.KindReference, comment.KindInline} {
				if c, ok := result.ByKind[k]; ok && c > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", c, k))
				}
			}
			fmt.Printf(" (%s)", strings.Join(parts, ", "))
		}
		fmt.Println()

		// Count total refs.
		totalRefs := 0
		for _, n := range result.Nodes {
			totalRefs += len(n.References)
		}
		if totalRefs > 0 {
			fmt.Printf("  ├─ %d cross-references detected\n", totalRefs)
		}
		if result.BrokenRefs > 0 {
			fmt.Printf("  ├─ %d broken references found\n", result.BrokenRefs)
		}
		fmt.Printf("  ├─ %d files scanned\n", result.FilesCount)
		fmt.Printf("  └─ Completed in %s\n", result.Duration.Round(1000000))
		fmt.Printf("\nStored in %s\n", dbPath)
	},
}

var ccDriftCmd = &cobra.Command{
	Use:   "drift [path]",
	Short: "Detect comment drift: broken refs, stale comments, expired TODOs",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		root := "."
		if len(args) > 0 {
			root = args[0]
		}
		root, _ = filepath.Abs(root)

		// Scan fresh.
		result, err := comment.ScanDir(root)
		if err != nil {
			FatalErrorRespectJSON("scanning comments: %v", err)
		}
		comment.ValidateReferences(root, result)

		// Detect drift.
		report := comment.DetectDrift(root, result)

		if jsonOutput {
			data, _ := json.MarshalIndent(report, "", "  ")
			fmt.Println(string(data))
			return
		}

		// Human-readable drift report.
		fmt.Println()
		fmt.Println("┌─ COMMENT DRIFT REPORT ─────────────────────────────────────┐")

		if len(report.BrokenRefs) > 0 {
			fmt.Println("│                                                             │")
			fmt.Printf("│ BROKEN REFERENCES (%d):                                     \n", len(report.BrokenRefs))
			for _, item := range report.BrokenRefs {
				truncReason := truncateCC(item.Reason, 55)
				fmt.Printf("│   🔴 %-55s│\n", fmt.Sprintf("%s:%d → %s", item.Node.File, item.Node.Line, truncReason))
			}
		}

		if len(report.StaleComments) > 0 {
			fmt.Println("│                                                             │")
			fmt.Printf("│ STALE COMMENTS (%d):                                        \n", len(report.StaleComments))
			for _, item := range report.StaleComments {
				content := truncateCC(item.Node.Content, 40)
				fmt.Printf("│   ⚠️  %-54s│\n", fmt.Sprintf("%s:%d → \"%s\"", item.Node.File, item.Node.Line, content))
				fmt.Printf("│      %-55s│\n", item.Reason)
			}
		}

		if len(report.ExpiredTodos) > 0 {
			fmt.Println("│                                                             │")
			fmt.Printf("│ EXPIRED TODOs (%d):                                         \n", len(report.ExpiredTodos))
			for _, item := range report.ExpiredTodos {
				content := truncateCC(item.Node.Content, 40)
				fmt.Printf("│   ⏰ %-55s│\n", fmt.Sprintf("%s:%d → \"%s\" (%s)", item.Node.File, item.Node.Line, content, item.Reason))
			}
		}

		if len(report.Inconsistent) > 0 {
			fmt.Println("│                                                             │")
			fmt.Printf("│ INCONSISTENT INVARIANTS (%d):                               \n", len(report.Inconsistent))
			for _, item := range report.Inconsistent {
				fmt.Printf("│   ❓ %-55s│\n", fmt.Sprintf("%s:%d → %s", item.Node.File, item.Node.Line, item.Details))
			}
		}

		total := len(report.BrokenRefs) + len(report.StaleComments) + len(report.ExpiredTodos) + len(report.Inconsistent)
		if total == 0 {
			fmt.Println("│                                                             │")
			fmt.Println("│   ✅ No comment drift detected                              │")
		}

		fmt.Println("│                                                             │")
		fmt.Println("└─────────────────────────────────────────────────────────────┘")
	},
}

var ccLinksCmd = &cobra.Command{
	Use:   "links [--file path] [--broken]",
	Short: "Show comment cross-reference graph",
	Run: func(cmd *cobra.Command, args []string) {
		root, _ := filepath.Abs(".")
		filterFile, _ := cmd.Flags().GetString("file")
		brokenOnly, _ := cmd.Flags().GetBool("broken")

		// Scan fresh.
		result, err := comment.ScanDir(root)
		if err != nil {
			FatalErrorRespectJSON("scanning comments: %v", err)
		}
		comment.ValidateReferences(root, result)

		if jsonOutput {
			// Filter nodes that have references.
			var withRefs []comment.Node
			for _, n := range result.Nodes {
				if len(n.References) == 0 {
					continue
				}
				if filterFile != "" && n.File != filterFile {
					continue
				}
				if brokenOnly {
					hasBroken := false
					for _, r := range n.References {
						if r.Status == comment.RefBroken {
							hasBroken = true
							break
						}
					}
					if !hasBroken {
						continue
					}
				}
				withRefs = append(withRefs, n)
			}
			data, _ := json.MarshalIndent(withRefs, "", "  ")
			fmt.Println(string(data))
			return
		}

		// Group by file.
		byFile := make(map[string][]comment.Node)
		for _, n := range result.Nodes {
			if len(n.References) == 0 {
				continue
			}
			if filterFile != "" && n.File != filterFile {
				continue
			}
			byFile[n.File] = append(byFile[n.File], n)
		}

		if len(byFile) == 0 {
			fmt.Println("No cross-references found.")
			return
		}

		for file, nodes := range byFile {
			fmt.Printf("\n%s:\n", file)
			for _, n := range nodes {
				for _, ref := range n.References {
					if brokenOnly && ref.Status != comment.RefBroken {
						continue
					}
					icon := statusIcon(ref.Status)
					fmt.Printf("  L%-4d \"%s\"  → %s %s\n",
						n.Line, truncateCC(ref.Target, 40), icon, ref.Status)
				}
			}
		}
	},
}

func init() {
	codecommentCmd.AddCommand(ccScanCmd)
	codecommentCmd.AddCommand(ccDriftCmd)
	codecommentCmd.AddCommand(ccLinksCmd)

	ccLinksCmd.Flags().String("file", "", "Filter by file path")
	ccLinksCmd.Flags().Bool("broken", false, "Show only broken references")

	rootCmd.AddCommand(codecommentCmd)
}

func statusIcon(s comment.RefStatus) string {
	switch s {
	case comment.RefValid:
		return "✅"
	case comment.RefBroken:
		return "🔴"
	case comment.RefStale:
		return "⚠️"
	default:
		return "❓"
	}
}

func truncateCC(s string, maxLen int) string {
	// Remove newlines for display.
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

