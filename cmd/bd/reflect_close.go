package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

func runIssueClose(since string) reflectIssueCloseSummary {
	issues, err := collectRecentOpenIssues(rootCtx, since)
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	summary := reflectIssueCloseSummary{Candidates: len(issues)}
	if len(issues) == 0 {
		fmt.Printf("%s No open beads modified in last %s\n", ui.RenderPassIcon(), since)
		return summary
	}

	fmt.Printf("\n%s Open beads (modified in last %s):\n\n", ui.RenderInfoIcon(), since)
	for i, issue := range issues {
		fmt.Printf("  %d. %s %s %s (%s)\n",
			i+1,
			ui.RenderPriority(issue.Priority),
			ui.RenderID(issue.ID),
			issue.Title,
			ui.RenderStatus(string(issue.Status)),
		)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nClose any? [1/2/.../all/none]: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" || input == "none" {
		return summary
	}

	indices := parseSelection(input, len(issues))
	if len(indices) == 0 {
		fmt.Printf("%s No valid selections\n", ui.RenderWarnIcon())
		return summary
	}

	for _, idx := range indices {
		issue := issues[idx]
		fmt.Printf("\nClosing %s...\n", issue.ID)
		fmt.Print("  Reason (optional): ")
		reason, _ := reader.ReadString('\n')
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "Closed"
		}

		if err := closeIssueWithRouting(issue.ID, reason); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing %s: %v\n", issue.ID, err)
			continue
		}
		summary.Closed = append(summary.Closed, issueToInfo(issue))
		fmt.Printf("%s Closed %s\n", ui.RenderPassIcon(), issue.ID)
	}

	return summary
}

func collectRecentOpenIssues(ctx context.Context, since string) ([]*types.Issue, error) {
	duration, err := parseDurationString(since)
	if err != nil {
		duration = 24 * time.Hour
	}
	cutoff := time.Now().Add(-duration)

	filter := types.IssueFilter{
		ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
		UpdatedAfter:  &cutoff,
	}
	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Priority != issues[j].Priority {
			return issues[i].Priority < issues[j].Priority
		}
		return issues[i].UpdatedAt.After(issues[j].UpdatedAt)
	})

	return issues, nil
}

func parseSelection(input string, max int) []int {
	if input == "all" || input == "both" {
		indices := make([]int, max)
		for i := range indices {
			indices[i] = i
		}
		return indices
	}

	var result []int
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err1 != nil || err2 != nil {
				continue
			}
			if start > end {
				start, end = end, start
			}
			for i := start; i <= end; i++ {
				if i >= 1 && i <= max {
					result = append(result, i-1)
				}
			}
			continue
		}
		num, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		if num >= 1 && num <= max {
			result = append(result, num-1)
		}
	}
	return result
}

func closeIssueWithRouting(id, reason string) error {
	actor := getActorWithGit()
	session := os.Getenv("CLAUDE_SESSION_ID")

	if daemonClient != nil && !needsRouting(id) {
		_, err := daemonClient.CloseIssue(&rpc.CloseArgs{
			ID:      id,
			Reason:  reason,
			Session: session,
		})
		return err
	}

	result, err := resolveAndGetIssueWithRouting(rootCtx, store, id)
	if err != nil {
		return err
	}
	if result == nil || result.Issue == nil {
		return fmt.Errorf("issue not found: %s", id)
	}
	defer result.Close()

	return result.Store.CloseIssue(rootCtx, result.ResolvedID, reason, actor, session)
}

func issueToInfo(issue *types.Issue) reflectIssueInfo {
	return reflectIssueInfo{
		ID:       issue.ID,
		Title:    issue.Title,
		Status:   string(issue.Status),
		Priority: issue.Priority,
	}
}
