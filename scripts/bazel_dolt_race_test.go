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
	"sync"
	"testing"
)

// The dolt-race union lane (item 5 A1b, and Option B for the embedded
// tier): in mode remote, bazel.yml's bazel-dolt-race job runs
// --config=dolt-race, the union of --config=embedded, --config=doltserver and
// --config=doltserver-proxied, in one `bazel test`, in place of the
// bazel-embedded, bazel-doltserver and bazel-proxied jobs; in every other
// mode those three run as before. Its job.status is every member's
// workflow_call output, so pr.yml's gate ids (BAZEL_EMBEDDED,
// BAZEL_DOLTSERVER, BAZEL_PROXIED), ci-gate.sh and bazel-gate.sh are
// unchanged, and every tier stays hard-gating on every event (unlike
// bazel-dolt-int's per-tier classify, whose single gate tier would leave the
// others non-gating on push, nightly and dispatch, where nothing reads the
// call outputs). Outside mode remote one client would do all three tiers'
// work with no in-flight deduplication, so the union would cost the sum of
// the tiers rather than the max: the members stay there.

const (
	bazelDoltRaceJobName = "bazel-dolt-race"
	bazelDoltRaceConfig  = "dolt-race"
	// Mode remote, on every event that reaches it (same-repo pull requests,
	// merge groups, push, nightly, dispatch, bazel-farm.yml's authorized
	// call): exactly where the members do not run.
	bazelDoltRaceJobIf = "${{ needs.rbe.outputs.mode == 'remote' }}"
	// The members: their pre-union rules (bazel-test's skip rule; remote
	// execution only), minus mode remote.
	bazelDoltMemberIf    = "${{ needs.rbe.outputs.mode != 'skip' && needs.rbe.outputs.mode != 'remote' }}"
	bazelProxiedMemberIf = "${{ needs.rbe.outputs.enabled == 'true' && needs.rbe.outputs.mode != 'remote' }}"
	// bazel-embedded: remote execution only (fork-ro, fork-rw), as before
	// Option B, minus mode remote.
	bazelEmbeddedMemberIf = "${{ needs.rbe.outputs.enabled == 'true' && needs.rbe.outputs.mode != 'remote' }}"
)

// bazelUnionLanes: a union job -> the gated member jobs it runs in place of
// in mode remote (bazelDoltRaceJobIf). Each member keeps its gate id
// (bazelLaneGateIDs) and its workflow_call output, which reads the union's
// result first and the member's own otherwise (bazelLaneCallOutput).
var bazelUnionLanes = map[string][]string{
	bazelDoltRaceJobName: {bazelEmbedJobName, bazelDoltJobName, bazelProxiedJobName},
}

// bazelUnionHasFlagGatedMember reports whether a union job runs in place of
// at least one flag-gated, advisory member (bazel-dolt-int's bazel-cmd-dolt,
// item 5 A2): such a union must carry that member's own continue-on-error, so
// a failure confined to that member's tier stays out of the call's aggregate
// result (BAZEL) in the union's mode exactly as it already is through the
// member outside it. The gated member's id still reads the union's result
// directly (bazelLaneReporter), unaffected either way.
func bazelUnionHasFlagGatedMember(job string) bool {
	for _, m := range bazelUnionLanes[job] {
		if _, ok := bazelFlagGatedLanes[m]; ok {
			return true
		}
	}
	return false
}

// bazelUnionOf: the union job a gated member belongs to, or "".
func bazelUnionOf(member string) string {
	for union, members := range bazelUnionLanes {
		if slices.Contains(members, member) {
			return union
		}
	}
	return ""
}

// bazelUnionMemberOutputKey: the union job's own output key that carries a
// member's tier result, for unions that do not report "result" uniformly
// (item 5 A2's bazel-dolt-int: must-fix 2's per-tier outputs, since a
// job-level continue-on-error would hide a server-storage failure from
// anything that is not itself gated on that one tier). Members not listed
// here read their union's plain "result" output, as dolt-race's do.
var bazelUnionMemberOutputKey = map[string]string{
	bazelServerJobName:  "server_storage_result",
	bazelCmdDoltJobName: "cmd_dolt_result",
}

