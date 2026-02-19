package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

type landStep struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

var (
	landEpicID    string
	landCheckOnly bool
	landRunSync   bool
	landRunPush   bool
)

var landCmd = &cobra.Command{
	Use:     "land",
	GroupID: "sync",
	Short:   "Run deterministic landing gates for an epic/session",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		landingActor := strings.TrimSpace(actor)
		if landingActor == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "actor is required (use --actor or set BD_ACTOR/BEADS_ACTOR)"},
				Events:  []string{"land_failed"},
			}, 1)
			return
		}
		if strings.TrimSpace(landEpicID) == "" {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": "--epic is required"},
				Events:  []string{"land_failed"},
			}, 1)
			return
		}

		resolvedEpicID, err := utils.ResolvePartialID(ctx, store, landEpicID)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "invalid_input",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to resolve epic %q: %v", landEpicID, err)},
				Events:  []string{"land_failed"},
			}, 1)
			return
		}

		steps := make([]landStep, 0)
		gateFailed := false

		statusInProgress := types.StatusInProgress
		wipFilter := types.IssueFilter{
			Status:   &statusInProgress,
			Assignee: &landingActor,
			Limit:    50,
		}
		wipIssues, err := store.SearchIssues(ctx, "", wipFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to query in_progress issues: %v", err)},
				Events:  []string{"land_failed"},
			}, 1)
			return
		}
		if len(wipIssues) == 0 {
			steps = append(steps, landStep{Name: "gate1_wip", Status: "pass", Message: "no in_progress issues for actor"})
		} else {
			gateFailed = true
			ids := make([]string, 0, len(wipIssues))
			for _, issue := range wipIssues {
				ids = append(ids, issue.ID)
			}
			steps = append(steps, landStep{Name: "gate1_wip", Status: "fail", Message: "in_progress issues remain: " + strings.Join(ids, ",")})
		}

		statusHooked := types.StatusHooked
		hookedFilter := types.IssueFilter{
			Status:   &statusHooked,
			Assignee: &landingActor,
			Limit:    50,
		}
		hookedIssues, err := store.SearchIssues(ctx, "", hookedFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to query hooked issues: %v", err)},
				Events:  []string{"land_failed"},
			}, 1)
			return
		}
		if len(hookedIssues) == 0 {
			steps = append(steps, landStep{Name: "gate1_hooked", Status: "pass", Message: "no hooked issues for actor"})
		} else {
			gateFailed = true
			ids := make([]string, 0, len(hookedIssues))
			for _, issue := range hookedIssues {
				ids = append(ids, issue.ID)
			}
			steps = append(steps, landStep{Name: "gate1_hooked", Status: "fail", Message: "hooked issues remain: " + strings.Join(ids, ",")})
		}

		statusOpen := types.StatusOpen
		openFilter := types.IssueFilter{
			Status:   &statusOpen,
			ParentID: &resolvedEpicID,
			Limit:    200,
		}
		openIssues, err := store.SearchIssues(ctx, "", openFilter)
		if err != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to query open children: %v", err)},
				Events:  []string{"land_failed"},
			}, 1)
			return
		}
		if len(openIssues) == 0 {
			steps = append(steps, landStep{Name: "gate1_open_under_epic", Status: "pass", Message: "no open issues under epic"})
		} else {
			gateFailed = true
			ids := make([]string, 0, len(openIssues))
			for _, issue := range openIssues {
				ids = append(ids, issue.ID)
			}
			steps = append(steps, landStep{Name: "gate1_open_under_epic", Status: "fail", Message: "open issues remain under epic: " + strings.Join(ids, ",")})
		}

		gitDirty, gitErr := gitStatusDirty()
		if gitErr != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to check git status: %v", gitErr)},
				Events:  []string{"land_failed"},
			}, 1)
			return
		}
		if !gitDirty {
			steps = append(steps, landStep{Name: "gate3_git_clean", Status: "pass", Message: "git working tree is clean"})
		} else {
			gateFailed = true
			steps = append(steps, landStep{Name: "gate3_git_clean", Status: "fail", Message: "git working tree is dirty"})
		}

		if gateFailed {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "gate_failed",
				IssueID: resolvedEpicID,
				Details: map[string]interface{}{
					"actor":   landingActor,
					"steps":   steps,
					"message": "landing gates failed",
				},
				Events: []string{"land_gates_failed"},
			}, exitCodePolicyViolation)
			return
		}

		if landCheckOnly {
			steps = append(steps, landStep{Name: "actions", Status: "skipped", Message: "check-only mode enabled"})
			finishEnvelope(commandEnvelope{
				OK:      true,
				Command: "land",
				Result:  "check_passed",
				IssueID: resolvedEpicID,
				Details: map[string]interface{}{
					"actor": landingActor,
					"steps": steps,
				},
				Events: []string{"land_check_passed"},
			}, 0)
			return
		}

		// Release DB handle before spawning nested bd command.
		if store != nil {
			_ = store.Close()
			store = nil
		}

		if landRunSync {
			if _, err := runSubprocess("bd", "sync"); err != nil {
				steps = append(steps, landStep{Name: "gate3_sync", Status: "fail", Message: err.Error()})
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "land",
					Result:  "operation_failed",
					IssueID: resolvedEpicID,
					Details: map[string]interface{}{
						"actor": landingActor,
						"steps": steps,
					},
					Events: []string{"land_failed_sync"},
				}, 1)
				return
			}
			steps = append(steps, landStep{Name: "gate3_sync", Status: "pass", Message: "bd sync completed"})
		} else {
			steps = append(steps, landStep{Name: "gate3_sync", Status: "skipped", Message: "--sync not requested"})
		}

		if landRunPush {
			if _, err := runSubprocess("git", "push"); err != nil {
				steps = append(steps, landStep{Name: "gate3_push", Status: "fail", Message: err.Error()})
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "land",
					Result:  "operation_failed",
					IssueID: resolvedEpicID,
					Details: map[string]interface{}{
						"actor": landingActor,
						"steps": steps,
					},
					Events: []string{"land_failed_push"},
				}, 1)
				return
			}
			steps = append(steps, landStep{Name: "gate3_push", Status: "pass", Message: "git push completed"})
		} else {
			steps = append(steps, landStep{Name: "gate3_push", Status: "skipped", Message: "--push not requested"})
		}

		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "land",
			Result:  "landed",
			IssueID: resolvedEpicID,
			Details: map[string]interface{}{
				"actor": landingActor,
				"steps": steps,
			},
			Events: []string{"land_completed"},
		}, 0)
	},
}

func gitStatusDirty() (bool, error) {
	out, err := runSubprocess("git", "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func runSubprocess(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %v | %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func init() {
	landCmd.Flags().StringVar(&landEpicID, "epic", "", "Epic ID for landing gate scope")
	landCmd.Flags().BoolVar(&landCheckOnly, "check-only", false, "Run gates without sync/push operations")
	landCmd.Flags().BoolVar(&landRunSync, "sync", false, "Run bd sync after gates pass")
	landCmd.Flags().BoolVar(&landRunPush, "push", false, "Run git push after gates pass")
	landCmd.ValidArgsFunction = noCompletions

	rootCmd.AddCommand(landCmd)
}
