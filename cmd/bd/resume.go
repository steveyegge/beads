package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

var resumeEpicID string
var resumeSessionClosedCount int
var resumeFileRereadCount int
var resumeStateTransition bool
var resumeStateFrom string
var resumeStateTo string

var resumeCmd = &cobra.Command{
	Use:     "resume",
	GroupID: "issues",
	Short:   "Deterministic resume guard snapshot for an actor",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, resumeStateFrom, resumeStateTo) {
			return
		}
		ctx := rootCtx
		resumeActor := strings.TrimSpace(actor)
		if resumeActor == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "resume",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "actor is required (use --actor or set BD_ACTOR/BEADS_ACTOR)"},
				Events:  []string{"resume_failed"},
			}, 1)
			return
		}

		var resolvedEpic *string
		if strings.TrimSpace(resumeEpicID) != "" {
			epic, err := utils.ResolvePartialID(ctx, store, resumeEpicID)
			if err != nil {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "resume",
					Result:  "invalid_input",
					Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve epic %q: %v", resumeEpicID, err)},
					Events:  []string{"resume_failed"},
				}, 1)
				return
			}
			resolvedEpic = &epic
		}

		inProgress := types.StatusInProgress
		wipFilter := types.IssueFilter{
			Status:   &inProgress,
			Assignee: &resumeActor,
			ParentID: resolvedEpic,
			Limit:    20,
		}
		wipIssues, err := store.SearchIssues(ctx, "", wipFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "resume",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to query in_progress issues: %v", err)},
				Events:  []string{"resume_failed"},
			}, 1)
			return
		}

		actions := make([]map[string]interface{}, 0, len(wipIssues))
		wipCompact := make([]map[string]interface{}, 0, len(wipIssues))
		for _, issue := range wipIssues {
			wipCompact = append(wipCompact, compactIssue(issue))
			actions = append(actions, map[string]interface{}{
				"issue_id":       issue.ID,
				"title":          issue.Title,
				"action_classes": buildResumeGuardActions(issue.ID),
			})
		}

		pinnedStatus := types.StatusPinned
		anchorFilter := types.IssueFilter{
			Status:   &pinnedStatus,
			ParentID: resolvedEpic,
			Limit:    1,
		}
		anchorIssues, err := store.SearchIssues(ctx, "", anchorFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "resume",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to query pinned anchor: %v", err)},
				Events:  []string{"resume_failed"},
			}, 1)
			return
		}

		anchor := map[string]interface{}{}
		if len(anchorIssues) > 0 {
			anchorIssue := anchorIssues[0]
			digest := strings.TrimSpace(anchorIssue.Notes)
			if len(digest) > 400 {
				digest = digest[:400] + "..."
			}
			anchor = map[string]interface{}{
				"id":             anchorIssue.ID,
				"title":          anchorIssue.Title,
				"digest_excerpt": digest,
			}
		}

		result := "no_wip"
		if len(wipIssues) > 0 {
			result = "resume_required"
		}
		freshnessSignals := evaluateContextFreshnessSignals(
			resumeSessionClosedCount,
			resumeFileRereadCount,
			resumeStateTransition,
		)
		if len(freshnessSignals) > 0 {
			if result == "resume_required" {
				result = "resume_required_context_refresh"
			} else {
				result = "context_refresh_recommended"
			}
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "resume",
			Result:  result,
			Details: map[string]interface{}{
				"actor":                       resumeActor,
				"current_wip":                 wipCompact,
				"required_actions":            actions,
				"anchor_digest":               anchor,
				"wip_count":                   len(wipIssues),
				"epic_scope_resolved":         resolvedEpic,
				"context_freshness_signals":   freshnessSignals,
				"context_refresh_recommended": len(freshnessSignals) > 0,
			},
			Events: []string{"resume_snapshot"},
		}, 0)
	},
}

func buildResumeGuardActions(issueID string) []map[string]string {
	id := strings.TrimSpace(issueID)
	if id == "" {
		return []map[string]string{}
	}
	return []map[string]string{
		{
			"class":        "resume",
			"next_command": fmt.Sprintf("bd show %s --json", id),
		},
		{
			"class":        "close",
			"next_command": fmt.Sprintf("bd flow close-safe --issue %s --reason \"Updated: <summary>; verified with <command>\" --verified \"<command> -> <result>\"", id),
		},
		{
			"class":        "block",
			"next_command": fmt.Sprintf("bd flow block-with-context --issue %s --context-pack \"state=<state>; failing test/repro=<repro>; next=<command>; links/files=<paths>; blockers=<ids>\"", id),
		},
		{
			"class":        "relinquish",
			"next_command": fmt.Sprintf("bd update %s --assignee \"\" --status open --append-notes \"Context pack: state=<state>; next=<command>\"", id),
		},
	}
}

func evaluateContextFreshnessSignals(sessionClosedCount, fileRereadCount int, stateTransition bool) []string {
	signals := make([]string, 0, 3)
	if sessionClosedCount >= 3 {
		signals = append(signals, "session_close_threshold")
	}
	if fileRereadCount >= 5 {
		signals = append(signals, "file_reread_threshold")
	}
	if stateTransition {
		signals = append(signals, "state_transition")
	}
	return signals
}

func init() {
	resumeCmd.Flags().StringVar(&resumeStateFrom, "state-from", "", "Current session state for lifecycle transition validation")
	resumeCmd.Flags().StringVar(&resumeStateTo, "state-to", "", "Target session state for lifecycle transition validation")
	resumeCmd.Flags().StringVar(&resumeEpicID, "epic", "", "Optional epic scope for WIP and anchor checks")
	resumeCmd.Flags().IntVar(&resumeSessionClosedCount, "session-closed-count", 0, "Count of behavior-changing closes in current session")
	resumeCmd.Flags().IntVar(&resumeFileRereadCount, "file-reread-count", 0, "Count of repeated file reads in current session")
	resumeCmd.Flags().BoolVar(&resumeStateTransition, "state-transition", false, "Whether execution/recovery state transition occurred")
	resumeCmd.ValidArgsFunction = noCompletions
	rootCmd.AddCommand(resumeCmd)
}
