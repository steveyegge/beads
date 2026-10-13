package scripts_test

import (
	"cmp"
	"maps"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The dolt-int union lane (item 5 A2): in mode remote, bazel.yml's
// bazel-dolt-int job runs --config=dolt-int, the union of
// --config=doltserver-integration and --config=doltserver-cmd, in one
// `bazel test`, in place of the bazel-server-storage and bazel-cmd-dolt jobs;
// in every other mode those two run as before. Unlike dolt-race, the members
// have different gating categories: bazel-server-storage is plain-gated
// (BAZEL_SERVER_STORAGE), bazel-cmd-dolt is flag-gated and advisory
// (BAZEL_CMD_DOLT, continue-on-error) until pr.yml's BAZEL_CMD_DOLT_REQUIRED
// is "true". The union carries bazel-cmd-dolt's own continue-on-error, so
// server-storage's requirement (read directly off the job's result) is
// unaffected and a cmd-dolt-only failure stays out of the call's own
// aggregate (BAZEL), exactly as it already is through bazel-cmd-dolt outside
// mode remote.
const (
	bazelDoltIntJobName = "bazel-dolt-int"
	bazelDoltIntConfig  = "dolt-int"
	// Mode remote, on every event that reaches it: exactly where the members
	// do not run.
	bazelDoltIntJobIf = "${{ needs.rbe.outputs.mode == 'remote' }}"
)

func init() {
	// bazelUnionLanes (declared in bazel_dolt_race_test.go): a union job -> the
	// gated member jobs it runs in place of in mode remote. Each member keeps
	// its gate id/flag and its workflow_call output, which reads the union's
	// result first and the member's own otherwise (bazelLaneCallOutput).
	bazelUnionLanes[bazelDoltIntJobName] = []string{bazelServerJobName, bazelCmdDoltJobName}
}

// bazelDoltIntJobTimeoutMargin: as bazelDoltRaceJobTimeoutMargin.
const bazelDoltIntJobTimeoutMargin = 5

// bazelDoltIntTestStepTimeout: the union's `bazel test` step timeout. Both
// members share the same 1200 s Bazel test_timeout cap; the step allows about
// 3 minutes past that cap so Bazel writes TIMEOUT logs before GitHub kills the
// step.
const bazelDoltIntTestStepTimeout = 23

// The union's members, and the tag filter each owns.
var bazelDoltIntMembers = []struct{ config, tag string }{
	{"doltserver-integration", "dolt-server-integration"},
	{"doltserver-cmd", "dolt-server-cmd"},
}

// TestBazelDoltIntConfigIsMemberUnion: --config=dolt-int is exactly the set
// union of its members' lines, except that each member's --test_tag_filters
// is replaced by one filter naming both members' tags. Every flag stays a
// member's own, so each member target's test action is unchanged (item 5 A2,
// aquery-verified byte-identical actionKeys). Both arms run, the mode
// deciding which, so drift between them would split results by mode: a flag
// added to a member must be added to the union too.
func TestBazelDoltIntConfigIsMemberUnion(t *testing.T) {
	rc := readPolicyFile(t, bazelPolicyRoot(t), ".bazelrc")
	const tagPrefix = "test --test_tag_filters="

	var want []string
	var tags []string
	for _, m := range bazelDoltIntMembers {
		lines := bazelRCConfigLines(rc, m.config)
		if len(lines) == 0 {
			t.Fatalf(".bazelrc has no --config=%s lines", m.config)
		}
		var memberTags []string
		for _, l := range lines {
			if v, ok := strings.CutPrefix(l, tagPrefix); ok {
				memberTags = append(memberTags, v)
				continue
			}
			if !slices.Contains(want, l) {
				want = append(want, l)
			}
		}
		if len(memberTags) != 1 || memberTags[0] != m.tag {
			t.Errorf("--config=%s tag filters %v, want exactly [%s]", m.config, memberTags, m.tag)
		}
		tags = append(tags, m.tag)
	}

	got := bazelRCConfigLines(rc, bazelDoltIntConfig)
	var gotTags []string
	var gotRest []string
	for _, l := range got {
		if v, ok := strings.CutPrefix(l, tagPrefix); ok {
			gotTags = append(gotTags, v)
			continue
		}
		gotRest = append(gotRest, l)
	}
	if wantTag := strings.Join(tags, ","); len(gotTags) != 1 || gotTags[0] != wantTag {
		t.Errorf("--config=%s tag filters %v, want exactly [%s]", bazelDoltIntConfig, gotTags, wantTag)
	}
	if !sameStringSet(gotRest, want) {
		t.Errorf("--config=%s is not the union of its members' lines (tag filter aside):\ngot:\n%s\nwant (any order):\n%s",
			bazelDoltIntConfig, strings.Join(gotRest, "\n"), strings.Join(want, "\n"))
	}
	if len(gotRest) != len(want) {
		t.Errorf("--config=%s repeats a line: %d lines for %d distinct member lines", bazelDoltIntConfig, len(gotRest), len(want))
	}
	// No nested --config: the union is spelled out, so the policy above sees
	// every flag.
	for _, l := range got {
		if strings.Contains(l, "--config=") {
			t.Errorf("--config=%s line %q: spell the members' flags out, no nested --config", bazelDoltIntConfig, l)
		}
	}
}

// TestBazelDoltIntSoleRunIsInTheUnion: where the members execute remotely
// (the fork modes) they add --config=sole-run; the union carries that
// config's lines itself, so both tiers' actions are the same in either arm.
func TestBazelDoltIntSoleRunIsInTheUnion(t *testing.T) {
	rc := readPolicyFile(t, bazelPolicyRoot(t), ".bazelrc")
	sole := bazelRCConfigLines(rc, "sole-run")
	if len(sole) == 0 {
		t.Fatal(".bazelrc has no --config=sole-run lines")
	}
	union := bazelRCConfigLines(rc, bazelDoltIntConfig)
	for _, l := range sole {
		if !slices.Contains(union, l) {
			t.Errorf("--config=sole-run line %q is not in --config=%s: the members' remote runs and the union would differ", l, bazelDoltIntConfig)
		}
	}
	// And the union job takes no sole-run of its own: nothing to add.
	test := readCIWorkflow(t, bazelWorkflowName).job(t, bazelDoltIntJobName).step(t, "bazel test //... --config="+bazelDoltIntConfig)
	if strings.Contains(test.Run, "BAZEL_SOLE_RUN") || strings.Contains(test.Run, "sole-run") {
		t.Errorf("%s test step adds sole-run; --config=%s already has its lines", bazelDoltIntJobName, bazelDoltIntConfig)
	}
}

// TestBazelDoltIntIsGated pins the cutover: the union job carries
// bazel-cmd-dolt's own continue-on-error (the mixed-gating design: see the
// package comment above), it runs the union config with the members' checks
// once over its BEP (the new Shard balance step is bespoke, --label-scoped to
// server-storage's own target), and both members' workflow_call outputs read
// its result first.
func TestBazelDoltIntIsGated(t *testing.T) {
	workflow := readCIWorkflow(t, bazelWorkflowName)
	job := workflow.job(t, bazelDoltIntJobName)

	if job.If != bazelDoltIntJobIf {
		t.Errorf("%s if: %q, want %q", bazelDoltIntJobName, job.If, bazelDoltIntJobIf)
	}
	// Unlike dolt-race (both members plain-gated): bazel-cmd-dolt is
	// flag-gated/advisory with its own continue-on-error everywhere else, so
	// the union must carry the same, or a cmd-dolt-only failure in mode
	// remote would redden the call's aggregate (BAZEL) when it would not in
	// any other mode. Server-storage's requirement is unaffected: pr.yml's
	// ci-gate reads BAZEL_SERVER_STORAGE off this job's result (job.status,
	// via the always() "Record job result" step) regardless of
	// continue-on-error.
	if !job.ContinueOnError {
		t.Errorf("%s: want continue-on-error (bazel-cmd-dolt's own advisory nature, carried into the union)", bazelDoltIntJobName)
	}
	if job.Name != "dolt-int" {
		// ci-analytics' collector maps the check name "Bazel / dolt-int" to
		// lane dolt-int.
		t.Errorf("%s name = %q, want dolt-int", bazelDoltIntJobName, job.Name)
	}
	if !slices.Equal(job.Needs, []string{bazelRBEJobName}) {
		t.Errorf("%s needs %v, want [%s]", bazelDoltIntJobName, job.Needs, bazelRBEJobName)
	}
	if !reflect.DeepEqual(job.Outputs, map[string]string{"result": "${{ steps.result.outputs.result }}"}) {
		t.Errorf("%s outputs = %v, want only result from the result step", bazelDoltIntJobName, job.Outputs)
	}

	// The test step: the union config, as the member lanes run theirs.
	test := job.step(t, "bazel test //... --config="+bazelDoltIntConfig)
	if strings.TrimSpace(test.Run) != bazelTierTestRun(bazelDoltIntConfig) {
		t.Errorf("%s test step run changed; want exactly:\n%s\ngot:\n%s", bazelDoltIntJobName, bazelTierTestRun(bazelDoltIntConfig), test.Run)
	}
	// server-storage's two checks and its own "No test target is marked
	// flaky" step (cmd-dolt has none of either): textually identical, once
	// over the union's BEP.
	for member, steps := range map[string][]string{
		bazelServerJobName: {"No test target is marked flaky", "Every target and shard ran tests", "Every listed test ran in its shard"},
	} {
		m := workflow.job(t, member)
		for _, name := range steps {
			want, got := m.step(t, name), job.step(t, name)
			if strings.TrimSpace(got.Run) != strings.TrimSpace(want.Run) || got.If != want.If {
				t.Errorf("%s step %q = (if %q) %q, want %s's (if %q) %q", bazelDoltIntJobName, name, got.If, got.Run, member, want.If, want.Run)
			}
		}
	}
	// Shard balance is bespoke: server-storage's own step has no --label (it
	// has only ever budgeted its own target), the union's scopes to that same
	// target with --label so cmd-dolt's untested shards do not skew it (see
	// the job's own comment and shard_budget.py's docstring).
	storageShardBalance := workflow.job(t, bazelServerJobName).step(t, "Shard balance")
	unionShardBalance := job.step(t, "Shard balance")
	if strings.TrimSpace(unionShardBalance.Run) == strings.TrimSpace(storageShardBalance.Run) {
		t.Errorf("%s Shard balance step is textually identical to %s's; want it scoped with --label", bazelDoltIntJobName, bazelServerJobName)
	}
	if !strings.Contains(unionShardBalance.Run, "--label //internal/storage/dolt:dolt_server_full_test") {
		t.Errorf("%s Shard balance step = %q, want --label //internal/storage/dolt:dolt_server_full_test", bazelDoltIntJobName, unionShardBalance.Run)
	}
	for _, member := range bazelUnionLanes[bazelDoltIntJobName] {
		for _, step := range workflow.job(t, member).Steps {
			if strings.Contains(step.Run, "shard_budget.py") && member != bazelServerJobName {
				t.Errorf("%s step %q: a per-lane check the union does not run", member, step.Name)
			}
			if strings.Contains(step.Run, "check_shard_coverage.py") && member != bazelServerJobName {
				t.Errorf("%s step %q: a per-lane check the union does not run", member, step.Name)
			}
		}
	}

	// No job depends on it, and every member's output reads it first.
	for name, other := range workflow.Jobs {
		if slices.Contains(other.Needs, bazelDoltIntJobName) {
			t.Errorf("job %s needs %s", name, bazelDoltIntJobName)
		}
	}
	call := readBazelWorkflowCall(t)
	for _, member := range bazelUnionLanes[bazelDoltIntJobName] {
		if got, want := call.Outputs[member].Value, bazelLaneCallOutput(member); got != want {
			t.Errorf("workflow_call output %s = %q, want %q", member, got, want)
		}
	}
	for name := range call.Outputs {
		if strings.Contains(name, bazelDoltIntConfig) {
			t.Errorf("workflow_call output %s: the union reports through its members' outputs, not one of its own (the gate ids stay)", name)
		}
	}
}

// TestBazelDoltIntJobTimeouts: the union's test step has a 23-minute timeout,
// every step has one, and the job's exceeds their sum by
// bazelDoltIntJobTimeoutMargin minutes (a hang fails a named step) while
// staying under the merge queue's check timeout.
func TestBazelDoltIntJobTimeouts(t *testing.T) {
	job := readCIWorkflow(t, bazelWorkflowName).job(t, bazelDoltIntJobName)
	test := job.step(t, "bazel test //... --config="+bazelDoltIntConfig)
	if test.TimeoutMinutes != bazelDoltIntTestStepTimeout {
		t.Errorf("%s test step timeout-minutes = %d, want %d", bazelDoltIntJobName, test.TimeoutMinutes, bazelDoltIntTestStepTimeout)
	}
	sum := 0
	for i, step := range job.Steps {
		if step.TimeoutMinutes <= 0 {
			t.Errorf("%s step %d (%q) has no timeout-minutes; a hang would run into the job timeout",
				bazelDoltIntJobName, i, cmp.Or(step.Name, step.Uses))
		}
		sum += step.TimeoutMinutes
	}
	if job.TimeoutMinutes < sum+bazelDoltIntJobTimeoutMargin {
		t.Errorf("%s timeout-minutes %d, want at least the sum of its step timeouts (%d) + %d",
			bazelDoltIntJobName, job.TimeoutMinutes, sum, bazelDoltIntJobTimeoutMargin)
	}
	if job.TimeoutMinutes >= bazelDoltRaceMergeQueueTimeout {
		t.Errorf("%s timeout-minutes %d, want below the merge queue's %d-minute check timeout",
			bazelDoltIntJobName, job.TimeoutMinutes, bazelDoltRaceMergeQueueTimeout)
	}
}

// TestBazelDoltIntArmsExactlyOnePerMode evaluates the union's and the
// members' if: for every execution mode and every event bazel.yml runs on:
// the union runs exactly in mode remote, never beside a member, and each
// member's gate id/flag is reported by exactly one job wherever that member
// ran before the union (bazel-server-storage: the remote-executing modes
// plus cache, when enabled and integration is not off; bazel-cmd-dolt: where
// bazel-integration runs) and by none elsewhere, so bazel-gate.sh's per-mode
// skips are unchanged. The workflow_call outputs, evaluated as GitHub does,
// carry the running arm's result: a failed or cancelled union reds both
// ids/flags, and a union that never reported reads as skipped, which no mode
// it runs in allows.
func TestBazelDoltIntArmsExactlyOnePerMode(t *testing.T) {
	workflow := readCIWorkflow(t, bazelWorkflowName)
	union := workflow.job(t, bazelDoltIntJobName)
	members := map[string]struct {
		ifExpr string
		before func(mode string) bool // where the member ran before the union
	}{
		bazelServerJobName:  {bazelServerStorageIf, func(m string) bool { return bazelRemoteModes[m] }},
		bazelCmdDoltJobName: {bazelCmdDoltMemberIf, func(m string) bool { return bazelRemoteModes[m] || m == "cache" }},
	}
	if got := slices.Sorted(maps.Keys(members)); !slices.Equal(got, slices.Sorted(slices.Values(bazelUnionLanes[bazelDoltIntJobName]))) {
		t.Fatalf("members %v, want bazelUnionLanes' %v", got, bazelUnionLanes[bazelDoltIntJobName])
	}
	for name, m := range members {
		if got := workflow.job(t, name).If; got != m.ifExpr {
			t.Errorf("%s if = %q, want %q", name, got, m.ifExpr)
		}
	}
	for _, mode := range bazelRBEModes {
		for _, event := range []string{"pull_request", "merge_group", "pull_request_target", "push", "schedule", "workflow_dispatch"} {
			ctx := map[string]string{
				"needs.rbe.outputs.mode":    mode,
				"needs.rbe.outputs.enabled": bazelModeEnabled(mode),
				"github.event_name":         event,
				"inputs.integration":        "",
			}
			unionRuns := evalRRCIf(t, union.If, ctx)
			if unionRuns != (mode == "remote") {
				t.Errorf("mode %s, %s: %s runs = %v, want %v", mode, event, bazelDoltIntJobName, unionRuns, mode == "remote")
			}
			for name, m := range members {
				runs := evalRRCIf(t, workflow.job(t, name).If, ctx)
				if unionRuns && runs {
					t.Errorf("mode %s, %s: %s and %s both run", mode, event, bazelDoltIntJobName, name)
				}
				if got, want := unionRuns || runs, m.before(mode); got != want {
					t.Errorf("mode %s, %s: %s's tier reported by a job = %v, want %v (where it ran before the union)", mode, event, name, got, want)
				}
				for _, res := range []string{"success", "failure", "cancelled", ""} {
					jobs := map[string]string{}
					switch {
					case unionRuns:
						jobs[bazelDoltIntJobName] = res
					case runs:
						jobs[name] = res
					}
					want := res
					if !unionRuns && !runs {
						want = ""
					}
					if got := bazelCallOutputValue(t, name, jobs); got != want {
						t.Errorf("mode %s, %s, reporting job %q: output %s = %q, want %q", mode, event, res, name, got, want)
					}
				}
			}
		}
	}
	// The union's runner is the lanes' ternary: Blacksmith in mode remote,
	// the only mode it runs in.
	if union.RunsOn != bazelLaneRunsOn {
		t.Errorf("%s runs-on = %q, want %q", bazelDoltIntJobName, union.RunsOn, bazelLaneRunsOn)
	}
}

// bazelDoltIntTestActions: the union's test actions, 17 server-storage + 46
// cmd-dolt (aquery, item 5 A2).
const bazelDoltIntTestActions = 63

// TestBazelDoltIntFitsTheJobsBudget: the union runs both tiers' shards in one
// invocation under --config=remote-exec's --jobs, so all of them must fit at
// once or the slower tier's shards queue behind the other's. Every rule
// tagged with one of the union's tags, read from the BUILD files: its
// shard_count (1 when unsharded), summed.
func TestBazelDoltIntFitsTheJobsBudget(t *testing.T) {
	root := bazelPolicyRoot(t)
	rc := readPolicyFile(t, root, ".bazelrc")
	m := regexp.MustCompile(`(?m)^build:remote-exec --jobs=(\d+)$`).FindStringSubmatch(rc)
	if m == nil {
		t.Fatal(".bazelrc has no build:remote-exec --jobs=N line")
	}
	budget, _ := strconv.Atoi(m[1])

	var tags []string
	for _, mem := range bazelDoltIntMembers {
		tags = append(tags, `"`+mem.tag+`"`)
	}
	ruleStart := regexp.MustCompile(`(?m)^[A-Za-z_][A-Za-z0-9_]*\($`)
	total, targets := 0, 0
	for _, f := range repoFiles(t, root, 300) {
		if path.Base(f) != "BUILD.bazel" {
			continue
		}
		build := readPolicyFile(t, root, f)
		for _, line := range strings.Split(build, "\n") {
			for _, tag := range tags {
				if strings.Contains(line, tag) && !strings.HasPrefix(line, "    tags = [") {
					t.Errorf("%s: %q outside a one-line tags list: %q", f, tag, line)
				}
			}
		}
		for _, loc := range ruleStart.FindAllStringIndex(build, -1) {
			end := strings.Index(build[loc[0]:], "\n)\n")
			if end < 0 {
				continue
			}
			rule := build[loc[0] : loc[0]+end]
			tagged := false
			for _, line := range strings.Split(rule, "\n") {
				if strings.HasPrefix(line, "    tags = [") && slices.ContainsFunc(tags, func(tag string) bool { return strings.Contains(line, tag) }) {
					tagged = true
				}
			}
			if !tagged {
				continue
			}
			shards := 1
			if sm := shardCountPattern.FindStringSubmatch(rule); sm != nil {
				shards, _ = strconv.Atoi(sm[1])
			}
			total += shards
			targets++
		}
	}
	if total != bazelDoltIntTestActions {
		t.Errorf("--config=%s: %d test actions over %d targets, want %d (aquery-verified at the cutover): re-check against --jobs=%d and update", bazelDoltIntConfig, total, targets, bazelDoltIntTestActions, budget)
	}
	if total > budget {
		t.Errorf("--config=%s: %d test actions, more than --config=remote-exec's --jobs=%d: the tiers would no longer run at once", bazelDoltIntConfig, total, budget)
	}
}
