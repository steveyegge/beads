package scripts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// worker-env-sync-automerge.yml lands worker-env-autosync.yml's sync PR
// (bot/worker-env-sync) with no review: tools/rbe/worker-env-sync-verify
// checks the PR from the API, as data, and the act job arms auto-merge only
// on its "arm" verdict. Every property pinned here is part of that trust
// boundary; relaxing one is a security decision, not a refactor.
const (
	workerEnvAutomergeWorkflow = "worker-env-sync-automerge.yml"
	workerEnvSyncVerify        = "tools/rbe/worker-env-sync-verify"
	workerEnvSyncPRBot         = "gascity-autofix[bot]"
	workerEnvSyncPushBot       = "gastownhall-autofix[bot]"
	workerEnvSyncReviewLabel   = "status/needs-review-auto"

	// verify's if: the head branch and the same-repository filters first,
	// so no other PR's pull_request_target run gets past it, then the fork
	// guard and the variable gate.
	workerEnvAutomergeVerifyIf = "(github.event_name != 'pull_request_target' || " +
		"(github.event.pull_request.head.ref == 'bot/worker-env-sync' && " +
		"github.event.pull_request.head.repo.full_name == github.repository)) && " +
		"github.repository == 'gastownhall/beads' && vars.WORKER_ENV_SYNC_AUTOMERGE == 'true'"
	workerEnvAutomergeGroupPrefix = "worker-env-sync-automerge-"
	workerEnvAutomergeGroup       = workerEnvAutomergeGroupPrefix + "${{ github.event_name == 'pull_request_target' && " +
		"(github.event.pull_request.head.ref != 'bot/worker-env-sync' || " +
		"github.event.pull_request.head.repo.full_name != github.repository) && " +
		"github.event.pull_request.number || 'sync-pr' }}"

	workerEnvActIf   = "needs.verify.outputs.arm == 'true' || needs.verify.outputs.disarm == 'true' || needs.verify.outputs.comment == 'true'"
	workerEnvRerunIf = "needs.verify.outputs.rerun != ''"

	workerEnvMergeTokenStep = "Mint merge token (gascity-autofix)"
	workerEnvDisarmStep     = "Take the sync PR out of auto-merge and the merge queue"
	workerEnvArmStep        = "Arm auto-merge"
	workerEnvPRTokenStep    = "Mint PR token (gascity-autofix)"
	workerEnvCommentStep    = "Comment the refusal on the sync PR"
	workerEnvLabelStep      = "Label the sync PR for review instead"

	workerEnvMergeToken = "${{ steps.merge-token.outputs.token }}"
	workerEnvPRToken    = "${{ steps.pr-token.outputs.token }}"
	workerEnvFallbackIf = "needs.verify.outputs.arm == 'true' && needs.verify.outputs.fallback-label == 'true' && (steps.merge-token.outcome == 'failure' || steps.arm.outputs.denied == '1')"
)

// The act job's steps, their if:, and the writes each may make.
var workerEnvActSteps = []struct{ name, id, ifExpr string }{
	{workerEnvMergeTokenStep, "merge-token", "needs.verify.outputs.arm == 'true' || needs.verify.outputs.disarm == 'true'"},
	{workerEnvDisarmStep, "", "needs.verify.outputs.disarm == 'true'"},
	{workerEnvArmStep, "arm", "needs.verify.outputs.arm == 'true' && steps.merge-token.outcome == 'success'"},
	{workerEnvPRTokenStep, "pr-token", "needs.verify.outputs.comment == 'true' || (" + workerEnvFallbackIf + ")"},
	{workerEnvCommentStep, "", "needs.verify.outputs.comment == 'true'"},
	{workerEnvLabelStep, "", workerEnvFallbackIf},
}

// TestWorkerEnvSyncAutomergeWorkflowTriggers: pull_request_target for the
// fast path (opened, reopened, synchronize, ready_for_review into main),
// and a 15-minute schedule plus workflow_dispatch to re-check an open sync
// PR after a missed event or a merge-queue ejection. Nothing else: no
// pull_request (a fork's own copy of this file would run), no
// workflow_run or comment trigger.
func TestWorkerEnvSyncAutomergeWorkflowTriggers(t *testing.T) {
	root := readYAMLNode(t, filepath.Join(".github", "workflows", workerEnvAutomergeWorkflow))
	if got := yamlScalar(root, "name"); got != "rbe worker-env sync automerge" {
		t.Errorf("name = %q, want rbe worker-env sync automerge", got)
	}
	triggers := yamlMapKeys(root, "on")
	sort.Strings(triggers)
	if want := []string{"pull_request_target", "schedule", "workflow_dispatch"}; !slices.Equal(triggers, want) {
		t.Fatalf("triggers = %v, want exactly %v", triggers, want)
	}
	var doc struct {
		On struct {
			PullRequestTarget struct {
				Types    []string `yaml:"types"`
				Branches []string `yaml:"branches"`
			} `yaml:"pull_request_target"`
			Schedule []map[string]string `yaml:"schedule"`
		} `yaml:"on"`
	}
	if err := root.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if want := []string{"opened", "reopened", "synchronize", "ready_for_review"}; !slices.Equal(doc.On.PullRequestTarget.Types, want) {
		t.Errorf("pull_request_target types = %v, want %v", doc.On.PullRequestTarget.Types, want)
	}
	if !slices.Equal(doc.On.PullRequestTarget.Branches, []string{"main"}) {
		t.Errorf("pull_request_target branches = %v, want [main]", doc.On.PullRequestTarget.Branches)
	}
	if len(doc.On.Schedule) != 1 || doc.On.Schedule[0]["cron"] != "*/15 * * * *" {
		t.Errorf("schedule = %v, want one */15 * * * * cron", doc.On.Schedule)
	}
}

