package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
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
	landStateFrom      string
	landStateTo        string
	landEpicID         string
	landCheckOnly      bool
	landRunPullRebase  bool
	landRunSync        bool
	landRunPush        bool
	landRunSyncMerge   bool
	landRequireQuality bool
	landQualitySummary string
	landRequireHandoff bool
	landNextPrompt     string
	landStash          string
)

var landCmd = &cobra.Command{
	Use:     "land",
	GroupID: "sync",
	Short:   "Run deterministic landing gates for an epic/session",
	Run: func(cmd *cobra.Command, args []string) {
		if !enforceLifecycleStateTransitionGuard(cmd, landStateFrom, landStateTo) {
			return
		}
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
		criticalWarnings, warnErr := landCriticalDoctorWarnings()
		if warnErr != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to evaluate critical doctor warnings: %v", warnErr)},
				Events:  []string{"land_failed"},
			}, 1)
			return
		}
		if len(criticalWarnings) > 0 {
			gateFailed = true
			steps = append(steps, landStep{
				Name:    "gate3_critical_warnings",
				Status:  "fail",
				Message: "critical doctor warnings promoted to blockers: " + strings.Join(criticalWarnings, ","),
			})
		} else {
			steps = append(steps, landStep{Name: "gate3_critical_warnings", Status: "pass", Message: "no critical doctor warnings"})
		}

		qualityStep := evaluateLandQualityGate(landRequireQuality, landQualitySummary)
		steps = append(steps, qualityStep)
		if qualityStep.Status == "fail" {
			gateFailed = true
		}

		readyIssues, readyErr := store.GetReadyWork(ctx, types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  5,
		})
		if readyErr != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "system_error",
				Details: map[string]interface{}{"message": fmt.Sprintf("failed to query handoff ready snapshot: %v", readyErr)},
				Events:  []string{"land_failed"},
			}, 1)
			return
		}
		readyIDs := make([]string, 0, len(readyIssues))
		for _, issue := range readyIssues {
			readyIDs = append(readyIDs, issue.ID)
		}
		if len(readyIDs) == 0 {
			steps = append(steps, landStep{Name: "gate4_ready_snapshot", Status: "pass", Message: "ready snapshot: none"})
		} else {
			steps = append(steps, landStep{Name: "gate4_ready_snapshot", Status: "pass", Message: "ready snapshot: " + strings.Join(readyIDs, ",")})
		}

		handoffStep := evaluateLandHandoffGate(landRequireHandoff, landNextPrompt, landStash)
		steps = append(steps, handoffStep)
		if handoffStep.Status == "fail" {
			gateFailed = true
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
			choreographySteps, _ := runLandGate3Choreography(true, landRunPullRebase, landRunSync, landRunPush, landRunSyncMerge, runSubprocess)
			steps = append(steps, choreographySteps...)
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

		choreographySteps, runErr := runLandGate3Choreography(false, landRunPullRebase, landRunSync, landRunPush, landRunSyncMerge, runSubprocess)
		steps = append(steps, choreographySteps...)
		if runErr != nil {
			finishEnvelope(commandEnvelope{
				OK:      false,
				Command: "land",
				Result:  "operation_failed",
				IssueID: resolvedEpicID,
				Details: map[string]interface{}{
					"actor": landingActor,
					"steps": steps,
				},
				Events: []string{"land_gate3_failed"},
			}, 1)
			return
		}

		skippedGate3Ops := skippedGate3Operations(choreographySteps)
		resultName := "landed"
		events := []string{"land_completed"}
		if len(skippedGate3Ops) > 0 {
			resultName = "landed_with_skipped_gate3"
			events = append(events, "land_completed_partial")
		}
		finishEnvelope(commandEnvelope{
			OK:      true,
			Command: "land",
			Result:  resultName,
			IssueID: resolvedEpicID,
			Details: map[string]interface{}{
				"actor":             landingActor,
				"steps":             steps,
				"skipped_gate3_ops": skippedGate3Ops,
			},
			Events: events,
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

func evaluateLandQualityGate(requireQuality bool, summary string) landStep {
	if !requireQuality {
		return landStep{Name: "gate2_quality", Status: "skipped", Message: "quality evidence not required"}
	}
	if strings.TrimSpace(summary) == "" {
		return landStep{Name: "gate2_quality", Status: "fail", Message: "quality evidence missing (provide --quality-summary)"}
	}
	return landStep{Name: "gate2_quality", Status: "pass", Message: strings.TrimSpace(summary)}
}

func evaluateLandHandoffGate(requireHandoff bool, nextPrompt, stash string) landStep {
	if !requireHandoff {
		return landStep{Name: "gate4_handoff", Status: "skipped", Message: "handoff fields not required"}
	}
	missing := make([]string, 0, 2)
	if strings.TrimSpace(nextPrompt) == "" {
		missing = append(missing, "next-prompt")
	}
	if strings.TrimSpace(stash) == "" {
		missing = append(missing, "stash")
	}
	if len(missing) > 0 {
		return landStep{Name: "gate4_handoff", Status: "fail", Message: "missing handoff fields: " + strings.Join(missing, ",")}
	}
	return landStep{Name: "gate4_handoff", Status: "pass", Message: "next prompt and stash fields recorded"}
}

func landCriticalDoctorWarnings() ([]string, error) {
	workingPath, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	ctx := rootCtx
	if ctx == nil {
		ctx = context.Background()
	}
	checks := runPreflightGateChecks(ctx, workingPath)
	return criticalWarningNamesFromChecks(checks), nil
}

func criticalWarningNamesFromChecks(checks []doctorCheck) []string {
	critical := make([]string, 0)
	for _, check := range checks {
		if strings.ToLower(strings.TrimSpace(check.Status)) != statusWarning {
			continue
		}
		if _, ok := preflightCriticalDoctorWarnings[check.Name]; ok {
			critical = append(critical, check.Name)
		}
	}
	sort.Strings(critical)
	return uniqueSortedStrings(critical)
}

func runLandGate3Choreography(checkOnly, runPullRebase, runSync, runPush, runMerge bool, runner func(string, ...string) (string, error)) ([]landStep, error) {
	steps := make([]landStep, 0, 5)
	if checkOnly {
		if runPullRebase {
			steps = append(steps, landStep{Name: "gate3_pull_rebase", Status: "skipped", Message: "check-only mode"})
		} else {
			steps = append(steps, landStep{Name: "gate3_pull_rebase", Status: "skipped", Message: "--pull-rebase not requested"})
		}
		steps = append(steps, landStep{Name: "gate3_sync_status", Status: "skipped", Message: "check-only mode"})
		if runMerge {
			steps = append(steps, landStep{Name: "gate3_sync_merge", Status: "skipped", Message: "check-only mode"})
		}
		if runSync {
			steps = append(steps, landStep{Name: "gate3_sync", Status: "skipped", Message: "check-only mode"})
		} else {
			steps = append(steps, landStep{Name: "gate3_sync", Status: "skipped", Message: "--sync not requested"})
		}
		if runPush {
			steps = append(steps, landStep{Name: "gate3_push", Status: "skipped", Message: "check-only mode"})
		} else {
			steps = append(steps, landStep{Name: "gate3_push", Status: "skipped", Message: "--push not requested"})
		}
		return steps, nil
	}

	if runPullRebase {
		if _, err := runner("git", "pull", "--rebase"); err != nil {
			steps = append(steps, landStep{Name: "gate3_pull_rebase", Status: "fail", Message: err.Error()})
			return steps, err
		}
		steps = append(steps, landStep{Name: "gate3_pull_rebase", Status: "pass", Message: "git pull --rebase completed"})
	} else {
		steps = append(steps, landStep{Name: "gate3_pull_rebase", Status: "skipped", Message: "--pull-rebase not requested"})
	}

	if _, err := runner("bd", "sync", "--status"); err != nil {
		steps = append(steps, landStep{Name: "gate3_sync_status", Status: "fail", Message: err.Error()})
		return steps, err
	}
	steps = append(steps, landStep{Name: "gate3_sync_status", Status: "pass", Message: "bd sync --status completed"})

	if runMerge {
		if _, err := runner("bd", "sync", "--merge"); err != nil {
			steps = append(steps, landStep{Name: "gate3_sync_merge", Status: "fail", Message: err.Error()})
			return steps, err
		}
		steps = append(steps, landStep{Name: "gate3_sync_merge", Status: "pass", Message: "bd sync --merge completed"})
	}

	if runSync {
		if _, err := runner("bd", "sync"); err != nil {
			steps = append(steps, landStep{Name: "gate3_sync", Status: "fail", Message: err.Error()})
			return steps, err
		}
		steps = append(steps, landStep{Name: "gate3_sync", Status: "pass", Message: "bd sync completed"})
	} else {
		steps = append(steps, landStep{Name: "gate3_sync", Status: "skipped", Message: "--sync not requested"})
	}

	if runPush {
		if _, err := runner("git", "push"); err != nil {
			steps = append(steps, landStep{Name: "gate3_push", Status: "fail", Message: err.Error()})
			return steps, err
		}
		steps = append(steps, landStep{Name: "gate3_push", Status: "pass", Message: "git push completed"})
	} else {
		steps = append(steps, landStep{Name: "gate3_push", Status: "skipped", Message: "--push not requested"})
	}

	return steps, nil
}

func skippedGate3Operations(steps []landStep) []string {
	skipped := make([]string, 0)
	for _, step := range steps {
		if step.Status != "skipped" {
			continue
		}
		switch step.Name {
		case "gate3_pull_rebase", "gate3_sync", "gate3_push":
			skipped = append(skipped, step.Name)
		}
	}
	sort.Strings(skipped)
	return skipped
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
	landCmd.Flags().StringVar(&landStateFrom, "state-from", "", "Current session state for lifecycle transition validation")
	landCmd.Flags().StringVar(&landStateTo, "state-to", "", "Target session state for lifecycle transition validation")
	landCmd.Flags().StringVar(&landEpicID, "epic", "", "Epic ID for landing gate scope")
	landCmd.Flags().BoolVar(&landCheckOnly, "check-only", false, "Run gates without sync/push operations")
	landCmd.Flags().BoolVar(&landRunPullRebase, "pull-rebase", false, "Run git pull --rebase during Gate 3 choreography")
	landCmd.Flags().BoolVar(&landRunSync, "sync", false, "Run bd sync after gates pass")
	landCmd.Flags().BoolVar(&landRunPush, "push", false, "Run git push after gates pass")
	landCmd.Flags().BoolVar(&landRunSyncMerge, "sync-merge", false, "Run bd sync --merge during Gate 3 choreography")
	landCmd.Flags().BoolVar(&landRequireQuality, "require-quality", false, "Require Gate 2 quality evidence summary")
	landCmd.Flags().StringVar(&landQualitySummary, "quality-summary", "", "Gate 2 quality evidence summary (tests/lint/build results)")
	landCmd.Flags().BoolVar(&landRequireHandoff, "require-handoff", false, "Require Gate 4 next prompt + stash fields")
	landCmd.Flags().StringVar(&landNextPrompt, "next-prompt", "", "Gate 4 next-session prompt text")
	landCmd.Flags().StringVar(&landStash, "stash", "", "Gate 4 stash field value (for example: none)")
	landCmd.ValidArgsFunction = noCompletions

	rootCmd.AddCommand(landCmd)
}
