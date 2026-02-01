package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
)

type reflectSummary struct {
	Since       string                   `json:"since"`
	Interactive bool                     `json:"interactive"`
	IssueClose  reflectIssueCloseSummary `json:"issue_close,omitempty"`
	Memory      reflectMemorySummary     `json:"memory,omitempty"`
	Debt        reflectDebtSummary       `json:"debt,omitempty"`
}

type reflectIssueCloseSummary struct {
	Candidates int                `json:"candidates"`
	Closed     []reflectIssueInfo `json:"closed,omitempty"`
}

type reflectIssueInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
}

type reflectMemorySummary struct {
	Saved bool   `json:"saved"`
	Path  string `json:"path,omitempty"`
}

type reflectDebtSummary struct {
	Logged   bool              `json:"logged"`
	Created  *reflectIssueInfo `json:"created,omitempty"`
	Path     string            `json:"path,omitempty"`
	Severity string            `json:"severity,omitempty"`
}

var reflectCmd = &cobra.Command{
	Use:   "reflect",
	Short: "Capture lessons and close issues at breakpoints",
	Long: `Reflect on recent work at natural breakpoints.

Actions:
  - Close issues completed this session
  - Capture lessons learned
  - Flag technical debt

Run without flags for full interactive flow.`,
	Run: runReflect,
}

func init() {
	reflectCmd.Flags().Bool("issue-close", false, "Only run issue closure")
	reflectCmd.Flags().Bool("memory", false, "Only run lesson capture")
	reflectCmd.Flags().Bool("debt", false, "Only run debt flagging")
	reflectCmd.Flags().Bool("non-interactive", false, "No prompts, summary only")
	reflectCmd.Flags().String("since", "24h", "Time window for 'recent' beads")
	rootCmd.AddCommand(reflectCmd)
}

func runReflect(cmd *cobra.Command, args []string) {
	issueClose, _ := cmd.Flags().GetBool("issue-close")
	memory, _ := cmd.Flags().GetBool("memory")
	debt, _ := cmd.Flags().GetBool("debt")
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
	since, _ := cmd.Flags().GetString("since")

	runAll := !issueClose && !memory && !debt

	if jsonOutput {
		nonInteractive = true
	}

	if nonInteractive {
		summary := runReflectSummary(since, runAll, issueClose, memory, debt)
		if jsonOutput {
			printJSON(summary)
		}
		return
	}

	// Interactive flow writes to disk or DB.
	if runAll || issueClose || memory || debt {
		CheckReadonly("reflect")
	}

	summary := reflectSummary{Since: since, Interactive: true}

	if runAll || issueClose {
		summary.IssueClose = runIssueClose(since)
	}
	if runAll || memory {
		summary.Memory = runMemoryCapture()
	}
	if runAll || debt {
		summary.Debt = runDebtCapture()
	}

	if jsonOutput {
		printJSON(summary)
	}
}

func runReflectSummary(since string, runAll, issueClose, memory, debt bool) reflectSummary {
	summary := reflectSummary{Since: since, Interactive: false}

	if runAll || issueClose {
		candidates, err := collectRecentOpenIssues(rootCtx, since)
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		summary.IssueClose = reflectIssueCloseSummary{Candidates: len(candidates)}
		if !jsonOutput {
			if len(candidates) == 0 {
				fmt.Printf("%s No open beads modified in last %s\n", ui.RenderPassIcon(), since)
			} else {
				fmt.Printf("%s Open beads modified in last %s: %d\n", ui.RenderInfoIcon(), since, len(candidates))
				fmt.Printf("%s Run 'bd reflect' interactively to close\n", ui.RenderInfoIcon())
			}
		}
	}

	if runAll || memory {
		summary.Memory = reflectMemorySummary{Saved: false}
		if !jsonOutput {
			fmt.Printf("%s Run 'bd reflect --memory' to capture lessons\n", ui.RenderInfoIcon())
		}
	}

	if runAll || debt {
		summary.Debt = reflectDebtSummary{Logged: false}
		if !jsonOutput {
			fmt.Printf("%s Run 'bd reflect --debt' to flag technical debt\n", ui.RenderInfoIcon())
		}
	}

	return summary
}

func printJSON(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}
	fmt.Println(string(data))
}