// TestWorkerEnvSyncAutomergeConcurrency: every run about the sync PR (its
// pull_request_target events, the schedule, a dispatch) waits in one
// group and none is cancelled, so a verdict is acted on before the next
// verification starts and a disarm can never land between another run's
// verify and arm. Any other PR's run (it skips at verify's if:) gets a
// group of its own, so it can never displace a waiting sync-PR run; a fork
// PR from a branch named bot/worker-env-sync included.
func TestWorkerEnvSyncAutomergeConcurrency(t *testing.T) {
	root := readYAMLNode(t, filepath.Join(".github", "workflows", workerEnvAutomergeWorkflow))
	var doc struct {
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress *bool  `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
	}
	if err := root.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.Concurrency.Group != workerEnvAutomergeGroup {
		t.Fatalf("concurrency group = %q, want %q", doc.Concurrency.Group, workerEnvAutomergeGroup)
	}
	if doc.Concurrency.CancelInProgress == nil || *doc.Concurrency.CancelInProgress {
		t.Errorf("concurrency must set cancel-in-progress: false")
	}
	expr := strings.TrimPrefix(doc.Concurrency.Group, workerEnvAutomergeGroupPrefix)
	for _, c := range []struct {
		name string
		ctx  map[string]string
		want string
	}{
		{"schedule", map[string]string{"github.event_name": "schedule"}, "sync-pr"},
		{"dispatch", map[string]string{"github.event_name": "workflow_dispatch"}, "sync-pr"},
		{"the sync PR", workerEnvPRTContext("bot/worker-env-sync", "gastownhall/beads", "4321"), "sync-pr"},
		{"a fork PR from bot/worker-env-sync", workerEnvPRTContext("bot/worker-env-sync", "mallory/beads", "4322"), "4322"},
		{"another same-repo PR", workerEnvPRTContext("fix/thing", "gastownhall/beads", "77"), "77"},
	} {
		if got := mustEvalGHRunsOn(t, expr, c.ctx); got != c.want {
			t.Errorf("%s: concurrency group suffix = %q, want %q", c.name, got, c.want)
		}
	}
}

func workerEnvPRTContext(headRef, headRepo, number string) map[string]string {
	return map[string]string{
		"github.event_name":                             "pull_request_target",
		"github.repository":                             "gastownhall/beads",
		"github.event.pull_request.head.ref":            headRef,
		"github.event.pull_request.head.repo.full_name": headRepo,
		"github.event.pull_request.number":              number,
	}
}

// TestWorkerEnvSyncAutomergeVariableGate: nothing runs unless
// WORKER_ENV_SYNC_AUTOMERGE is 'true' (unset is today's review path, and
// the rollback), and no pull_request_target run but the sync PR's own gets
// past verify's if:. act and rerun need verify, so they are gated with it;
// the variable is read nowhere else in the file.
func TestWorkerEnvSyncAutomergeVariableGate(t *testing.T) {
	workflow := readCIWorkflow(t, workerEnvAutomergeWorkflow)
	verify := workflow.job(t, "verify")
	if verify.If != workerEnvAutomergeVerifyIf {
		t.Fatalf("verify if = %q, want %q", verify.If, workerEnvAutomergeVerifyIf)
	}
	on := map[string]string{"github.repository": "gastownhall/beads", "vars.WORKER_ENV_SYNC_AUTOMERGE": "true"}
	with := func(base map[string]string, kv ...string) map[string]string {
		m := map[string]string{}
		for k, v := range on {
			m[k] = v
		}
		for k, v := range base {
			m[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	syncPR := workerEnvPRTContext("bot/worker-env-sync", "gastownhall/beads", "4321")
	for _, c := range []struct {
		name string
		ctx  map[string]string
		want bool
	}{
		{"schedule", with(map[string]string{"github.event_name": "schedule"}), true},
		{"dispatch", with(map[string]string{"github.event_name": "workflow_dispatch"}), true},
		{"the sync PR", with(syncPR), true},
		{"variable unset", with(syncPR, "vars.WORKER_ENV_SYNC_AUTOMERGE", ""), false},
		{"variable false", with(map[string]string{"github.event_name": "schedule"}, "vars.WORKER_ENV_SYNC_AUTOMERGE", "false"), false},
		{"a fork PR from bot/worker-env-sync", with(workerEnvPRTContext("bot/worker-env-sync", "mallory/beads", "4322")), false},
		{"a same-repo PR from another branch", with(workerEnvPRTContext("bot/worker-env-sync-2", "gastownhall/beads", "4323")), false},
		{"a fork of this repository", with(map[string]string{"github.event_name": "schedule"}, "github.repository", "mallory/beads"), false},
	} {
		v, err := evalGHExpr(verify.If, c.ctx)
		if err != nil {
			t.Fatalf("evalGHExpr(%q): %v", verify.If, err)
		}
		if ghTruthy(v) != c.want {
			t.Errorf("%s: verify runs = %v, want %v", c.name, ghTruthy(v), c.want)
		}
	}
	for _, name := range []string{"act", "rerun"} {
		if job := workflow.job(t, name); !slices.Equal(job.Needs, []string{"verify"}) {
			t.Errorf("job %s needs %v, want [verify] (it is gated by verify's if: alone)", name, job.Needs)
		}
	}
	walkYAML(readYAMLNode(t, filepath.Join(".github", "workflows", workerEnvAutomergeWorkflow)), "", func(path string, key bool, value string) {
		if !key && strings.Contains(value, "WORKER_ENV_SYNC_AUTOMERGE") && path != ".jobs.verify.if" {
			t.Errorf("%s reads WORKER_ENV_SYNC_AUTOMERGE; only verify's if: may", path)
		}
	})
}

// TestWorkerEnvSyncAutomergePermissions: least privilege per job. The
// workflow grants nothing; verify reads (pull requests, contents for the
// files and the activity API, actions for the CI runs it may re-run); act
// writes only through the App tokens it mints, with no GITHUB_TOKEN
// permission at all; rerun alone may re-run jobs. Only act holds the
// autofix environment (the App's key).
func TestWorkerEnvSyncAutomergePermissions(t *testing.T) {
	root := readYAMLNode(t, filepath.Join(".github", "workflows", workerEnvAutomergeWorkflow))
	var top struct {
		Permissions map[string]string `yaml:"permissions"`
		Env         map[string]string `yaml:"env"`
	}
	if err := root.Decode(&top); err != nil {
		t.Fatal(err)
	}
	if top.Permissions == nil || len(top.Permissions) != 0 {
		t.Errorf("workflow permissions = %v, want {} (every job states its own)", top.Permissions)
	}
	if len(top.Env) != 0 {
		t.Errorf("workflow env = %v, want none", top.Env)
	}
	workflow := readCIWorkflow(t, workerEnvAutomergeWorkflow)
	var jobs []string
	for name := range workflow.Jobs {
		jobs = append(jobs, name)
	}
	sort.Strings(jobs)
	if want := []string{"act", "rerun", "verify"}; !slices.Equal(jobs, want) {
		t.Fatalf("jobs = %v, want %v", jobs, want)
	}
	for name, want := range map[string]struct {
		perms map[string]any
		env   string
	}{
		"verify": {map[string]any{"actions": "read", "contents": "read", "pull-requests": "read"}, ""},
		"act":    {map[string]any{}, "autofix"},
		"rerun":  {map[string]any{"actions": "write"}, ""},
	} {
		job := workflow.job(t, name)
		if !reflect.DeepEqual(job.Permissions, want.perms) {
			t.Errorf("job %s permissions = %v, want %v", name, job.Permissions, want.perms)
		}
		if job.Environment.Name != want.env {
			t.Errorf("job %s environment = %q, want %q", name, job.Environment.Name, want.env)
		}
		if job.RunsOn != "ubuntu-latest" {
			t.Errorf("job %s runs-on = %q, want ubuntu-latest (it holds a token; never a Blacksmith label)", name, job.RunsOn)
		}
		if job.TimeoutMinutes <= 0 || job.TimeoutMinutes > 10 {
			t.Errorf("job %s timeout-minutes = %d, want 1..10 (runs share one non-cancelling group)", name, job.TimeoutMinutes)
		}
		if len(job.Env) != 0 {
			t.Errorf("job %s env = %v, want none", name, job.Env)
		}
	}
	walkYAML(root, "", func(path string, key bool, value string) {
		if !key && strings.Contains(path, ".permissions.") && value != "read" && path != ".jobs.rerun.permissions.actions" {
			t.Errorf("%s = %q; only read, and rerun's actions: write", path, value)
		}
	})
}

// TestWorkerEnvSyncAutomergeNeverRunsPRCode pins the pull_request_target
// rules. The only checkout is verify's, of the run's own base commit
// (github.sha: main), sparse, with no token left on disk; the head's two
// files reach the verifier through the contents API as data. No run:
// interpolates ${{ }} (event data reaches scripts through env), nothing
// fetches, checks out or downloads anything (no artifact, no other run's
// output), no action but the checkout and the two App-token mints is
// used, and the secrets are those mints' app-id and private-key, the
// gascity-autofix App's alone.
func TestWorkerEnvSyncAutomergeNeverRunsPRCode(t *testing.T) {
	rel := filepath.Join(".github", "workflows", workerEnvAutomergeWorkflow)
	root := readYAMLNode(t, rel)
	workflow := readCIWorkflow(t, workerEnvAutomergeWorkflow)
	fetches := regexp.MustCompile(`\bgit\s+(fetch|clone|checkout|switch|worktree|pull|submodule)\b|\bgh\s+pr\s+checkout\b|\bgh\s+run\s+download\b|\bgh\s+release\s+download\b|download-artifact|zipball|tarball`)
	checkouts := 0
	for name, job := range workflow.Jobs {
		for i, step := range job.Steps {
			if strings.Contains(step.Run, "${{") {
				t.Errorf("job %s step %q interpolates an expression into run:\n%s", name, step.Name, step.Run)
			}
			if fetches.MatchString(step.Run) {
				t.Errorf("job %s step %q fetches, checks out or downloads code:\n%s", name, step.Name, step.Run)
			}
			if _, ok := step.With["run-id"]; ok {
				t.Errorf("job %s step %q downloads another run's artifacts", name, step.Name)
			}
			switch family := actionFamily(step.Uses); {
			case step.Uses == "":
			case family == "actions/checkout":
				checkouts++
				if name != "verify" || i != 0 || step.Uses != "actions/checkout@"+checkoutSHA {
					t.Errorf("job %s step %d checks out (%q); only verify's first step may, with actions/checkout@%s", name, i, step.Uses, checkoutSHA)
				}
				want := map[string]string{"ref": "${{ github.sha }}", "persist-credentials": "false", "sparse-checkout": "tools/rbe\n"}
				if !reflect.DeepEqual(step.With, want) {
					t.Errorf("checkout with = %q, want %q (the run's base commit, never a PR ref; no token on disk)", step.With, want)
				}
			case family == "actions/create-github-app-token":
				if name != "act" || step.Uses != "actions/create-github-app-token@"+appTokenActionSHA {
					t.Errorf("job %s step %q mints an App token with %q; only act may, with actions/create-github-app-token@%s", name, step.Name, step.Uses, appTokenActionSHA)
				}
			default:
				t.Errorf("job %s step %q uses %q; only the checkout and the App-token mint may be used", name, step.Name, step.Uses)
			}
		}
	}
	if checkouts != 1 {
		t.Errorf("%d checkouts, want exactly verify's", checkouts)
	}
	act := workflow.job(t, "act")
	allowed := map[string]bool{}
	for _, step := range []string{workerEnvMergeTokenStep, workerEnvPRTokenStep} {
		i := act.stepIndex(t, step)
		allowed[fmt.Sprintf(".jobs.act.steps[%d].with.app-id", i)] = true
		allowed[fmt.Sprintf(".jobs.act.steps[%d].with.private-key", i)] = true
	}
	secretRef := regexp.MustCompile(`\bsecrets\s*(\.|\[)`)
	walkYAML(root, "", func(path string, key bool, value string) {
		if key && value == "continue-on-error" && path != fmt.Sprintf(".jobs.act.steps[%d].continue-on-error", act.stepIndex(t, workerEnvMergeTokenStep)) {
			t.Errorf("%s hides a failure; only the merge-token mint may (its failure is the fallback signal)", path)
		}
		if !key && value == "inherit" {
			t.Errorf("%s: secrets: inherit", path)
		}
		if key || !secretRef.MatchString(value) {
			return
		}
		if !allowed[path] {
			t.Errorf("%s reads secrets (%q); only the two mints' app-id and private-key may", path, value)
		}
		for _, ref := range regexp.MustCompile(`secrets\.([A-Za-z0-9_]+)`).FindAllStringSubmatch(value, -1) {
			if ref[1] != "GASCITY_AUTOFIX_APP_ID" && ref[1] != "GASCITY_AUTOFIX_APP_PRIVATE_KEY" {
				t.Errorf("%s reads secret %s; only the gascity-autofix App's", path, ref[1])
			}
		}
	})
	// The verifier itself never runs the PR's files either: it reads them
	// through the API into a scratch directory, which only cmp, sed, diff
	// and sha256sum read.
	src := readPolicyFile(t, bazelPolicyRoot(t), workerEnvSyncVerify)
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if fetches.MatchString(line) || regexp.MustCompile(`\b(source|eval)\s|^\s*\.\s`).MatchString(line) {
			t.Errorf("%s: %q fetches or runs code", workerEnvSyncVerify, line)
		}
		if regexp.MustCompile(`\bgh\s+(pr|issue|run|workflow|label|release)\b|\s-X\s|--method|mutation`).MatchString(line) {
			t.Errorf("%s: %q writes to GitHub; the verifier only reads", workerEnvSyncVerify, line)
		}
	}
}

// TestWorkerEnvSyncAutomergeVerifyJob: verify checks out main's tools/rbe
// and runs the verifier with the read-only token and the event's facts in
// env. A verdict (exit 1 refused, 2 unknown) never fails the step; the
// outputs carry it to act. Anything above 2 is the script failing, which
// must not pass as a verdict.
func TestWorkerEnvSyncAutomergeVerifyJob(t *testing.T) {
	job := readCIWorkflow(t, workerEnvAutomergeWorkflow).job(t, "verify")
	if len(job.Steps) != 2 {
		t.Fatalf("verify has %d steps, want the checkout and the verifier", len(job.Steps))
	}
	step := job.Steps[1]
	wantEnv := map[string]string{
		"GH_TOKEN":     "${{ github.token }}",
		"REPO":         "${{ github.repository }}",
		"EVENT_NAME":   "${{ github.event_name }}",
		"EVENT_ACTION": "${{ github.event.action }}",
		"EVENT_SENDER": "${{ github.event.sender.login }}",
		"PR":           "${{ github.event.pull_request.number }}",
	}
	if step.Name != "Verify the sync PR" || step.ID != "verify" || step.If != "" || !reflect.DeepEqual(step.Env, wantEnv) ||
		!strings.Contains(step.Run, workerEnvSyncVerify+` "$PR"`) {
		t.Errorf("verify step name %q id %q if %q env %v run %q; want id verify running %s \"$PR\" with env %v", step.Name, step.ID, step.If, step.Env, step.Run, workerEnvSyncVerify, wantEnv)
	}
	wantOutputs := map[string]string{}
	for _, k := range []string{"pr", "head-sha", "node-id", "verdict", "armed", "queued", "arm", "disarm", "comment", "fallback-label", "rerun", "reason"} {
		wantOutputs[k] = "${{ steps.verify.outputs." + k + " }}"
	}
	if !reflect.DeepEqual(job.Outputs, wantOutputs) {
		t.Errorf("verify outputs = %v, want %v", job.Outputs, wantOutputs)
	}

	requireHostTool(t, "bash")
	dir := t.TempDir()
	for rc, want := range map[int]int{0: 0, 1: 0, 2: 0, 3: 3, 127: 127} {
		rbeTestWrite(t, filepath.Join(dir, workerEnvSyncVerify), fmt.Sprintf("#!/usr/bin/env bash\nexit %d\n", rc), 0o755)
		cmd := exec.Command("bash", "-e", "-c", step.Run)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PR=4321")
		out, err := cmd.CombinedOutput()
		if got := exitCode(err); got != want {
			t.Errorf("verify step with the verifier exiting %d: exit %d, want %d\n%s", rc, got, want, out)
		}
	}
}

// TestWorkerEnvSyncAutomergeActJob pins act: the gascity-autofix App's two
// tokens (the merge token, contents + pull-requests + merge-queues write,
// the set enablePullRequestAutoMerge needs on one token, to arm and
// disarm; the PR token, pull-requests write alone, to comment and to
// label), each used only by its steps, and after the mints only gh runs.
// The decision table runs every step's real if: through the expression
// evaluator: nothing arms unless verify said arm, the fallback label
// follows only a failed mint or a permission-denied arm, and a refusal
// disarms before it comments.
func TestWorkerEnvSyncAutomergeActJob(t *testing.T) {
	job := readCIWorkflow(t, workerEnvAutomergeWorkflow).job(t, "act")
	if job.If != workerEnvActIf {
		t.Errorf("act if = %q, want %q", job.If, workerEnvActIf)
	}
	if len(job.Steps) != len(workerEnvActSteps) {
		t.Fatalf("act has %d steps, want %d", len(job.Steps), len(workerEnvActSteps))
	}
	for i, want := range workerEnvActSteps {
		step := job.Steps[i]
		if step.Name != want.name || step.ID != want.id || step.If != want.ifExpr {
			t.Errorf("act step %d = %q id %q if %q; want %q id %q if %q", i, step.Name, step.ID, step.If, want.name, want.id, want.ifExpr)
		}
	}
	app := map[string]string{
		"app-id":       "${{ secrets.GASCITY_AUTOFIX_APP_ID }}",
		"private-key":  "${{ secrets.GASCITY_AUTOFIX_APP_PRIVATE_KEY }}",
		"owner":        "gastownhall",
		"repositories": "beads",
	}
	withPerms := func(perms ...string) map[string]string {
		m := map[string]string{}
		for k, v := range app {
			m[k] = v
		}
		for _, p := range perms {
			m["permission-"+p] = "write"
		}
		return m
	}
	merge := job.step(t, workerEnvMergeTokenStep)
	if !reflect.DeepEqual(merge.With, withPerms("contents", "pull-requests", "merge-queues")) || merge.ContinueOnError != true {
		t.Errorf("merge-token mint with %v continue-on-error %v; want %v and continue-on-error: true (a failed mint is the fallback signal)", merge.With, merge.ContinueOnError, withPerms("contents", "pull-requests", "merge-queues"))
	}
	prToken := job.step(t, workerEnvPRTokenStep)
	if !reflect.DeepEqual(prToken.With, withPerms("pull-requests")) || prToken.ContinueOnError != nil {
		t.Errorf("pr-token mint with %v continue-on-error %v; want %v", prToken.With, prToken.ContinueOnError, withPerms("pull-requests"))
	}
	wantEnv := map[string]map[string]string{
		workerEnvDisarmStep: {"GH_TOKEN": workerEnvMergeToken, "REPO": "${{ github.repository }}", "PR": "${{ needs.verify.outputs.pr }}",
			"NODE_ID": "${{ needs.verify.outputs.node-id }}", "ARMED": "${{ needs.verify.outputs.armed }}", "QUEUED": "${{ needs.verify.outputs.queued }}"},
		workerEnvArmStep: {"GH_TOKEN": workerEnvMergeToken, "REPO": "${{ github.repository }}", "PR": "${{ needs.verify.outputs.pr }}",
			"HEAD_SHA": "${{ needs.verify.outputs.head-sha }}"},
		workerEnvCommentStep: {"GH_TOKEN": workerEnvPRToken, "REPO": "${{ github.repository }}", "PR": "${{ needs.verify.outputs.pr }}",
			"HEAD_SHA": "${{ needs.verify.outputs.head-sha }}", "REASON": "${{ needs.verify.outputs.reason }}",
			"RUN_URL": "${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}"},
		workerEnvLabelStep: {"GH_TOKEN": workerEnvPRToken, "REPO": "${{ github.repository }}", "PR": "${{ needs.verify.outputs.pr }}",
			"LABEL": workerEnvSyncReviewLabel},
	}
	for name, env := range wantEnv {
		if got := job.step(t, name).Env; !reflect.DeepEqual(got, env) {
			t.Errorf("act step %q env = %v, want %v", name, got, env)
		}
	}
	arm := job.step(t, workerEnvArmStep)
	if !strings.Contains(arm.Run, `gh pr merge "$PR" --repo "$REPO" --auto --squash --match-head-commit "$HEAD_SHA"`) {
		t.Errorf("arm step run %q, want gh pr merge --auto --squash bound to the verified head with --match-head-commit", arm.Run)
	}
	// After the mints only gh runs: no step starts a command that runs a
	// script, a repository file or a network tool other than gh.
	for _, step := range job.Steps {
		for _, cmd := range workerEnvShellCommands(step.Run) {
			if slices.Contains([]string{"curl", "wget", "git", "bash", "sh", "python", "python3", "node", "make", "go", "source", "eval", "."}, cmd) ||
				strings.Contains(cmd, "/") {
				t.Errorf("act step %q runs %q; after the mints only gh runs", step.Name, cmd)
			}
		}
	}

	// The decision table.
	type outcome struct {
		verdict, armed, queued, arm, disarm, comment, fallback, mint, denied string
	}
	run := func(o outcome) []string {
		ctx := map[string]string{
			"needs.verify.outputs.verdict":        o.verdict,
			"needs.verify.outputs.armed":          o.armed,
			"needs.verify.outputs.queued":         o.queued,
			"needs.verify.outputs.arm":            o.arm,
			"needs.verify.outputs.disarm":         o.disarm,
			"needs.verify.outputs.comment":        o.comment,
			"needs.verify.outputs.fallback-label": o.fallback,
			"steps.arm.outputs.denied":            o.denied,
		}
		if v, err := evalGHExpr(job.If, ctx); err != nil || !ghTruthy(v) {
			return nil
		}
		var ran []string
		for _, s := range workerEnvActSteps {
			ctx["steps.merge-token.outcome"] = "skipped"
			if slices.Contains(ran, workerEnvMergeTokenStep) {
				ctx["steps.merge-token.outcome"] = o.mint
			}
			v, err := evalGHExpr(s.ifExpr, ctx)
			if err != nil {
				t.Fatalf("evalGHExpr(%q): %v", s.ifExpr, err)
			}
			if ghTruthy(v) {
				ran = append(ran, s.name)
			}
		}
		return ran
	}
	for _, c := range []struct {
		name string
		o    outcome
		want []string
	}{
		{"verified, not armed: arm", outcome{verdict: "arm", arm: "true", fallback: "true", mint: "success"},
			[]string{workerEnvMergeTokenStep, workerEnvArmStep}},
		{"verified, App lacks the permissions: label for review", outcome{verdict: "arm", arm: "true", fallback: "true", mint: "failure"},
			[]string{workerEnvMergeTokenStep, workerEnvPRTokenStep, workerEnvLabelStep}},
		{"verified, arming denied: label for review", outcome{verdict: "arm", arm: "true", fallback: "true", mint: "success", denied: "1"},
			[]string{workerEnvMergeTokenStep, workerEnvArmStep, workerEnvPRTokenStep, workerEnvLabelStep}},
		{"verified, arming denied, already in review: nothing more", outcome{verdict: "arm", arm: "true", fallback: "false", mint: "success", denied: "1"},
			[]string{workerEnvMergeTokenStep, workerEnvArmStep}},
		{"verified and already armed: nothing", outcome{verdict: "arm", armed: "true", arm: "false", fallback: "false"}, nil},
		{"refused, first time: comment", outcome{verdict: "refuse", arm: "false", comment: "true"},
			[]string{workerEnvPRTokenStep, workerEnvCommentStep}},
		{"refused and armed: disarm, then comment", outcome{verdict: "refuse", armed: "true", arm: "false", disarm: "true", comment: "true", mint: "success"},
			[]string{workerEnvMergeTokenStep, workerEnvDisarmStep, workerEnvPRTokenStep, workerEnvCommentStep}},
		{"refused, already commented: nothing", outcome{verdict: "refuse", arm: "false", comment: "false"}, nil},
		{"unknown and armed: disarm, no comment", outcome{verdict: "unknown", armed: "true", arm: "false", disarm: "true", mint: "success"},
			[]string{workerEnvMergeTokenStep, workerEnvDisarmStep}},
		{"unknown: nothing", outcome{verdict: "unknown", arm: "false"}, nil},
	} {
		if got := run(c.o); !slices.Equal(got, c.want) {
			t.Errorf("%s: act runs %q, want %q", c.name, got, c.want)
		}
	}
	for _, verdict := range []string{"refuse", "unknown", "none", ""} {
		for _, mint := range []string{"success", "failure"} {
			if got := run(outcome{verdict: verdict, arm: "false", disarm: "true", comment: "true", fallback: "true", mint: mint, denied: "1"}); slices.Contains(got, workerEnvArmStep) || slices.Contains(got, workerEnvLabelStep) {
				t.Errorf("verdict %q (arm false), mint %s: act runs %q; nothing arms or labels without verify's arm", verdict, mint, got)
			}
		}
	}
}

// workerEnvShellCommands lists the command word of every simple command in
// a run: script, roughly: each line and each part after ; && || | $( or a
// then/else/do keyword, past if/elif/while/!; comment lines and heredoc
// bodies (lines between <<WORD and WORD) are skipped.
func workerEnvShellCommands(script string) []string {
	var cmds []string
	split := regexp.MustCompile(`;|&&|\|\||\||\$\(|\bthen\b|\belse\b|\bdo\b`)
	heredoc := regexp.MustCompile(`<<-?'?([A-Z_]+)'?`)
	end := ""
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if end != "" {
			if trimmed == end {
				end = ""
			}
			continue
		}
		if m := heredoc.FindStringSubmatch(line); m != nil {
			end = m[1]
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, part := range split.Split(trimmed, -1) {
			fields := strings.Fields(part)
			for len(fields) > 0 && slices.Contains([]string{"if", "elif", "while", "!", "{", "(", "fi", "done", "esac", "}", ")"}, fields[0]) {
				fields = fields[1:]
			}
			for len(fields) > 0 && regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`).MatchString(fields[0]) {
				fields = fields[1:] // an assignment, or a prefix to the command
			}
			if len(fields) > 0 && !strings.HasSuffix(fields[0], ")") {
				cmds = append(cmds, fields[0])
			}
		}
	}
	return cmds
}

