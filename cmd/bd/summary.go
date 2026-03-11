package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

// SummaryChild represents a child issue in summary output.
type SummaryChild struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Status      string     `json:"status"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
	CloseReason string     `json:"close_reason,omitempty"`
}

// EpicSummaryResult holds the result of an epic summary.
type EpicSummaryResult struct {
	EpicID      string         `json:"epic_id"`
	EpicTitle   string         `json:"epic_title"`
	Status      string         `json:"status"`
	Children    []SummaryChild `json:"children"`
	TotalCount  int            `json:"total_count"`
	ClosedCount int            `json:"closed_count"`
	Decisions   []string       `json:"decisions,omitempty"`
}

// SinceSummaryResult holds the result of a date-range summary.
type SinceSummaryResult struct {
	Since       time.Time      `json:"since"`
	Closed      []SummaryChild `json:"closed"`
	TotalClosed int            `json:"total_closed"`
}

// SessionSummaryResult holds the result of a session summary.
type SessionSummaryResult struct {
	SessionID string         `json:"session_id"`
	Closed    []SummaryChild `json:"closed"`
}

func buildEpicSummary(ctx context.Context, s *dolt.DoltStore, epicID string) (*EpicSummaryResult, error) {
	epic, err := s.GetIssue(ctx, epicID)
	if err != nil {
		return nil, fmt.Errorf("issue %s not found: %w", epicID, err)
	}
	filter := types.IssueFilter{ParentID: &epicID}
	children, err := s.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, fmt.Errorf("searching children: %w", err)
	}
	result := &EpicSummaryResult{
		EpicID:    epic.ID,
		EpicTitle: epic.Title,
		Status:    string(epic.Status),
	}
	for _, child := range children {
		sc := SummaryChild{
			ID: child.ID, Title: child.Title, Status: string(child.Status),
			ClosedAt: child.ClosedAt, CloseReason: child.CloseReason,
		}
		result.Children = append(result.Children, sc)
		if child.Status == types.StatusClosed {
			result.ClosedCount++
		}
	}
	result.TotalCount = len(children)
	// Find decision comments by checking DECISION: text prefix (legacy format).
	comments, err := s.GetIssueComments(ctx, epicID)
	if err == nil {
		for _, c := range comments {
			if len(c.Text) > 9 && c.Text[:9] == "DECISION:" {
				result.Decisions = append(result.Decisions, c.Text)
			}
		}
	}
	return result, nil
}

func buildSinceSummary(ctx context.Context, s *dolt.DoltStore, since time.Time) (*SinceSummaryResult, error) {
	closedStatus := types.StatusClosed
	filter := types.IssueFilter{Status: &closedStatus, ClosedAfter: &since}
	issues, err := s.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, fmt.Errorf("searching issues: %w", err)
	}
	result := &SinceSummaryResult{Since: since}
	for _, issue := range issues {
		result.Closed = append(result.Closed, SummaryChild{
			ID: issue.ID, Title: issue.Title, Status: string(issue.Status),
			ClosedAt: issue.ClosedAt, CloseReason: issue.CloseReason,
		})
	}
	result.TotalClosed = len(result.Closed)
	return result, nil
}

func buildSessionSummary(ctx context.Context, s *dolt.DoltStore, sessionID string) (*SessionSummaryResult, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("no active session. Set CLAUDE_SESSION_ID or use --since=DATE instead")
	}
	closedStatus := types.StatusClosed
	filter := types.IssueFilter{Status: &closedStatus}
	issues, err := s.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, fmt.Errorf("searching issues: %w", err)
	}
	result := &SessionSummaryResult{SessionID: sessionID}
	for _, issue := range issues {
		if issue.ClosedBySession == sessionID {
			result.Closed = append(result.Closed, SummaryChild{
				ID: issue.ID, Title: issue.Title, Status: string(issue.Status),
				ClosedAt: issue.ClosedAt, CloseReason: issue.CloseReason,
			})
		}
	}
	return result, nil
}

var summaryCmd = &cobra.Command{
	Use:     "summary [epic-id]",
	GroupID: "views",
	Short:   "Summarize completed work",
	Long: `Show a summary of completed work.