// bazelLaneCallOutput: the workflow_call output value bazel.yml must declare
// for a lane that reports its job.status.
func bazelLaneCallOutput(lane string) string {
	if union := bazelUnionOf(lane); union != "" {
		key := cmp.Or(bazelUnionMemberOutputKey[lane], "result")
		return "${{ jobs." + union + ".outputs." + key + " || jobs." + lane + ".outputs.result }}"
	}
	return "${{ jobs." + lane + ".outputs.result }}"
}

// bazelLaneReporter: the job whose result a gated lane's output carries in
// this mode (lanes: job -> modes it runs in), or "" when neither the lane
// nor its union runs (an expected skip).
func bazelLaneReporter(lanes map[string]map[string]bool, lane, mode string) string {
	if lanes[lane][mode] {
		return lane
	}
	if union := bazelUnionOf(lane); union != "" && lanes[union][mode] {
		return union
	}
	return ""
}

var (
	bazelCallOutputsMu    sync.Mutex
	bazelCallOutputsCache map[string]string
)

// bazelCallOutputValue evaluates bazel.yml's workflow_call output name with
// jobs (job -> its result output; missing or "" = not reported), the way
// GitHub does: what pr.yml's needs.bazel.outputs.<name> reads.
func bazelCallOutputValue(t *testing.T, name string, jobs map[string]string) string {
	t.Helper()
	bazelCallOutputsMu.Lock()
	if bazelCallOutputsCache == nil {
		bazelCallOutputsCache = map[string]string{}
		for n, out := range readBazelWorkflowCall(t).Outputs {
			bazelCallOutputsCache[n] = out.Value
		}
	}
	expr, ok := bazelCallOutputsCache[name]
	bazelCallOutputsMu.Unlock()
	if !ok {
		t.Fatalf("%s has no workflow_call output %s", bazelWorkflowName, name)
	}
	ctx := map[string]string{}
	for _, m := range regexp.MustCompile(`jobs\.([A-Za-z0-9_-]+)\.outputs\.([A-Za-z0-9_-]+)`).FindAllStringSubmatch(expr, -1) {
		// Every job.outputs reference in a lane's own workflow_call output
		// expression carries that job's conclusion for gating purposes,
		// whatever its output key is named (plain "result", or one of a
		// union's per-tier keys: must-fix 2). jobs is keyed by job name only,
		// so every reference to a given job resolves to the same simulated
		// value regardless of which output key reads it.
		ctx[m[0]] = jobs[m[1]]
	}
	v, err := evalGHExpr(expr, ctx)
	if err != nil {
		t.Fatalf("workflow_call output %s = %q: %v", name, expr, err)
	}
	str, _ := v.(string)
	return str
}

// bazelDoltRaceJobTimeoutMargin: the minutes the union job's timeout-minutes
// must exceed the sum of all its step timeouts (runner setup, post-job
// cleanup), so a hang fails a named step, not the job.
const bazelDoltRaceJobTimeoutMargin = 5

// bazelDoltRaceMergeQueueTimeout: the merge queue's check timeout (ruleset
// merge_queue.check_response_timeout_minutes). The union job is required in
// every merge group, so its own timeout stays below it.
const bazelDoltRaceMergeQueueTimeout = 60

// bazelDoltRaceTestStepTimeout: the union's `bazel test` step timeout. Every
// shard is capped at 1200 s; the slowest mode-remote embedded `bazel test`
// measured was 1252 s (a nightly --config=fresh run, 2026-10-07), so the step
// allows 28 minutes: past that run, and about 3 minutes past the cap so Bazel
// writes TIMEOUT logs before GitHub kills the step. Tighten other steps, not
// this one, if the job's sum stops fitting under the merge queue's 60.
const bazelDoltRaceTestStepTimeout = 28

// bazelDoltRaceJobTimeout: the job's timeout-minutes, the step sum + 5 and
// below the merge queue's 60: about a minute of that 60 is left for runner
// pickup and the rbe job, so a union that hangs all the way to its own
// timeout can still drop the group (accepted: that run is red anyway).
const bazelDoltRaceJobTimeout = 59