// workerEnvFakeGH stands in for gh. gh api GETs are served from
// $FAKE_GH_DIR/api/<endpoint with / as __> (a missing file is gh's 404); a
// file $FAKE_GH_DIR/fail/<subcommand>_<subsubcommand> makes that
// invocation fail with its content on stderr. Every call's argv is kept,
// NUL-separated, one file per call.
const workerEnvFakeGH = `#!/usr/bin/env bash
set -u
n=$(( $(cat "$FAKE_GH_DIR/count" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$FAKE_GH_DIR/count"
printf '%s\0' "$@" > "$FAKE_GH_DIR/call.$(printf '%03d' "$n")"
if [ "${1-}" = api ]; then
  shift
  endpoint=""
  while [ $# -gt 0 ]; do
    case "$1" in
    --paginate) ;;
    -H|-f|-F) shift ;;
    -X|--method) echo "fake gh: unexpected method $*" >&2; exit 126 ;;
    -*) echo "fake gh: unknown flag $1" >&2; exit 127 ;;
    *) if [ -z "$endpoint" ]; then endpoint=$1; fi ;;
    esac
    shift
  done
  f="$FAKE_GH_DIR/api/${endpoint//\//__}"
  if [ -f "$f" ]; then cat "$f"; exit 0; fi
  echo "gh: Not Found (HTTP 404) $endpoint" >&2
  exit 1
fi
f="$FAKE_GH_DIR/fail/${1-}_${2-}"
if [ -f "$f" ]; then cat "$f" >&2; exit 1; fi
exit 0
`

