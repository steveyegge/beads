package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

var resumeEpicID string

var resumeCmd = &cobra.Command{
	Use:     "resume",
	GroupID: "issues",
	Short:   "Deterministic resume guard snapshot for an actor",
	Run: func(cmd *cobra.Command, args []string) {
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
				"issue_id": issue.ID,
				"title":    issue.Title,
				"actions":  []string{"resume", "close", "block", "relinquish"},
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

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "resume",
			Result:  result,
			Details: map[string]interface{}{
				"actor":               resumeActor,
				"current_wip":         wipCompact,
				"required_actions":    actions,
				"anchor_digest":       anchor,
				"wip_count":           len(wipIssues),
				"epic_scope_resolved": resolvedEpic,
			},
			Events: []string{"resume_snapshot"},
		}, 0)
	},
}

func init() {
	resumeCmd.Flags().StringVar(&resumeEpicID, "epic", "", "Optional epic scope for WIP and anchor checks")
	resumeCmd.ValidArgsFunction = noCompletions
	rootCmd.AddCommand(resumeCmd)
}