// The union's members, and the tag filter each owns, in the union filter's
// order.
var bazelDoltRaceMembers = []struct{ config, tag string }{
	{"embedded", "embedded"},
	{"doltserver", "dolt-server"},
	{"doltserver-proxied", "dolt-server-proxied"},
}

// bazelDoltRaceJobs: the union's one line that is no member's
// (TestBazelDoltRaceFitsTheJobsBudget). --jobs is in no action key.
const bazelDoltRaceJobs = 128

var bazelDoltRaceNonMemberLines = []string{"test --jobs=" + strconv.Itoa(bazelDoltRaceJobs)}

// bazelRCConfigLines returns .bazelrc's option lines for --config=name
// (any command: test:name, build:name, ...), as "<command> <option>"
// with the ":name" suffix stripped, in file order.
func bazelRCConfigLines(rc, name string) []string {
	var out []string
	for _, line := range strings.Split(rc, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		head, opt, ok := strings.Cut(line, " ")
		cmd, cfg, hasCfg := strings.Cut(head, ":")
		if !ok || !hasCfg || cfg != name {
			continue
		}
		out = append(out, cmd+" "+strings.TrimSpace(opt))
	}
	return out
}

// TestBazelDoltRaceConfigIsMemberUnion: --config=dolt-race is exactly the
// set union of its members' lines, except that each member's
// --test_tag_filters is replaced by one filter naming every member's tag, plus
// bazelDoltRaceNonMemberLines (--jobs, in no action key). Every other flag
// stays a member's own, so each member target's test action is unchanged:
// `bazel aquery 'mnemonic("TestRunner", ...)'` over each member's targets
// gives byte-identical actions (key, argv, env, outputs) under the member
// config and under the union, 182 + 27 + 35 = 244 (item 5 Option B). Both
// arms run, the mode deciding which, so drift between them would split
// results by mode: a flag added to a member must be added to the union too.
func TestBazelDoltRaceConfigIsMemberUnion(t *testing.T) {
	rc := readPolicyFile(t, bazelPolicyRoot(t), ".bazelrc")
	const tagPrefix = "test --test_tag_filters="

	var want []string
	var tags []string
	for _, m := range bazelDoltRaceMembers {
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

	got := bazelRCConfigLines(rc, bazelDoltRaceConfig)
	var gotTags []string
	var gotRest []string
	for _, l := range got {
		if v, ok := strings.CutPrefix(l, tagPrefix); ok {
			gotTags = append(gotTags, v)
			continue
		}
		if slices.Contains(bazelDoltRaceNonMemberLines, l) {
			continue
		}
		gotRest = append(gotRest, l)
	}
	for _, l := range bazelDoltRaceNonMemberLines {
		if n := slices.Index(got, l); n < 0 || slices.Index(got[n+1:], l) >= 0 {
			t.Errorf("--config=%s: want exactly one %q line", bazelDoltRaceConfig, l)
		}
		if slices.Contains(want, l) {
			t.Errorf("--config=%s: %q is a member's line, not the union's own", bazelDoltRaceConfig, l)
		}
	}
	if wantTag := strings.Join(tags, ","); len(gotTags) != 1 || gotTags[0] != wantTag {
		t.Errorf("--config=%s tag filters %v, want exactly [%s]", bazelDoltRaceConfig, gotTags, wantTag)
	}
	if !sameStringSet(gotRest, want) {
		t.Errorf("--config=%s is not the union of its members' lines (tag filter aside):\ngot:\n%s\nwant (any order):\n%s",
			bazelDoltRaceConfig, strings.Join(gotRest, "\n"), strings.Join(want, "\n"))
	}
	if len(gotRest) != len(want) {
		t.Errorf("--config=%s repeats a line: %d lines for %d distinct member lines", bazelDoltRaceConfig, len(gotRest), len(want))
	}
	// No nested --config: the union is spelled out, so the policy above
	// sees every flag.
	for _, l := range got {
		if strings.Contains(l, "--config=") {
			t.Errorf("--config=%s line %q: spell the members' flags out, no nested --config", bazelDoltRaceConfig, l)
		}
	}
}

// bazelShardCoverageArgs splits a check_shard_coverage.py step's run into
// everything before its first --suite/--whole and the --suite/--whole
// arguments, one per element ("--suite <target> <script> <spec>",
// "--whole <target>").
func bazelShardCoverageArgs(t *testing.T, run string) (string, []string) {
	t.Helper()
	fields := strings.Fields(run)
	var head []string
	var args []string
	for i := 0; i < len(fields); {
		switch fields[i] {
		case "--suite":
			if i+3 >= len(fields) {
				t.Fatalf("check_shard_coverage run %q: --suite needs <target> <script> <spec>", run)
			}
			args = append(args, strings.Join(fields[i:i+4], " "))
			i += 4
		case "--whole":
			if i+1 >= len(fields) {
				t.Fatalf("check_shard_coverage run %q: --whole needs <target>", run)
			}
			args = append(args, strings.Join(fields[i:i+2], " "))
			i += 2
		default:
			if len(args) > 0 {
				t.Fatalf("check_shard_coverage run %q: %q after the suites", run, fields[i])
			}
			head = append(head, fields[i])
			i++
		}
	}
	return strings.Join(head, " "), args
}

func sameStringSet(a, b []string) bool {
	as := map[string]bool{}
	for _, s := range a {
		as[s] = true
	}
	bs := map[string]bool{}
	for _, s := range b {
		bs[s] = true
	}
	if len(as) != len(bs) {
		return false
	}
	for s := range as {
		if !bs[s] {
			return false
		}
	}
	return true
}

// TestBazelDoltRaceSoleRunIsInTheUnion: where bazel-doltserver executes
// remotely (the fork modes) it adds --config=sole-run; the union carries that
// config's lines itself, so the dolt-server tier's actions are the same in
// either arm. Every sole-run line is a dolt-race line.
func TestBazelDoltRaceSoleRunIsInTheUnion(t *testing.T) {
	rc := readPolicyFile(t, bazelPolicyRoot(t), ".bazelrc")
	sole := bazelRCConfigLines(rc, "sole-run")
	if len(sole) == 0 {
		t.Fatal(".bazelrc has no --config=sole-run lines")
	}
	union := bazelRCConfigLines(rc, bazelDoltRaceConfig)
	for _, l := range sole {
		if !slices.Contains(union, l) {
			t.Errorf("--config=sole-run line %q is not in --config=%s: bazel-doltserver's remote runs and the union would differ", l, bazelDoltRaceConfig)
		}
	}
	// And the union job takes no sole-run of its own: nothing to add.
	test := readCIWorkflow(t, bazelWorkflowName).job(t, bazelDoltRaceJobName).step(t, "bazel test //... --config="+bazelDoltRaceConfig)
	if strings.Contains(test.Run, "BAZEL_SOLE_RUN") || strings.Contains(test.Run, "sole-run") {
		t.Errorf("%s test step adds sole-run; --config=%s already has its lines", bazelDoltRaceJobName, bazelDoltRaceConfig)
	}
}

// TestBazelDoltRaceIsGated pins the cutover: the union job is a required
// lane like its members (no continue-on-error, the always() job.status
// recorder), it runs the union config with the members' checks once over its
// BEP, and both members' workflow_call outputs read its result first.
func TestBazelDoltRaceIsGated(t *testing.T) {
	workflow := readCIWorkflow(t, bazelWorkflowName)
	job := workflow.job(t, bazelDoltRaceJobName)

	if job.If != bazelDoltRaceJobIf {
		t.Errorf("%s if: %q, want %q", bazelDoltRaceJobName, job.If, bazelDoltRaceJobIf)
	}
	if job.ContinueOnError {
		t.Errorf("%s: continue-on-error hides the dolt-server and proxied tiers' failures from pr.yml's ci-gate", bazelDoltRaceJobName)
	}
	if job.Name != "dolt-race" {
		// ci-analytics' collector maps the check name "Bazel / dolt-race"
		// to lane dolt-race.
		t.Errorf("%s name = %q, want dolt-race", bazelDoltRaceJobName, job.Name)
	}
	if !slices.Equal(job.Needs, []string{bazelRBEJobName}) {
		t.Errorf("%s needs %v, want [%s]", bazelDoltRaceJobName, job.Needs, bazelRBEJobName)
	}
	if !reflect.DeepEqual(job.Outputs, map[string]string{"result": "${{ steps.result.outputs.result }}"}) {
		t.Errorf("%s outputs = %v, want only result from the result step", bazelDoltRaceJobName, job.Outputs)
	}

	// The test step: the union config, as the member lanes run theirs.
	test := job.step(t, "bazel test //... --config="+bazelDoltRaceConfig)
	if strings.TrimSpace(test.Run) != bazelTierTestRun(bazelDoltRaceConfig) {
		t.Errorf("%s test step run changed; want exactly:\n%s\ngot:\n%s", bazelDoltRaceJobName, bazelTierTestRun(bazelDoltRaceConfig), test.Run)
	}
	// Every step of every member has its counterpart in the union, derived
	// from the member jobs rather than a fixed list, so a check step added to
	// a member cannot silently stop running in mode remote: same name (the
	// unnamed checkout by its uses), same if and uses, and the same run,
	// except the member's own `bazel test` step (the union's, above) and
	// check_shard_coverage (the union of the members' arguments, below).
	// Artifact names and step timeouts may differ (with: and timeout-minutes
	// are not compared; the served-timings upload is pinned below). Where the
	// members' if differs for one step (bazel-doltserver uploads its logs on
	// a failed test step only, the others on any failure), the union's is one
	// of theirs.
	memberIfs := map[string][]string{}
	for _, member := range bazelUnionLanes[bazelDoltRaceJobName] {
		for _, step := range workflow.job(t, member).Steps {
			label := cmp.Or(step.Name, step.Uses)
			memberIfs[label] = append(memberIfs[label], step.If)
		}
	}
	for _, member := range bazelUnionLanes[bazelDoltRaceJobName] {
		for _, want := range workflow.job(t, member).Steps {
			if strings.HasPrefix(want.Name, "bazel test //... --config=") {
				continue
			}
			var got *ciWorkflowStep
			for i, step := range job.Steps {
				if (want.Name != "" && step.Name == want.Name) || (want.Name == "" && step.Name == "" && step.Uses == want.Uses) {
					got = &job.Steps[i]
					break
				}
			}
			label := cmp.Or(want.Name, want.Uses)
			if got == nil {
				t.Errorf("%s step %q has no counterpart in %s: it would not run in mode remote", member, label, bazelDoltRaceJobName)
				continue
			}
			if !slices.Contains(memberIfs[label], got.If) || got.Uses != want.Uses {
				t.Errorf("%s step %q = (if %q, uses %q), want %s's uses %q and one of the members' ifs %q", bazelDoltRaceJobName, label, got.If, got.Uses, member, want.Uses, memberIfs[label])
			}
			if strings.Contains(want.Run, "check_shard_coverage.py") {
				continue
			}
			if strings.TrimSpace(got.Run) != strings.TrimSpace(want.Run) {
				t.Errorf("%s step %q runs %q, want %s's %q", bazelDoltRaceJobName, label, got.Run, member, want.Run)
			}
		}
	}
	// check_shard_coverage takes the union of the members' --suite/--whole
	// arguments, as each member runs its own (same if, same --bep), so every
	// shard tiling a member checks is still checked in mode remote.
	const coverage = "Every listed test ran in its shard"
	gotCov := job.step(t, coverage)
	gotHead, gotArgs := bazelShardCoverageArgs(t, gotCov.Run)
	var wantArgs []string
	for _, member := range bazelUnionLanes[bazelDoltRaceJobName] {
		m := workflow.job(t, member)
		for _, step := range m.Steps {
			if strings.Contains(step.Run, "shard_budget.py") {
				t.Errorf("%s step %q: a per-lane check the union does not run", member, step.Name)
			}
			if !strings.Contains(step.Run, "check_shard_coverage.py") {
				continue
			}
			if step.Name != coverage || step.If != gotCov.If {
				t.Errorf("%s step %q (if %q): want the union's %q (if %q)", member, step.Name, step.If, coverage, gotCov.If)
			}
			head, args := bazelShardCoverageArgs(t, step.Run)
			if head != gotHead {
				t.Errorf("%s %q runs %q before its suites, the union %q", member, coverage, head, gotHead)
			}
			wantArgs = append(wantArgs, args...)
		}
	}
	if !sameStringSet(gotArgs, wantArgs) || len(gotArgs) != len(wantArgs) {
		t.Errorf("%s %q suites:\n%s\nwant exactly the members' (any order):\n%s", bazelDoltRaceJobName, coverage, strings.Join(gotArgs, "\n"), strings.Join(wantArgs, "\n"))
	}
	// bazel-embedded's served-timings upload (telemetry), under the union's
	// own artifact name (TestBazelEmbeddedServedTimingsUploadIsTelemetry).
	wantServed, gotServed := workflow.job(t, bazelEmbedJobName).step(t, bazelServedTimingsStepName), job.step(t, bazelServedTimingsStepName)
	wantServed.With = maps.Clone(wantServed.With)
	wantServed.With["name"] = bazelServedTimingsArtifact[bazelDoltRaceJobName]
	if !reflect.DeepEqual(gotServed, wantServed) {
		t.Errorf("%s step %q = %+v, want %s's under its own artifact name: %+v", bazelDoltRaceJobName, bazelServedTimingsStepName, gotServed, bazelEmbedJobName, wantServed)
	}

	// No job depends on it, and every member's output reads it first.
	for name, other := range workflow.Jobs {
		if slices.Contains(other.Needs, bazelDoltRaceJobName) {
			t.Errorf("job %s needs %s", name, bazelDoltRaceJobName)
		}
	}
	call := readBazelWorkflowCall(t)
	for _, member := range bazelUnionLanes[bazelDoltRaceJobName] {
		if got, want := call.Outputs[member].Value, bazelLaneCallOutput(member); got != want {
			t.Errorf("workflow_call output %s = %q, want %q", member, got, want)
		}
	}
	for name := range call.Outputs {
		if strings.Contains(name, bazelDoltRaceConfig) {
			t.Errorf("workflow_call output %s: the union reports through its members' outputs, not one of its own (the gate ids stay)", name)
		}
	}
}

// TestBazelDoltRaceJobTimeouts: the union's test step has a 28-minute
// timeout, every step has one, and the job's (59) exceeds their sum by
// bazelDoltRaceJobTimeoutMargin minutes (a hang fails a named step) while
// staying under the merge queue's check timeout (a hung required job would
// otherwise drop the group).
func TestBazelDoltRaceJobTimeouts(t *testing.T) {
	job := readCIWorkflow(t, bazelWorkflowName).job(t, bazelDoltRaceJobName)
	test := job.step(t, "bazel test //... --config="+bazelDoltRaceConfig)
	if test.TimeoutMinutes != bazelDoltRaceTestStepTimeout {
		t.Errorf("%s test step timeout-minutes = %d, want %d", bazelDoltRaceJobName, test.TimeoutMinutes, bazelDoltRaceTestStepTimeout)
	}
	sum := 0
	for i, step := range job.Steps {
		if step.TimeoutMinutes <= 0 {
			t.Errorf("%s step %d (%q) has no timeout-minutes; a hang would run into the job timeout",
				bazelDoltRaceJobName, i, cmp.Or(step.Name, step.Uses))
		}
		sum += step.TimeoutMinutes
	}
	if job.TimeoutMinutes < sum+bazelDoltRaceJobTimeoutMargin {
		t.Errorf("%s timeout-minutes %d, want at least the sum of its step timeouts (%d) + %d",
			bazelDoltRaceJobName, job.TimeoutMinutes, sum, bazelDoltRaceJobTimeoutMargin)
	}
	if job.TimeoutMinutes != bazelDoltRaceJobTimeout {
		t.Errorf("%s timeout-minutes %d, want %d", bazelDoltRaceJobName, job.TimeoutMinutes, bazelDoltRaceJobTimeout)
	}
	if job.TimeoutMinutes >= bazelDoltRaceMergeQueueTimeout {
		t.Errorf("%s timeout-minutes %d, want below the merge queue's %d-minute check timeout",
			bazelDoltRaceJobName, job.TimeoutMinutes, bazelDoltRaceMergeQueueTimeout)
	}
}

// TestBazelDoltRaceArmsExactlyOnePerMode evaluates the union's and the
// members' if: for every execution mode and every event bazel.yml runs on:
// the union runs exactly in mode remote, never beside a member, and each
// member's gate id is reported by exactly one job wherever that member ran
// before the union (bazel-doltserver: every mode but skip; bazel-proxied:
// and bazel-embedded: the remote-executing modes) and by none elsewhere, so bazel-gate.sh's
// per-mode skips are unchanged. The workflow_call outputs, evaluated as
// GitHub does, carry the running arm's result: a failed or cancelled union
// reds both ids, and a union that never reported reads as skipped, which no
// mode it runs in allows.
func TestBazelDoltRaceArmsExactlyOnePerMode(t *testing.T) {
	workflow := readCIWorkflow(t, bazelWorkflowName)
	union := workflow.job(t, bazelDoltRaceJobName)
	members := map[string]struct {
		ifExpr string
		before func(mode string) bool // where the member ran before the union
	}{
		bazelDoltJobName:    {bazelDoltMemberIf, func(m string) bool { return m != "skip" }},
		bazelProxiedJobName: {bazelProxiedMemberIf, func(m string) bool { return bazelRemoteModes[m] }},
		// Option B: remote execution only before the union, as proxied.
		bazelEmbedJobName: {bazelEmbeddedMemberIf, func(m string) bool { return bazelRemoteModes[m] }},
	}
	if got := slices.Sorted(maps.Keys(members)); !slices.Equal(got, slices.Sorted(slices.Values(bazelUnionLanes[bazelDoltRaceJobName]))) {
		t.Fatalf("members %v, want bazelUnionLanes' %v", got, bazelUnionLanes[bazelDoltRaceJobName])
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
			}
			unionRuns := evalRRCIf(t, union.If, ctx)
			if unionRuns != (mode == "remote") {
				t.Errorf("mode %s, %s: %s runs = %v, want %v", mode, event, bazelDoltRaceJobName, unionRuns, mode == "remote")
			}
			for name, m := range members {
				runs := evalRRCIf(t, workflow.job(t, name).If, ctx)
				if unionRuns && runs {
					t.Errorf("mode %s, %s: %s and %s both run", mode, event, bazelDoltRaceJobName, name)
				}
				if got, want := unionRuns || runs, m.before(mode); got != want {
					t.Errorf("mode %s, %s: %s's tier reported by a job = %v, want %v (where it ran before the union)", mode, event, name, got, want)
				}
				for _, res := range []string{"success", "failure", "cancelled", ""} {
					jobs := map[string]string{}
					switch {
					case unionRuns:
						jobs[bazelDoltRaceJobName] = res
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
		t.Errorf("%s runs-on = %q, want %q", bazelDoltRaceJobName, union.RunsOn, bazelLaneRunsOn)
	}
}

// bazelDoltRaceWaves: the union's test actions may fill its --jobs at most
// this many times over. At the cutover they were 244 (aquery of
// mnemonic("TestRunner") under --config=dolt-race: 182 embedded + 27
// dolt-server + 35 proxied-server), two waves of 128, which is what the
// wall-clock model and the 28-minute test step assume; a third wave would
// put the last shards' 1200 s cap past the step timeout.
const bazelDoltRaceWaves = 2

// bazelDoltRaceTestActionsFloor: a sanity floor on the counted actions, so a
// counter that stops seeing a tier (a tag spelled another way, a rule shape
// the parser misses) fails instead of passing under the bound: the shards
// the union's check_shard_coverage step alone names (the embedded cmd block's
// 100, embedded storage's 40, the three conformance --whole targets and
// proxied's 34). Every tier must also count at least one action.
const bazelDoltRaceTestActionsFloor = 100 + 40 + 3 + 34

// TestBazelDoltRaceFitsTheJobsBudget: the union runs all three tiers' shards
// in one invocation under its own --jobs (bazelDoltRaceNonMemberLines), which
// overrides --config=remote-exec's for this config only. The pin is the
// farm's concurrency, not the action count (244 > 128: the embedded tier
// alone, 182, never fit --config=remote-exec's 64): at most what the two
// lanes it replaces in mode remote held together, bazel-embedded's and the
// dolt-server + proxied union's --config=remote-exec --jobs each, so Option B
// adds no peak concurrency on rbe-west. --jobs=128 is set on test:dolt-race
// and nowhere else, and on no member config (whose lanes keep remote-exec's).
// The counted test actions are every rule tagged with one of the union's
// tags, read from the BUILD files: its shard_count (1 when unsharded),
// summed; a tag outside a one-line `tags = [...]` list cannot be counted and
// fails the test. The count is bounded, not pinned, so a reshard needs no
// bump here: at most bazelDoltRaceWaves x --jobs, at least the floor.
func TestBazelDoltRaceFitsTheJobsBudget(t *testing.T) {
	root := bazelPolicyRoot(t)
	rc := readPolicyFile(t, root, ".bazelrc")
	m := regexp.MustCompile(`(?m)^build:remote-exec --jobs=(\d+)$`).FindStringSubmatch(rc)
	if m == nil {
		t.Fatal(".bazelrc has no build:remote-exec --jobs=N line")
	}
	budget, _ := strconv.Atoi(m[1])
	if limit := 2 * budget; bazelDoltRaceJobs > limit {
		t.Errorf("--config=%s --jobs=%d, more than the %d (2 x --config=remote-exec's --jobs=%d) the two lanes it replaces held together", bazelDoltRaceConfig, bazelDoltRaceJobs, limit, budget)
	}
	jobsLines := regexp.MustCompile(`(?m)^[^#\n]*--jobs=`+strconv.Itoa(bazelDoltRaceJobs)+`\b.*$`).FindAllString(rc, -1)
	if want := []string{"test:" + bazelDoltRaceConfig + " --jobs=" + strconv.Itoa(bazelDoltRaceJobs)}; !slices.Equal(jobsLines, want) {
		t.Errorf(".bazelrc --jobs=%d lines: %q, want exactly %q", bazelDoltRaceJobs, jobsLines, want)
	}
	if n := strings.Count(strings.Join(bazelRCConfigLines(rc, bazelDoltRaceConfig), "\n"), "--jobs="); n != 1 {
		t.Errorf("--config=%s has %d --jobs lines, want 1", bazelDoltRaceConfig, n)
	}
	for _, mem := range bazelDoltRaceMembers {
		for _, l := range bazelRCConfigLines(rc, mem.config) {
			if strings.Contains(l, "--jobs") {
				t.Errorf("--config=%s line %q: the member lanes keep --config=remote-exec's --jobs", mem.config, l)
			}
		}
	}

	var tags []string
	for _, mem := range bazelDoltRaceMembers {
		tags = append(tags, `"`+mem.tag+`"`)
	}
	ruleStart := regexp.MustCompile(`(?m)^[A-Za-z_][A-Za-z0-9_]*\($`)
	total, targets := 0, 0
	perTag := map[string]int{}
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
			var ruleTags []string
			for _, line := range strings.Split(rule, "\n") {
				if !strings.HasPrefix(line, "    tags = [") {
					continue
				}
				for _, tag := range tags {
					if strings.Contains(line, tag) {
						ruleTags = append(ruleTags, tag)
					}
				}
			}
			if len(ruleTags) == 0 {
				continue
			}
			shards := 1
			if sm := shardCountPattern.FindStringSubmatch(rule); sm != nil {
				shards, _ = strconv.Atoi(sm[1])
			}
			total += shards
			targets++
			for _, tag := range ruleTags {
				perTag[tag] += shards
			}
		}
	}
	if limit := bazelDoltRaceWaves * bazelDoltRaceJobs; total > limit {
		t.Errorf("--config=%s: %d test actions over %d targets, more than %d waves of its --jobs=%d (%d): the last shards' 1200 s cap would run past the 28-minute test step; re-check the wall-clock model, the step timeout and --jobs with the farm owner", bazelDoltRaceConfig, total, targets, bazelDoltRaceWaves, bazelDoltRaceJobs, limit)
	}
	if total < bazelDoltRaceTestActionsFloor {
		t.Errorf("--config=%s: %d test actions over %d targets, below the %d shards its own coverage check names: the counter is missing a tier", bazelDoltRaceConfig, total, targets, bazelDoltRaceTestActionsFloor)
	}
	for _, tag := range tags {
		if perTag[tag] == 0 {
			t.Errorf("--config=%s: no test action tagged %s counted", bazelDoltRaceConfig, tag)
		}
	}
}