type workerEnvFakeGHRun struct {
	code   int
	out    string
	calls  [][]string
	output string // the step's GITHUB_OUTPUT, when the caller reads one
}

// runWithFakeGH runs script (a file path, or -c text when isText) under
// bash -e with the fake gh first on PATH, the files the test lays out in
// the fake's directory, and env; it returns the exit code, the output and
// every gh call.
func runWithFakeGH(t *testing.T, setup func(fakeDir string), args []string, cwd string, env ...string) workerEnvFakeGHRun {
	t.Helper()
	bash := requireHostTool(t, "bash")
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	fake := filepath.Join(dir, "gh-data")
	rbeTestWrite(t, filepath.Join(bin, "gh"), workerEnvFakeGH, 0o755)
	for _, sub := range []string{"api", "fail"} {
		if err := os.MkdirAll(filepath.Join(fake, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if setup != nil {
		setup(fake)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), append([]string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"FAKE_GH_DIR=" + fake,
		"GH_TOKEN=fake-token",
	}, env...)...)
	out, err := cmd.CombinedOutput()
	code := exitCode(err)
	if code < 0 {
		t.Fatalf("run %v: %v\n%s", args, err, out)
	}
	files, err := filepath.Glob(filepath.Join(fake, "call.*"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	var calls [][]string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, strings.Split(string(bytes.TrimSuffix(raw, []byte{0})), "\x00"))
	}
	return workerEnvFakeGHRun{code: code, out: string(out), calls: calls}
}

func workerEnvFakeAPI(t *testing.T, fakeDir, endpoint, body string) {
	t.Helper()
	rbeTestWrite(t, filepath.Join(fakeDir, "api", strings.ReplaceAll(endpoint, "/", "__")), body, 0o644)
}

// TestWorkerEnvSyncAutomergeActSteps runs act's real step scripts against
// the fake gh: arming is gh pr merge --auto bound to the verified head; a
// permission refusal ("Resource not accessible by integration") is the
// fallback signal (denied=1, the step passes), any other failure fails the
// step and labels nothing; a disarm takes a queued PR out of the merge
// queue (dequeuePullRequest; gh pr merge --disable-auto leaves a queued PR
// as it is) and disables auto-merge, and fails loudly with no token; the
// refusal comment carries the per-head marker and the reasons in a code
// block; the fallback labels with status/needs-review-auto.
func TestWorkerEnvSyncAutomergeActSteps(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("the act job runs only on ubuntu-latest; GOOS=%s", runtime.GOOS)
	}
	job := readCIWorkflow(t, workerEnvAutomergeWorkflow).job(t, "act")
	const head = "8a46b3460d09263096f6ef0061dccfa6b9d10965"
	base := []string{"REPO=gastownhall/beads", "PR=4321", "HEAD_SHA=" + head}
	stepRun := func(t *testing.T, name string, setup func(string), env ...string) workerEnvFakeGHRun {
		t.Helper()
		output := filepath.Join(t.TempDir(), "output")
		r := runWithFakeGH(t, setup, []string{"-e", "-c", job.step(t, name).Run}, t.TempDir(), append(append([]string{"GITHUB_OUTPUT=" + output}, base...), env...)...)
		got, _ := os.ReadFile(output)
		r.output = string(got)
		return r
	}
	armCall := []string{"pr", "merge", "4321", "--repo", "gastownhall/beads", "--auto", "--squash", "--match-head-commit", head}

	t.Run("arm", func(t *testing.T) {
		r := stepRun(t, workerEnvArmStep, nil)
		if r.code != 0 || !reflect.DeepEqual(r.calls, [][]string{armCall}) || r.output != "" {
			t.Errorf("arm: exit %d, calls %q, output %q; want exit 0, %q, no denied\n%s", r.code, r.calls, r.output, armCall, r.out)
		}
	})
	t.Run("arm refused for lack of permission", func(t *testing.T) {
		r := stepRun(t, workerEnvArmStep, func(d string) {
			rbeTestWrite(t, filepath.Join(d, "fail", "pr_merge"), "GraphQL: Resource not accessible by integration (enablePullRequestAutoMerge)\n", 0o644)
		})
		if r.code != 0 || r.output != "denied=1\n" || !strings.Contains(r.out, "::warning") {
			t.Errorf("denied arm: exit %d, output %q; want exit 0, denied=1 and a warning\n%s", r.code, r.output, r.out)
		}
	})
	t.Run("arm failing otherwise", func(t *testing.T) {
		r := stepRun(t, workerEnvArmStep, func(d string) {
			rbeTestWrite(t, filepath.Join(d, "fail", "pr_merge"), "GraphQL: Head sha didn't match expected head sha (enablePullRequestAutoMerge)\n", 0o644)
		})
		if r.code == 0 || r.output != "" {
			t.Errorf("failed arm: exit %d, output %q; want a failure and no denied\n%s", r.code, r.output, r.out)
		}
	})

	disarmEnv := []string{"NODE_ID=PR_kwDOQAyAEs6abc"}
	dequeue := func(calls [][]string) bool {
		for _, c := range calls {
			if len(c) > 1 && c[0] == "api" && c[1] == "graphql" && strings.Contains(strings.Join(c, " "), "dequeuePullRequest") &&
				slices.Contains(c, "id=PR_kwDOQAyAEs6abc") {
				return true
			}
		}
		return false
	}
	disable := []string{"pr", "merge", "4321", "--repo", "gastownhall/beads", "--disable-auto"}
	for _, c := range []struct {
		name, armed, queued string
		dequeue, disable    bool
	}{
		{"armed", "true", "false", false, true},
		{"queued", "false", "true", true, false},
		{"armed and queued", "true", "true", true, true},
	} {
		t.Run("disarm "+c.name, func(t *testing.T) {
			r := stepRun(t, workerEnvDisarmStep, func(d string) {
				workerEnvFakeAPI(t, d, "graphql", `{"data":{"dequeuePullRequest":{"clientMutationId":null}}}`)
			}, append(disarmEnv, "ARMED="+c.armed, "QUEUED="+c.queued)...)
			if r.code != 0 || dequeue(r.calls) != c.dequeue || slices.ContainsFunc(r.calls, func(call []string) bool { return slices.Equal(call, disable) }) != c.disable {
				t.Errorf("disarm %s: exit %d, calls %q; want dequeue=%v disable-auto=%v\n%s", c.name, r.code, r.calls, c.dequeue, c.disable, r.out)
			}
			if c.dequeue && c.disable && !dequeue(r.calls[:1]) {
				t.Errorf("disarm %s: calls %q; want the dequeue first", c.name, r.calls)
			}
		})
	}
	t.Run("disarm with no token", func(t *testing.T) {
		r := stepRun(t, workerEnvDisarmStep, nil, append(disarmEnv, "ARMED=true", "QUEUED=false", "GH_TOKEN=")...)
		if r.code == 0 || len(r.calls) != 0 || !strings.Contains(r.out, "::error") {
			t.Errorf("disarm with no merge token: exit %d, calls %q; want a failure with an error and no gh call\n%s", r.code, r.calls, r.out)
		}
	})

	t.Run("comment", func(t *testing.T) {
		reason := "author: opened by 'mallory', not gascity-autofix[bot]\nfiles: changes \"README.md\""
		r := stepRun(t, workerEnvCommentStep, nil, "REASON="+reason, "RUN_URL=https://github.com/gastownhall/beads/actions/runs/7")
		if r.code != 0 || len(r.calls) != 1 || !slices.Equal(r.calls[0][:5], []string{"pr", "comment", "4321", "--repo", "gastownhall/beads"}) {
			t.Fatalf("comment: exit %d, calls %q\n%s", r.code, r.calls, r.out)
		}
		body := r.calls[0][len(r.calls[0])-1]
		for _, want := range []string{"<!-- worker-env-sync-automerge refused " + head + " -->", "```text\n" + reason + "\n```", "https://github.com/gastownhall/beads/actions/runs/7", workerEnvSyncReviewLabel} {
			if !strings.Contains(body, want) {
				t.Errorf("comment body lacks %q:\n%s", want, body)
			}
		}
	})

	t.Run("label", func(t *testing.T) {
		r := stepRun(t, workerEnvLabelStep, nil, "LABEL="+workerEnvSyncReviewLabel)
		want := [][]string{{"pr", "edit", "4321", "--repo", "gastownhall/beads", "--add-label", workerEnvSyncReviewLabel}}
		if r.code != 0 || !reflect.DeepEqual(r.calls, want) {
			t.Errorf("label: exit %d, calls %q, want %q\n%s", r.code, r.calls, want, r.out)
		}
	})
}