Modes:
  bd summary <epic-id>           Timeline of epic's children
  bd summary --since=2026-03-01  All work closed since date
  bd summary --session           Current session's closed work`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		sinceStr, _ := cmd.Flags().GetString("since")
		sessionFlag, _ := cmd.Flags().GetBool("session")
		if err := withStorage(rootCtx, store, dbPath, func(s *dolt.DoltStore) error {
			ctx := rootCtx
			if len(args) == 1 {
				result, err := buildEpicSummary(ctx, s, args[0])
				if err != nil {
					FatalErrorRespectJSON("%v", err)
				}
				if jsonOutput {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(result)
				}
				printEpicSummary(result)
				return nil
			}
			if sessionFlag {
				sessionID := os.Getenv("CLAUDE_SESSION_ID")
				result, err := buildSessionSummary(ctx, s, sessionID)
				if err != nil {
					FatalErrorRespectJSON("%v", err)
				}
				if jsonOutput {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(result)
				}
				printSessionSummary(result)
				return nil
			}
			if sinceStr != "" {
				since, err := time.Parse("2006-01-02", sinceStr)
				if err != nil {
					FatalErrorRespectJSON("invalid date format: %v (use YYYY-MM-DD)", err)
				}
				result, err := buildSinceSummary(ctx, s, since)
				if err != nil {
					FatalErrorRespectJSON("%v", err)
				}
				if jsonOutput {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(result)
				}
				printSinceSummary(result)
				return nil
			}
			FatalErrorRespectJSON("specify an epic ID, --since=DATE, or --session")
			return nil
		}); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
	},
}

func printEpicSummary(r *EpicSummaryResult) {
	fmt.Printf("Epic: %s %q\n", r.EpicID, r.EpicTitle)
	fmt.Printf("Status: %s\n\n", r.Status)
	fmt.Printf("Tasks (%d/%d complete):\n", r.ClosedCount, r.TotalCount)
	for _, c := range r.Children {
		check := " "
		if c.Status == string(types.StatusClosed) {
			check = "x"
		}
		line := fmt.Sprintf("  [%s] %s  %q", check, c.ID, c.Title)
		if c.ClosedAt != nil {
			line += fmt.Sprintf("  closed %s", c.ClosedAt.Format("2006-01-02"))
		}
		if c.CloseReason != "" {
			line += fmt.Sprintf("  %q", c.CloseReason)
		}
		fmt.Println(line)
	}
	if len(r.Decisions) > 0 {
		fmt.Printf("\nDecisions:\n")
		for _, d := range r.Decisions {
			fmt.Printf("  %s\n", d)
		}
	}
}

func printSinceSummary(r *SinceSummaryResult) {
	fmt.Printf("Work completed since %s:\n\n", r.Since.Format("2006-01-02"))
	for _, c := range r.Closed {
		line := fmt.Sprintf("  [x] %s  %q", c.ID, c.Title)
		if c.ClosedAt != nil {
			line += fmt.Sprintf("  closed %s", c.ClosedAt.Format("2006-01-02"))
		}
		fmt.Println(line)
	}
	fmt.Printf("\nTotal: %d tasks closed\n", r.TotalClosed)
}

func printSessionSummary(r *SessionSummaryResult) {
	fmt.Printf("Session: %s\n\n", r.SessionID)
	fmt.Printf("Closed (%d):\n", len(r.Closed))
	for _, c := range r.Closed {
		line := fmt.Sprintf("  [x] %s  %q", c.ID, c.Title)
		if c.CloseReason != "" {
			line += fmt.Sprintf("  closed %q", c.CloseReason)
		}
		fmt.Println(line)
	}
}

func init() {
	rootCmd.AddCommand(summaryCmd)
	summaryCmd.Flags().String("since", "", "Show work closed since date (YYYY-MM-DD)")
	summaryCmd.Flags().Bool("session", false, "Show current session's closed work")
}