// TestWorkerEnvSyncAutomergeRerunJob: rerun re-runs the failed jobs of the
// runs verify named, with the job's own token (actions: write, nothing
// else), and refuses a list that is not run ids before re-running any.
func TestWorkerEnvSyncAutomergeRerunJob(t *testing.T) {
	job := readCIWorkflow(t, workerEnvAutomergeWorkflow).job(t, "rerun")
	if job.If != workerEnvRerunIf || len(job.Steps) != 1 {
		t.Fatalf("rerun if %q, %d steps; want if %q and one step", job.If, len(job.Steps), workerEnvRerunIf)
	}
	step := job.Steps[0]
	wantEnv := map[string]string{"GH_TOKEN": "${{ github.token }}", "REPO": "${{ github.repository }}", "RUNS": "${{ needs.verify.outputs.rerun }}"}
	if !reflect.DeepEqual(step.Env, wantEnv) || step.Uses != "" {
		t.Errorf("rerun step env %v uses %q; want env %v and plain gh", step.Env, step.Uses, wantEnv)
	}
	if runtime.GOOS != "linux" {
		t.Skipf("the rerun job runs only on ubuntu-latest; GOOS=%s", runtime.GOOS)
	}
	r := runWithFakeGH(t, nil, []string{"-e", "-c", step.Run}, t.TempDir(), "REPO=gastownhall/beads", "RUNS=501 601")
	want := [][]string{
		{"run", "rerun", "501", "--failed", "--repo", "gastownhall/beads"},
		{"run", "rerun", "601", "--failed", "--repo", "gastownhall/beads"},
	}
	if r.code != 0 || !reflect.DeepEqual(r.calls, want) {
		t.Errorf("rerun: exit %d, calls %q, want %q\n%s", r.code, r.calls, want, r.out)
	}
	r = runWithFakeGH(t, nil, []string{"-e", "-c", step.Run}, t.TempDir(), "REPO=gastownhall/beads", "RUNS=501 $(id)")
	if r.code == 0 || len(r.calls) != 0 {
		t.Errorf("rerun of a non-numeric id: exit %d, calls %q; want a failure before any re-run\n%s", r.code, r.calls, r.out)
	}
}

// TestWorkerEnvSyncVerifyReviewLabelsMatchAutosync: the verifier's
// review-pipeline labels (a PR carrying one is in the review already, so
// the fallback never labels it) are worker-env-autosync.yml's, which
// decide whether it warns about an unlabeled sync PR: one list, two copies.
func TestWorkerEnvSyncVerifyReviewLabelsMatchAutosync(t *testing.T) {
	pr := readCIWorkflow(t, workerEnvAutosyncWorkflow).job(t, workerEnvAutosyncJob).step(t, "Open or update the sync PR")
	m := regexp.MustCompile(`select\(IN\(([^)]*)\)\)`).FindStringSubmatch(pr.Run)
	if m == nil {
		t.Fatalf("autosync PR step: no select(IN(...)) label list in %q", pr.Run)
	}
	var autosync []string
	for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[1], -1) {
		autosync = append(autosync, q[1])
	}
	src := readPolicyFile(t, bazelPolicyRoot(t), workerEnvSyncVerify)
	v := regexp.MustCompile(`(?m)^readonly review_labels='(\[[^']*\])'$`).FindStringSubmatch(src)
	if v == nil {
		t.Fatalf("%s: no readonly review_labels='[...]' line", workerEnvSyncVerify)
	}
	var verify []string
	if err := json.Unmarshal([]byte(v[1]), &verify); err != nil {
		t.Fatal(err)
	}
	sort.Strings(autosync)
	sort.Strings(verify)
	if !slices.Equal(autosync, verify) || len(verify) == 0 || !slices.Contains(verify, workerEnvSyncReviewLabel) {
		t.Errorf("review-pipeline labels: verifier %v, autosync %v; want the same non-empty list, holding %s", verify, autosync, workerEnvSyncReviewLabel)
	}
}

// workerEnvVerifyCase is one PR as GitHub's API shows it to the verifier.
type workerEnvVerifyCase struct {
	number                             int
	author, headRepo, baseRepo         string
	headRef, baseRef, state, headSHA   string
	draft, merged, autoMerge, queued   bool
	labels                             []string
	commits                            [][3]string // sha, author login, committer login ("" = no GitHub user)
	activity                           [][3]string // activity_type, after, actor
	files                              [][2]string // status, filename
	headManifest, headBuild, mainBuild string
	comments                           [][2]string            // user, body
	labeled                            []string               // labels the PR's events add, in order
	runs                               map[string][][4]string // workflow file: id, status, conclusion, run_attempt (newest first)
	openList                           []map[string]any
	hidePR                             bool
}

const workerEnvTestHead = "1111111111111111111111111111111111111111"

// newWorkerEnvVerifyCase: what worker-env-autosync.yml pushes when gascity
// pins manifest at pin, over a main that pins rbeTestStaleManifest.
func newWorkerEnvVerifyCase(manifest, pin string) workerEnvVerifyCase {
	stalePin := rbeTestPin(rbeTestStaleManifest)
	mainBuild := "# rbe_worker: the execution platform of every build.\n" + rbeTestPlatformBuild(stalePin) + "# --- end ---\n"
	return workerEnvVerifyCase{
		number: 4321, author: workerEnvSyncPRBot, headRepo: "gastownhall/beads", baseRepo: "gastownhall/beads",
		headRef: "bot/worker-env-sync", baseRef: "main", state: "open", headSHA: workerEnvTestHead,
		labels:       []string{"status/needs-triage"},
		commits:      [][3]string{{workerEnvTestHead, workerEnvSyncPushBot, workerEnvSyncPushBot}},
		activity:     [][3]string{{"force_push", workerEnvTestHead, workerEnvSyncPushBot}, {"branch_creation", "2222222222222222222222222222222222222222", workerEnvSyncPushBot}},
		files:        [][2]string{{"modified", "platforms/BUILD.bazel"}, {"modified", "tools/rbe/worker-env.txt"}},
		headManifest: manifest, headBuild: strings.Replace(mainBuild, stalePin, pin, 1), mainBuild: mainBuild,
		labeled: []string{"status/needs-triage"},
		runs: map[string][][4]string{
			"pr.yml":      {{"501", "completed", "success", "1"}},
			"pr-risk.yml": {{"601", "completed", "success", "1"}},
		},
	}
}

func (c workerEnvVerifyCase) serve(t *testing.T, dir string) {
	t.Helper()
	repo := "gastownhall/beads"
	js := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	pr := fmt.Sprint(c.number)
	var head map[string]any
	if c.headRepo != "" {
		head = map[string]any{"full_name": c.headRepo}
	}
	var autoMerge any
	if c.autoMerge {
		autoMerge = map[string]any{"merge_method": "squash", "enabled_by": map[string]any{"login": workerEnvSyncPRBot}}
	}
	var labels []map[string]any
	for _, l := range c.labels {
		labels = append(labels, map[string]any{"name": l})
	}
	if !c.hidePR {
		workerEnvFakeAPI(t, dir, "repos/"+repo+"/pulls/"+pr, js(map[string]any{
			"number": c.number, "node_id": "PR_kwDOQAyAEs6abc", "state": c.state, "merged": c.merged, "draft": c.draft,
			"user": map[string]any{"login": c.author}, "auto_merge": autoMerge, "commits": len(c.commits), "labels": labels,
			"head": map[string]any{"ref": c.headRef, "sha": c.headSHA, "repo": head},
			"base": map[string]any{"ref": c.baseRef, "repo": map[string]any{"full_name": c.baseRepo}},
		}))
	}
	var commits []map[string]any
	for _, cm := range c.commits {
		user := func(login string) any {
			if login == "" {
				return nil
			}
			return map[string]any{"login": login}
		}
		commits = append(commits, map[string]any{"sha": cm[0], "author": user(cm[1]), "committer": user(cm[2])})
	}
	workerEnvFakeAPI(t, dir, "repos/"+repo+"/pulls/"+pr+"/commits?per_page=100", js(commits))
	var activity []map[string]any
	for _, a := range c.activity {
		activity = append(activity, map[string]any{"activity_type": a[0], "after": a[1], "ref": "refs/heads/bot/worker-env-sync",
			"actor": map[string]any{"login": a[2]}, "timestamp": "2026-10-24T01:00:00Z"})
	}
	workerEnvFakeAPI(t, dir, "repos/"+repo+"/activity?ref=refs/heads/bot/worker-env-sync&per_page=100", js(activity))
	var files []map[string]any
	for _, f := range c.files {
		files = append(files, map[string]any{"status": f[0], "filename": f[1]})
	}
	workerEnvFakeAPI(t, dir, "repos/"+repo+"/pulls/"+pr+"/files?per_page=100", js(files))
	workerEnvFakeAPI(t, dir, "repos/"+repo+"/contents/tools/rbe/worker-env.txt?ref="+c.headSHA, c.headManifest)
	workerEnvFakeAPI(t, dir, "repos/"+repo+"/contents/platforms/BUILD.bazel?ref="+c.headSHA, c.headBuild)
	workerEnvFakeAPI(t, dir, "repos/"+repo+"/contents/platforms/BUILD.bazel?ref=main", c.mainBuild)
	workerEnvFakeAPI(t, dir, "graphql", js(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"isInMergeQueue": c.queued}}}}))
	var comments []map[string]any
	for _, cm := range c.comments {
		comments = append(comments, map[string]any{"user": map[string]any{"login": cm[0]}, "body": cm[1]})
	}
	workerEnvFakeAPI(t, dir, "repos/"+repo+"/issues/"+pr+"/comments?per_page=100", js(comments))
	var events []map[string]any
	for _, l := range c.labeled {
		events = append(events, map[string]any{"event": "labeled", "label": map[string]any{"name": l}})
	}
	workerEnvFakeAPI(t, dir, "repos/"+repo+"/issues/"+pr+"/events?per_page=100", js(events))
	for wf, runs := range c.runs {
		var list []map[string]any
		for _, r := range runs {
			var conclusion any = r[2]
			if r[2] == "" {
				conclusion = nil
			}
			var id, attempt int
			fmt.Sscan(r[0], &id)
			fmt.Sscan(r[3], &attempt)
			list = append(list, map[string]any{"id": id, "status": r[1], "conclusion": conclusion, "run_attempt": attempt, "head_sha": c.headSHA, "event": "pull_request"})
		}
		workerEnvFakeAPI(t, dir, "repos/"+repo+"/actions/workflows/"+wf+"/runs?event=pull_request&head_sha="+c.headSHA+"&per_page=100",
			js(map[string]any{"total_count": len(list), "workflow_runs": list}))
	}
	if c.openList != nil {
		workerEnvFakeAPI(t, dir, "repos/"+repo+"/pulls?state=open&base=main&head=gastownhall:bot/worker-env-sync&per_page=100", js(c.openList))
	}
}

// parseWorkerEnvOutput reads a $GITHUB_OUTPUT file: name=value lines and
// name<<DELIMITER heredocs.
func parseWorkerEnvOutput(t *testing.T, text string) map[string]string {
	t.Helper()
	out := map[string]string{}
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		if name, delim, ok := strings.Cut(line, "<<"); ok && !strings.Contains(name, "=") {
			var body []string
			for i++; i < len(lines) && lines[i] != delim; i++ {
				body = append(body, lines[i])
			}
			if i == len(lines) {
				t.Fatalf("GITHUB_OUTPUT heredoc %s has no closing %s:\n%s", name, delim, text)
			}
			out[name] = strings.Join(body, "\n")
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("GITHUB_OUTPUT line %q is neither name=value nor a heredoc", line)
		}
		out[name] = value
	}
	return out
}

// TestWorkerEnvSyncVerify runs tools/rbe/worker-env-sync-verify from a
// scratch copy of the trusted scripts against the fake gh's API (the
// head's files served by the contents API) and the shared gascity fixture
// (rbe_worker_env_test.go, main resolved with git ls-remote). A good sync
// PR is armed; each departure from what worker-env-autosync.yml pushes is
// refused with its own reason; what cannot be decided is unknown, which
// never arms. It checks the decisions the act and rerun jobs carry out, and
// that every gh call it makes is a read.
func TestWorkerEnvSyncVerify(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("the verifier runs only on worker-env-sync-automerge.yml's ubuntu-latest runner; GOOS=%s", runtime.GOOS)
	}
	requireHostTool(t, "jq")
	requireHostTool(t, "sha256sum")
	f := newRBEWorkerEnvFixture(t)
	trusted := t.TempDir()
	for _, script := range []string{rbeWorkerEnvSync, rbeWorkerEnvSyncRef, workerEnvSyncVerify} {
		rbeTestWrite(t, filepath.Join(trusted, script), readPolicyFile(t, bazelPolicyRoot(t), script), 0o755)
	}
	// A pin gascity's own BUILD carries without it hashing gascity's
	// manifest: the head copies both, so only the sha256 check can see it.
	bogusPin := rbeTestPin("not the manifest\n")
	inconsistent := newRBEWorkerEnvGascity(t, f.manifest, rbeTestPlatformBuild(bogusPin))
	marker := func(sha string) string { return "<!-- worker-env-sync-automerge refused " + sha + " -->\nrefused" }

	type want struct {
		code    int
		verdict string
		fails   []string // check names that must FAIL
		unknown []string // check names that must be UNKNOWN
		outputs map[string]string
	}
	for _, c := range []struct {
		name    string
		mutate  func(*workerEnvVerifyCase)
		gascity *rbeWorkerEnvFixture
		env     []string
		noArg   bool
		want    want
	}{
		{name: "good sync PR", want: want{0, "arm", nil, nil, map[string]string{
			"pr": "4321", "head-sha": workerEnvTestHead, "node-id": "PR_kwDOQAyAEs6abc", "armed": "false", "queued": "false",
			"arm": "true", "disarm": "false", "comment": "false", "fallback-label": "true", "rerun": ""}}},
		{name: "good sync PR on a synchronize the bot pushed", env: []string{"EVENT_ACTION=synchronize", "EVENT_SENDER=" + workerEnvSyncPushBot},
			want: want{0, "arm", nil, nil, map[string]string{"arm": "true"}}},
		{name: "already armed", mutate: func(c *workerEnvVerifyCase) { c.autoMerge = true },
			want: want{0, "arm", nil, nil, map[string]string{"armed": "true", "arm": "false", "disarm": "false", "fallback-label": "false"}}},
		{name: "already queued", mutate: func(c *workerEnvVerifyCase) { c.queued = true },
			want: want{0, "arm", nil, nil, map[string]string{"queued": "true", "arm": "false", "disarm": "false"}}},
		{name: "wrong author", mutate: func(c *workerEnvVerifyCase) { c.author = "mallory" },
			want: want{1, "refuse", []string{"author"}, nil, map[string]string{"arm": "false", "comment": "true", "disarm": "false"}}},
		{name: "wrong branch", mutate: func(c *workerEnvVerifyCase) { c.headRef = "bot/worker-env-sync-2" },
			want: want{1, "refuse", []string{"head-ref"}, nil, map[string]string{"arm": "false"}}},
		{name: "fork", mutate: func(c *workerEnvVerifyCase) { c.headRepo = "mallory/beads" },
			want: want{1, "refuse", []string{"head-repo"}, nil, map[string]string{"arm": "false"}}},
		{name: "deleted head repository", mutate: func(c *workerEnvVerifyCase) { c.headRepo = "" },
			want: want{1, "refuse", []string{"head-repo"}, nil, map[string]string{"arm": "false"}}},
		{name: "base not main", mutate: func(c *workerEnvVerifyCase) { c.baseRef = "release/v1.3" },
			want: want{1, "refuse", []string{"base-ref"}, nil, map[string]string{"arm": "false"}}},
		{name: "draft", mutate: func(c *workerEnvVerifyCase) { c.draft = true },
			want: want{1, "refuse", []string{"draft"}, nil, map[string]string{"arm": "false"}}},
		{name: "closed", mutate: func(c *workerEnvVerifyCase) { c.state, c.merged = "closed", true },
			want: want{1, "refuse", []string{"open"}, nil, map[string]string{"arm": "false", "comment": "false", "disarm": "false"}}},
		{name: "synchronize sent by someone else", env: []string{"EVENT_ACTION=synchronize", "EVENT_SENDER=mallory"},
			want: want{1, "refuse", []string{"sender"}, nil, map[string]string{"arm": "false"}}},
		{name: "commit by another author", mutate: func(c *workerEnvVerifyCase) { c.commits[0][1] = "mallory" },
			want: want{1, "refuse", []string{"commits"}, nil, map[string]string{"arm": "false"}}},
		{name: "commit by another committer", mutate: func(c *workerEnvVerifyCase) { c.commits[0][2] = "web-flow" },
			want: want{1, "refuse", []string{"commits"}, nil, map[string]string{"arm": "false"}}},
		{name: "commit no GitHub user wrote", mutate: func(c *workerEnvVerifyCase) {
			c.commits = append(c.commits, [3]string{"3333333333333333333333333333333333333333", "", workerEnvSyncPushBot})
		}, want: want{1, "refuse", []string{"commits"}, nil, map[string]string{"arm": "false"}}},
		{name: "push by another actor", mutate: func(c *workerEnvVerifyCase) { c.activity[0][2] = "mallory" },
			want: want{1, "refuse", []string{"pusher"}, nil, map[string]string{"arm": "false"}}},
		{name: "extra file", mutate: func(c *workerEnvVerifyCase) { c.files = append(c.files, [2]string{"added", "README.md"}) },
			want: want{1, "refuse", []string{"files"}, nil, map[string]string{"arm": "false"}}},
		{name: "manifest only", mutate: func(c *workerEnvVerifyCase) { c.files = c.files[1:] },
			want: want{1, "refuse", []string{"files"}, nil, map[string]string{"arm": "false"}}},
		{name: "edited non-pin BUILD line", mutate: func(c *workerEnvVerifyCase) {
			c.headBuild = strings.Replace(c.headBuild, "# --- end ---", "# --- edited ---", 1)
		}, want: want{1, "refuse", []string{"build-file"}, nil, map[string]string{"arm": "false"}}},
		{name: "two pin lines", mutate: func(c *workerEnvVerifyCase) {
			c.headBuild = strings.Replace(c.headBuild, "    },\n", "        \"worker-env\": \""+f.pin+"\",\n    },\n", 1)
		}, want: want{1, "refuse", []string{"build-file", "gascity"}, nil, map[string]string{"arm": "false"}}},
		{name: "manifest differs from gascity's", mutate: func(c *workerEnvVerifyCase) {
			c.headManifest = "arch x86_64\npkg git 3\n"
			c.headBuild = strings.Replace(c.headBuild, f.pin, rbeTestPin(c.headManifest), 1)
		}, want: want{1, "refuse", []string{"gascity"}, nil, map[string]string{"arm": "false"}}},
		{name: "pin is not the manifest's sha256", gascity: &inconsistent, mutate: func(c *workerEnvVerifyCase) {
			c.headBuild = strings.Replace(c.headBuild, f.pin, bogusPin, 1)
		}, want: want{1, "refuse", []string{"pin"}, nil, map[string]string{"arm": "false"}}},
		{name: "refused while armed", mutate: func(c *workerEnvVerifyCase) { c.author, c.autoMerge = "mallory", true },
			want: want{1, "refuse", []string{"author"}, nil, map[string]string{"armed": "true", "disarm": "true", "comment": "true"}}},
		{name: "refused while queued", mutate: func(c *workerEnvVerifyCase) { c.author, c.queued = "mallory", true },
			want: want{1, "refuse", []string{"author"}, nil, map[string]string{"queued": "true", "disarm": "true"}}},
		{name: "refusal already commented on this head", mutate: func(c *workerEnvVerifyCase) {
			c.author = "mallory"
			c.comments = [][2]string{{workerEnvSyncPRBot, marker(workerEnvTestHead)}}
		}, want: want{1, "refuse", []string{"author"}, nil, map[string]string{"comment": "false"}}},
		{name: "refusal commented on an earlier head", mutate: func(c *workerEnvVerifyCase) {
			c.author = "mallory"
			c.comments = [][2]string{{workerEnvSyncPRBot, marker("2222222222222222222222222222222222222222")}}
		}, want: want{1, "refuse", []string{"author"}, nil, map[string]string{"comment": "true"}}},
		{name: "marker someone else posted", mutate: func(c *workerEnvVerifyCase) {
			c.author = "mallory"
			c.comments = [][2]string{{"mallory", marker(workerEnvTestHead)}}
		}, want: want{1, "refuse", []string{"author"}, nil, map[string]string{"comment": "true"}}},
		{name: "activity not yet showing the head's push", mutate: func(c *workerEnvVerifyCase) {
			c.activity[0][1] = "2222222222222222222222222222222222222222"
		}, want: want{2, "unknown", nil, []string{"pusher"}, map[string]string{"arm": "false", "comment": "false", "disarm": "false"}}},
		{name: "unknown while armed", mutate: func(c *workerEnvVerifyCase) {
			c.activity[0][1] = "2222222222222222222222222222222222222222"
			c.autoMerge = true
		}, want: want{2, "unknown", nil, []string{"pusher"}, map[string]string{"disarm": "true", "comment": "false"}}},
		{name: "gascity unreachable", env: []string{"WORKER_ENV_SYNC_GIT_URL=file:///nonexistent-gascity"},
			want: want{2, "unknown", nil, []string{"gascity"}, map[string]string{"arm": "false"}}},
		{name: "PR unreadable", mutate: func(c *workerEnvVerifyCase) { c.hidePR = true },
			want: want{2, "unknown", nil, []string{"pr"}, map[string]string{"arm": "false", "disarm": "false"}}},
		{name: "fallback: already in the review pipeline", mutate: func(c *workerEnvVerifyCase) { c.labels = append(c.labels, "status/reviewing") },
			want: want{0, "arm", nil, nil, map[string]string{"arm": "true", "fallback-label": "false"}}},
		{name: "fallback: labeled for review once before", mutate: func(c *workerEnvVerifyCase) { c.labeled = append(c.labeled, workerEnvSyncReviewLabel) },
			want: want{0, "arm", nil, nil, map[string]string{"arm": "true", "fallback-label": "false"}}},
		{name: "rerun: schedule, failed first attempt", env: []string{"EVENT_NAME=schedule"}, mutate: func(c *workerEnvVerifyCase) {
			c.runs["pr.yml"] = [][4]string{{"501", "completed", "failure", "1"}}
		}, want: want{0, "arm", nil, nil, map[string]string{"rerun": "501"}}},
		{name: "rerun: cancelled and timed out", env: []string{"EVENT_NAME=workflow_dispatch"}, mutate: func(c *workerEnvVerifyCase) {
			c.runs["pr.yml"] = [][4]string{{"501", "completed", "cancelled", "1"}}
			c.runs["pr-risk.yml"] = [][4]string{{"601", "completed", "timed_out", "1"}}
		}, want: want{0, "arm", nil, nil, map[string]string{"rerun": "501 601"}}},
		{name: "rerun: never a second attempt", env: []string{"EVENT_NAME=schedule"}, mutate: func(c *workerEnvVerifyCase) {
			c.runs["pr.yml"] = [][4]string{{"501", "completed", "failure", "2"}}
		}, want: want{0, "arm", nil, nil, map[string]string{"rerun": ""}}},
		{name: "rerun: an earlier run of the head was re-run", env: []string{"EVENT_NAME=schedule"}, mutate: func(c *workerEnvVerifyCase) {
			c.runs["pr.yml"] = [][4]string{{"502", "completed", "failure", "1"}, {"501", "completed", "failure", "2"}}
		}, want: want{0, "arm", nil, nil, map[string]string{"rerun": ""}}},
		{name: "rerun: still running", env: []string{"EVENT_NAME=schedule"}, mutate: func(c *workerEnvVerifyCase) {
			c.runs["pr.yml"] = [][4]string{{"501", "in_progress", "", "1"}}
		}, want: want{0, "arm", nil, nil, map[string]string{"rerun": ""}}},
		{name: "rerun: not on a pull_request_target event", mutate: func(c *workerEnvVerifyCase) {
			c.runs["pr.yml"] = [][4]string{{"501", "completed", "failure", "1"}}
		}, want: want{0, "arm", nil, nil, map[string]string{"rerun": ""}}},
		{name: "rerun: never for a refused PR", env: []string{"EVENT_NAME=schedule"}, mutate: func(c *workerEnvVerifyCase) {
			c.author = "mallory"
			c.runs["pr.yml"] = [][4]string{{"501", "completed", "failure", "1"}}
		}, want: want{1, "refuse", []string{"author"}, nil, map[string]string{"rerun": ""}}},
		{name: "schedule finds the open sync PR", noArg: true, env: []string{"EVENT_NAME=schedule"}, mutate: func(c *workerEnvVerifyCase) {
			c.openList = []map[string]any{
				{"number": 4322, "head": map[string]any{"ref": "bot/worker-env-sync", "repo": map[string]any{"full_name": "mallory/beads"}}},
				{"number": 4321, "head": map[string]any{"ref": "bot/worker-env-sync", "repo": map[string]any{"full_name": "gastownhall/beads"}}},
			}
		}, want: want{0, "arm", nil, nil, map[string]string{"pr": "4321", "arm": "true"}}},
		{name: "schedule with no open sync PR", noArg: true, env: []string{"EVENT_NAME=schedule"}, mutate: func(c *workerEnvVerifyCase) {
			c.openList = []map[string]any{}
		}, want: want{0, "none", nil, nil, map[string]string{"arm": "false", "disarm": "false", "comment": "false", "rerun": ""}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := f
			if c.gascity != nil {
				g = *c.gascity
			}
			pr := newWorkerEnvVerifyCase(f.manifest, f.pin)
			if c.mutate != nil {
				pr.mutate(c.mutate)
			}
			outputFile := filepath.Join(t.TempDir(), "output")
			env := append(g.env(), "REPO=gastownhall/beads", "EVENT_NAME=pull_request_target", "EVENT_ACTION=opened", "EVENT_SENDER="+workerEnvSyncPRBot,
				"GITHUB_OUTPUT="+outputFile, "GITHUB_STEP_SUMMARY="+filepath.Join(t.TempDir(), "summary"), "WORKER_ENV_SYNC_VERIFY_RETRY_DELAY=0")
			env = append(env, c.env...)
			args := []string{filepath.Join(trusted, workerEnvSyncVerify)}
			if !c.noArg {
				args = append(args, "4321")
			}
			r := runWithFakeGH(t, func(d string) { pr.serve(t, d) }, args, t.TempDir(), env...)
			raw, _ := os.ReadFile(outputFile)
			out := parseWorkerEnvOutput(t, string(raw))
			if r.code != c.want.code || out["verdict"] != c.want.verdict {
				t.Fatalf("exit %d verdict %q, want exit %d verdict %q\n%s", r.code, out["verdict"], c.want.code, c.want.verdict, r.out)
			}
			for k, v := range c.want.outputs {
				if out[k] != v {
					t.Errorf("output %s = %q, want %q\n%s", k, out[k], v, r.out)
				}
			}
			gotFails := regexp.MustCompile(`(?m)^FAIL ([a-z-]+):`).FindAllStringSubmatch(r.out, -1)
			gotUnknown := regexp.MustCompile(`(?m)^UNKNOWN ([a-z-]+):`).FindAllStringSubmatch(r.out, -1)
			names := func(ms [][]string) []string {
				var n []string
				for _, m := range ms {
					n = append(n, m[1])
				}
				return n
			}
			if got := names(gotFails); !slices.Equal(got, c.want.fails) {
				t.Errorf("failed checks %v, want %v\n%s", got, c.want.fails, r.out)
			}
			if got := names(gotUnknown); !slices.Equal(got, c.want.unknown) {
				t.Errorf("undecided checks %v, want %v\n%s", got, c.want.unknown, r.out)
			}
			switch c.want.verdict {
			case "refuse":
				for _, check := range c.want.fails {
					if !strings.Contains(out["reason"], check+": ") {
						t.Errorf("reason %q does not name the failed check %s", out["reason"], check)
					}
				}
				if !strings.Contains(r.out, "::error title=worker-env sync PR refused::") {
					t.Errorf("no refusal annotation:\n%s", r.out)
				}
			case "unknown":
				if !strings.Contains(r.out, "::warning title=worker-env sync PR not verified::") {
					t.Errorf("no unknown annotation:\n%s", r.out)
				}
			}
			for _, call := range r.calls {
				joined := strings.Join(call, " ")
				if call[0] != "api" || slices.Contains(call, "-X") || slices.Contains(call, "--method") || strings.Contains(joined, "mutation") {
					t.Errorf("the verifier wrote to GitHub: gh %q", call)
				}
			}
		})
	}
}

func (c *workerEnvVerifyCase) mutate(f func(*workerEnvVerifyCase)) {
	// Deep-copy the slices a case may edit in place.
	c.commits = slices.Clone(c.commits)
	c.activity = slices.Clone(c.activity)
	c.files = slices.Clone(c.files)
	c.labels = slices.Clone(c.labels)
	c.labeled = slices.Clone(c.labeled)
	runs := map[string][][4]string{}
	for k, v := range c.runs {
		runs[k] = slices.Clone(v)
	}
	c.runs = runs
	f(c)
}
