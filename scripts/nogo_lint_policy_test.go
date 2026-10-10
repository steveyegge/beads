package scripts_test

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Lint and vet are nogo (//tools/nogo): go test's vet checks plus the
// golangci-lint linters .golangci.yml enables, validated beside every Go
// compile. bazel.yml's required lanes gate them: natively inside
// `bazel test //... --config=ci` (test lane), and for the files only other
// platforms compile inside the pure lane's release cross-compile
// (//tools/bazel:release_cross, every release platform). These pin that
// wiring and that golangci-lint is gone from CI, hooks and make.
//
// nogo validation is duplicated across lanes that compile the same Bazel
// configuration (race: test, embedded, doltserver, doltserver-proxied,
// dolt-race and the package gates). TestLintAndVetRunAsNogo used to forbid
// `run_validations` anywhere in .bazelrc ("every lane validates"); that
// blanket ban is replaced by TestNogoConfigurationsHaveOneValidatingLane
// (T1), TestNogoOwnersAreRequired (T2), TestNogoRaceOwnerBuildsEverything
// (T3), TestNoUnconditionalValidationOff (T5) and
// TestRunValidationsOnlyOnAllowlistedLines (T6), which together pin "every
// configuration's nogo runs in exactly one required lane" instead;
// TestNogoPolicyRejectsMutations pins that T1, T3, T5 and T6 reject known-bad
// .bazelrc/bazel.yml edits.
// --norun_validations is a build-request option, not a configuration flag:
// it changes no action key and discards no analysis.

// nogoBazelrcLines are .bazelrc's nogo configs, exactly.
var nogoBazelrcLines = []string{
	"build:nogo --@rules_go//go/config:race",
	"build:nogo --keep_going",
	"build:nogo --output_groups=nogo_fix",
	"build:nogo-cross --keep_going",
	"build:nogo-cross --output_groups=nogo_fix",
}

func TestLintAndVetRunAsNogo(t *testing.T) {
	root := sourceRepoRoot(t)

	rc := readPolicyFile(t, root, ".bazelrc")
	var got []string
	for _, line := range strings.Split(rc, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "build:nogo") {
			got = append(got, line)
		}
	}
	if !equalStrings(got, nogoBazelrcLines) {
		t.Errorf(".bazelrc nogo configs = %q, want %q", got, nogoBazelrcLines)
	}

	// No workflow installs or runs golangci-lint.
	entries, err := os.ReadDir(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		for name, j := range readCIWorkflow(t, e.Name()).Jobs {
			for _, st := range j.Steps {
				if strings.Contains(st.Run, "golangci") || strings.Contains(st.Uses, "golangci") {
					t.Errorf("%s %s step %q runs golangci-lint; nogo replaces it", e.Name(), name, st.Name)
				}
			}
		}
	}
	for _, gone := range []string{"scripts/ci/install-golangci-lint.sh", "scripts/ci/go-test-vet.sh"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(gone))); err == nil {
			t.Errorf("%s is back; nogo replaces it", gone)
		}
	}

	// Local entrypoints: make, the pre-commit hook and pre-commit's config.
	makefile := readPolicyFile(t, root, "Makefile")
	for _, want := range []string{
		"ci-pr-lint:\n\t@./scripts/ci/pr-lint.sh\n",
		"lint: ci-pr-lint\n",
		"vet: lint\n",
		"\t$(BAZEL) build --config=nogo -- $$packages\n",
	} {
		if !strings.Contains(makefile, want) {
			t.Errorf("Makefile lacks %q", want)
		}
	}
	for _, hook := range []string{".githooks/pre-commit", ".pre-commit-config.yaml"} {
		body := readPolicyFile(t, root, hook)
		if strings.Contains(body, "golangci-lint run") || strings.Contains(body, "golangci-lint@") || strings.Contains(body, "golangci/golangci-lint") {
			t.Errorf("%s still runs golangci-lint", hook)
		}
		if !strings.Contains(body, "make lint-changed LINT_CHANGED_SCOPE=staged") {
			t.Errorf("%s does not lint with nogo (make lint-changed LINT_CHANGED_SCOPE=staged)", hook)
		}
	}
	// gofmt is //scripts/repochecks:fmt_test's, not the lint wrapper's
	// (ga-96smfk.41).
	if wrapper := readPolicyFile(t, root, "scripts/ci/pr-lint.sh"); regexp.MustCompile(`fmt-check|gofmt-bin`).MatchString(wrapper) {
		t.Error("scripts/ci/pr-lint.sh runs gofmt; //scripts/repochecks:fmt_test gates formatting")
	}
}

// --- F5 S1: one validating lane per nogo configuration -----------------

// nogoPolicyInputs is what T1, T3, T5 and T6 check: .bazelrc's text and
// bazel.yml parsed. TestNogoPolicyRejectsMutations feeds them mutated copies.
type nogoPolicyInputs struct {
	rc       string
	workflow ciWorkflow
}

func readNogoPolicyInputs(t *testing.T) nogoPolicyInputs {
	t.Helper()
	return nogoPolicyInputs{
		rc:       readPolicyFile(t, bazelPolicyRoot(t), ".bazelrc"),
		workflow: readCIWorkflow(t, bazelWorkflowName),
	}
}

// nogoKeyFlags returns the build-affecting flags f5-spec.md §6 T1 calls out
// as fingerprint-relevant among line's whitespace-separated tokens: they
// select a distinct Bazel configuration (and so a distinct set of nogo action
// keys). Flags like --test_tag_filters, --keep_going or --test_arg narrow
// which tests run but do not change what is compiled, so they are not in the
// fingerprint. Each token is parsed whole (flag name, then value), never
// matched by prefix: --@rules_go//go/config:race=false is not the race
// configuration, and must not fingerprint as --@rules_go//go/config:race.
// Boolean flags are normalized to name=true / name=false (a bare
// --@rules_go//go/config:race is race=true; --no@rules_go//go/config:race is
// race=false).
func nogoKeyFlags(line string) []string {
	var out []string
	for _, tok := range strings.Fields(line) {
		name, value, hasValue := strings.Cut(tok, "=")
		switch name {
		case "--@rules_go//go/config:race", "--@rules_go//go/config:pure":
			if !hasValue {
				value = "true"
			}
			out = append(out, name+"="+value)
		case "--no@rules_go//go/config:race", "--no@rules_go//go/config:pure":
			if !hasValue {
				out = append(out, "--"+strings.TrimPrefix(name, "--no")+"=false")
			}
		case "--@rules_go//go/config:tags", "--platforms", "--//tools/bazel:release_platforms":
			out = append(out, tok)
		}
	}
	return out
}

// expandBazelrcConfig returns every "<cmd> <opt>" line (as bazelRCConfigLines
// formats them) that --config=name pulls in, recursively following any
// nested --config=Y reference within those lines (as .bazelrc's own comments
// describe, e.g. test:ci --config=prcore). seen prevents infinite recursion
// on a cycle.
//
// A nested reference is recognized wherever a --config= token appears: on a
// "common:" line (common applies to build and test alike, so common:ci
// --config=prcore pulls prcore in exactly as test:ci --config=prcore would),
// and anywhere among a line's options, not only as the first one (a line
// "build:ci --stripopt=foo --config=prcore" pulls prcore in too). An earlier
// version matched only a whole line beginning "build --config=" or "test
// --config=", so a --config= reference on a common: line, or not in first
// position, silently expanded to nothing (review of #7482, should-fix 1).
func expandBazelrcConfig(rc, name string, seen map[string]bool) []string {
	if seen[name] {
		return nil
	}
	seen[name] = true
	var out []string
	for _, l := range bazelRCConfigLines(rc, name) {
		out = append(out, l)
		for _, tok := range strings.Fields(l) {
			if v, ok := strings.CutPrefix(tok, "--config="); ok {
				out = append(out, expandBazelrcConfig(rc, v, seen)...)
			}
		}
	}
	return out
}

// nogoFingerprint reduces a config's expanded lines to its key-affecting
// flags, sorted and deduplicated, so two configs that build the same thing
// compare equal regardless of line order or non-key-affecting flags.
func nogoFingerprint(lines []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lines {
		for _, m := range nogoKeyFlags(l) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Strings(out)
	return out
}

// bazelrcConfigSkipsValidations reports whether --config=name's own
// (non-recursive) .bazelrc lines pass --norun_validations. Non-recursive
// because each non-owner config sets the flag itself (D1); inheriting it
// through a nested --config would make the policy table here miss a
// config that silently stopped validating by referencing one that did.
func bazelrcConfigSkipsValidations(rc, name string) bool {
	for _, l := range bazelRCConfigLines(rc, name) {
		if strings.Contains(l, "--norun_validations") {
			return true
		}
	}
	return false
}

// bazelrcConfigExpansionSkipsValidations reports whether --config=name, with
// every --config it pulls in followed recursively, passes
// --norun_validations. Owners are checked this way: an owner's validation is
// off if any config in its chain (test:ci -> test:prcore, or a config the
// owner's command line adds, like sole-run) turns it off.
func bazelrcConfigExpansionSkipsValidations(rc, name string) bool {
	for _, l := range expandBazelrcConfig(rc, name, map[string]bool{}) {
		if strings.Contains(l, "--norun_validations") {
			return true
		}
	}
	return false
}

// bazelInvocations returns the arguments (after `bazel <verb>`) of every
// `bazel <verb>` command in a step's run script whose verb is in verbs. A
// command continued over several lines with a trailing backslash is one
// command: its continuation lines' arguments are included. Arguments stop at
// the first shell pipe or list operator.
func bazelInvocations(run string, verbs ...string) [][]string {
	var out [][]string
	for _, line := range strings.Split(strings.ReplaceAll(run, "\\\n", " "), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "bazel" || !slices.Contains(verbs, fields[1]) {
			continue
		}
		var args []string
		for _, f := range fields[2:] {
			if f == "|" || f == "||" || f == "&&" || f == ";" {
				break
			}
			args = append(args, f)
		}
		out = append(out, args)
	}
	return out
}

var (
	shellVarRefPattern   = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)`)
	configInValuePattern = regexp.MustCompile(`--config=([A-Za-z0-9_-]+)`)
)

// jobBazelConfigs returns every --config name job's `bazel test`/`bazel
// build` commands select: spelled out (--config=X or --config X), or through
// a shell variable the step, job or workflow env defines (bazel.yml's
// BAZEL_SOLE_RUN and BAZEL_FRESH), in which case every --config=X the
// variable's expression can produce counts. This is the job's real
// job -> config mapping, read from what it runs, not from table order.
func jobBazelConfigs(workflow ciWorkflow, job ciWorkflowJob) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, st := range job.Steps {
		for _, args := range bazelInvocations(st.Run, "test", "build") {
			for i, a := range args {
				if v, ok := strings.CutPrefix(a, "--config="); ok {
					add(v)
				} else if a == "--config" && i+1 < len(args) {
					add(args[i+1])
				}
				for _, ref := range shellVarRefPattern.FindAllStringSubmatch(a, -1) {
					value, ok := st.Env[ref[1]]
					if !ok {
						value, ok = job.Env[ref[1]]
					}
					if !ok {
						value = workflow.Env[ref[1]]
					}
					for _, m := range configInValuePattern.FindAllStringSubmatch(value, -1) {
						add(m[1])
					}
				}
			}
		}
	}
	return out
}

// nogoConfigurationRow is one row of nogoConfigurations: one Bazel
// configuration (one nogo fingerprint).
type nogoConfigurationRow struct {
	name      string
	configs   []string // .bazelrc --config names sharing this fingerprint, owner first
	owner     string   // bazel.yml job name required to validate
	nonOwners []string // bazel.yml job names required to pass --norun_validations
	// ownerIsNogoOnlyStep: f5-spec.md §2.3/§5 S2 Variant A. True for a row
	// where no full lane validates the configuration at all -- every config
	// in the row, owner's own full lane included, passes --norun_validations
	// -- and the owner job instead runs a narrow `bazel build
	// --output_groups=nogo_fix` step that rebuilds only the configuration's
	// non-test library targets. checkNogoConfigurationsHaveOneValidatingLane
	// branches on this instead of requiring the owner's full lane to
	// validate, which Variant A's owner deliberately does not do.
	ownerIsNogoOnlyStep bool
}

// nogoConfigurations: f5-spec.md §6 T1's table. Each row is one Bazel
// configuration (one nogo fingerprint): the .bazelrc --config names (or, for
// the package gates, a pseudo-config checked against their pinned command
// line) that build it, the bazel.yml job that owns validating it, and the
// jobs that must pass --norun_validations instead.
//
// The race group (F5 S1) and the race+integration group (F5 S2 Variant A:
// integration, doltserver-integration, doltserver-cmd -- same fingerprint,
// race=true plus tags=gms_pure_go,integration) are both classified here.
// Under Variant A no full lane validates the integration group at all (every
// member config passes --norun_validations); its owner is instead the
// bazel-integration job's narrow nogo-only step over the group's non-test
// library package, //internal/testutil/integration (ownerIsNogoOnlyStep;
// f5-spec.md §2.3). `pure`'s optional D4a split is a later slice (f5-spec.md
// §5 S3): it still validates in every lane today, so it is intentionally
// left out of this table rather than added as a row that
// TestNogoConfigurationsHaveOneValidatingLane could never satisfy. `pure`,
// js/wasm and release-cross are unaffected by any slice (single-lane
// configurations: there is no other lane to deduplicate against), so they
// are included as trivial one-member rows for completeness and as a guard
// against a future lane quietly joining them.
var nogoConfigurations = []nogoConfigurationRow{
	{
		name:      "race",
		configs:   []string{"ci", "embedded", "doltserver", "doltserver-proxied", "dolt-race"},
		owner:     bazelJobName,
		nonOwners: []string{bazelEmbedJobName, bazelDoltJobName, bazelProxiedJobName, bazelDoltRaceJobName},
	},
	{
		name:                "integ",
		configs:             []string{"integration", "doltserver-integration", "doltserver-cmd"},
		owner:               bazelIntegJobName,
		nonOwners:           []string{bazelServerJobName, bazelCmdDoltJobName},
		ownerIsNogoOnlyStep: true,
	},
	{
		name:    "pure",
		configs: []string{"pure"},
		owner:   bazelPureJobName,
	},
	{
		name:    "js-wasm",
		configs: []string{"js-wasm"},
		owner:   bazelPureJobName,
	},
}

// nogoPackageGateJobs: the package gates do not select a named .bazelrc
// --config; their `bazel build` command line spells the race flag out
// (pr_lanes_bazel_coverage_test.go pins the exact command). They are race
// non-owners, checked against the "race" row's fingerprint and
// --norun_validations requirement directly from that pinned command.
var nogoPackageGateJobs = []string{bazelPackageMCPJobName, bazelPackageNPMJobName}

// T1 TestNogoConfigurationsHaveOneValidatingLane: every classified Bazel
// configuration has exactly one validating (required) lane; every lane that
// builds the same configuration passes --norun_validations instead; and
// every config within a row shares that row's fingerprint, so a key-affecting
// flag added to a non-owner (which would split it into a different, now
// unvalidated, configuration) fails here instead of silently losing
// coverage.
func TestNogoConfigurationsHaveOneValidatingLane(t *testing.T) {
	checkNogoConfigurationsHaveOneValidatingLane(t, readNogoPolicyInputs(t), t.Errorf)
}

func checkNogoConfigurationsHaveOneValidatingLane(t *testing.T, in nogoPolicyInputs, errorf func(string, ...any)) {
	t.Helper()
	rc, workflow := in.rc, in.workflow

	seenConfig := map[string]string{} // config -> row name, to catch a config in two rows
	for _, row := range nogoConfigurations {
		if row.owner == "" {
			t.Fatalf("nogoConfigurations row %q has no owner", row.name)
		}
		if len(row.configs) == 0 {
			t.Fatalf("nogoConfigurations row %q has no configs", row.name)
		}
		ownerFP := nogoFingerprint(expandBazelrcConfig(rc, row.configs[0], map[string]bool{}))
		for _, c := range row.configs {
			if prev, ok := seenConfig[c]; ok {
				errorf("--config=%s appears in both row %q and row %q", c, prev, row.name)
			}
			seenConfig[c] = row.name
			fp := nogoFingerprint(expandBazelrcConfig(rc, c, map[string]bool{}))
			if !sameStringSet(fp, ownerFP) {
				errorf("row %q: --config=%s fingerprint %v != owner %s's %v (a key-affecting flag split this config out of the row)",
					row.name, c, fp, row.configs[0], ownerFP)
			}
		}

		if row.ownerIsNogoOnlyStep {
			checkNogoOnlyStepRow(t, rc, workflow, row, ownerFP, errorf)
			continue
		}

		ownerJob := workflow.job(t, row.owner)
		if ownerJob.ContinueOnError {
			errorf("row %q owner %s has continue-on-error; a skipped finding there would never fail anything", row.name, row.owner)
		}
		// Every config the owner's bazel commands select, each expanded
		// recursively: test:ci -> test:prcore, and the sole-run/fresh configs
		// bazel.yml adds through BAZEL_SOLE_RUN/BAZEL_FRESH. Any of them
		// passing --norun_validations (itself or through a --config it pulls
		// in) leaves the row with no validating lane.
		ownerConfigs := jobBazelConfigs(workflow, ownerJob)
		if !slices.Contains(ownerConfigs, row.configs[0]) {
			errorf("row %q owner %s runs no bazel test/build with --config=%s (its commands select %v)", row.name, row.owner, row.configs[0], ownerConfigs)
		}
		for _, c := range ownerConfigs {
			if bazelrcConfigExpansionSkipsValidations(rc, c) {
				errorf("row %q owner %s's --config=%s (or a config it includes) passes --norun_validations; the owner is the row's only validating lane", row.name, row.owner, c)
			}
		}

		nonOwners := append([]string{}, row.nonOwners...)
		if row.name == "race" {
			nonOwners = append(nonOwners, nogoPackageGateJobs...)
		}
		usedConfigs := map[string]bool{}
		for _, job := range nonOwners {
			if job == row.owner {
				errorf("row %q lists its own owner %s as a non-owner", row.name, job)
			}
			isPackageGate := slices.Contains(nogoPackageGateJobs, job)
			if isPackageGate {
				// The package gates spell their flags out on the command
				// line rather than through a named --config
				// (pr_lanes_bazel_coverage_test.go pins the exact string);
				// confirmed here against their job's steps instead of
				// .bazelrc. The step's single `bazel build` invocation is
				// parsed into tokens and fingerprinted exactly as a
				// .bazelrc --config is (nogoFingerprint), rather than
				// substring-matched against the raw run text: a substring
				// check would accept --@rules_go//go/config:race=false (it
				// contains the race=true flag's text as a prefix) and would
				// never notice an unrelated key-affecting flag, like
				// --platforms, drifting between this step and the row's
				// owner (review of #7482, should-fix 3).
				step := workflow.job(t, job).step(t, "bazel build //cmd/bd:bd_for_tests")
				invocations := bazelInvocations(step.Run, "build")
				if len(invocations) != 1 {
					errorf("row %q non-owner %s's build step runs %d `bazel build` commands, want exactly 1", row.name, job, len(invocations))
					continue
				}
				args := invocations[0]
				fp := nogoFingerprint([]string{"build " + strings.Join(args, " ")})
				if !sameStringSet(fp, ownerFP) {
					errorf("row %q non-owner %s's build step fingerprint %v != owner %s's %v", row.name, job, fp, row.configs[0], ownerFP)
				}
				if !slices.Contains(args, "--norun_validations") {
					errorf("row %q non-owner %s's build step does not pass --norun_validations (as its own argument)", row.name, job)
				}
				continue
			}
			// Map job -> the row config(s) its own bazel commands select.
			jobConfigs := jobBazelConfigs(workflow, workflow.job(t, job))
			if slices.Contains(jobConfigs, row.configs[0]) {
				errorf("row %q non-owner %s runs the owner's --config=%s; it duplicates %s's nogo", row.name, job, row.configs[0], row.owner)
			}
			var laneConfigs []string
			for _, c := range jobConfigs {
				if slices.Contains(row.configs[1:], c) {
					laneConfigs = append(laneConfigs, c)
				}
			}
			if len(laneConfigs) == 0 {
				errorf("row %q non-owner %s runs none of the row's non-owner configs %v (its commands select %v)", row.name, job, row.configs[1:], jobConfigs)
			}
			for _, cfg := range laneConfigs {
				usedConfigs[cfg] = true
				if !bazelrcConfigSkipsValidations(rc, cfg) {
					errorf("row %q non-owner %s (--config=%s) does not pass --norun_validations; it duplicates %s's nogo", row.name, job, cfg, row.owner)
				}
			}
		}
		for _, c := range row.configs[1:] {
			if !usedConfigs[c] {
				errorf("row %q config --config=%s is run by none of its non-owner jobs %v", row.name, c, row.nonOwners)
			}
		}
	}
}

// checkNogoOnlyStepRow checks a row (f5-spec.md §2.3, S2 Variant A) whose
// owner is not a full validating lane but a narrow nogo-only `bazel build`
// step: every row.configs member -- the nominal owner's own full lane
// included -- must pass --norun_validations (Variant A: no full lane
// validates this configuration at all), and the owner job must run exactly
// one `bazel build --output_groups=nogo_fix` step whose fingerprint matches
// the row's, unconditionally capable of failing the job, so it stays the
// configuration's one validating lane and a drifting flag cannot quietly
// make it validate something else.
func checkNogoOnlyStepRow(t *testing.T, rc string, workflow ciWorkflow, row nogoConfigurationRow, ownerFP []string, errorf func(string, ...any)) {
	t.Helper()
	for _, c := range row.configs {
		if !bazelrcConfigSkipsValidations(rc, c) {
			errorf("row %q (nogo-only-step owner): --config=%s does not pass --norun_validations; under Variant A no full lane validates this configuration, only %s's nogo-only step does", row.name, c, row.owner)
		}
	}

	ownerJob := workflow.job(t, row.owner)
	if ownerJob.ContinueOnError {
		errorf("row %q owner %s has continue-on-error; a failed nogo-only step there would never fail the job", row.name, row.owner)
	}

	var nogoSteps []ciWorkflowStep
	for _, st := range ownerJob.Steps {
		for _, args := range bazelInvocations(st.Run, "build") {
			if slices.Contains(args, "--output_groups=nogo_fix") {
				nogoSteps = append(nogoSteps, st)
			}
		}
	}
	if len(nogoSteps) != 1 {
		errorf("row %q: owner %s has %d `bazel build --output_groups=nogo_fix` steps, want exactly 1; it is this configuration's only validating lane", row.name, row.owner, len(nogoSteps))
		return
	}
	step := nogoSteps[0]

	if step.ContinueOnError != nil && step.ContinueOnError != false {
		errorf("row %q: owner %s's nogo-only step %q has continue-on-error; a finding there would never fail the job", row.name, row.owner, step.Name)
	}
	if !strings.Contains(step.Run, "--keep_going") {
		errorf("row %q: owner %s's nogo-only step %q has no --keep_going; one finding would hide another", row.name, row.owner, step.Name)
	}

	var lines []string
	for _, args := range bazelInvocations(step.Run, "build") {
		if !slices.Contains(args, "--output_groups=nogo_fix") {
			continue
		}
		lines = append(lines, "build "+strings.Join(args, " "))
		// The command line's own --config=X (e.g. --config=integration)
		// pulls in its .bazelrc flags (build:integration's tags, here) the
		// same way an owner config's does; expanded recursively so a
		// nested --config reference is not missed either.
		for _, a := range args {
			if v, ok := strings.CutPrefix(a, "--config="); ok {
				lines = append(lines, expandBazelrcConfig(rc, v, map[string]bool{})...)
			}
		}
	}
	fp := nogoFingerprint(lines)
	if !sameStringSet(fp, ownerFP) {
		errorf("row %q: owner %s's nogo-only step fingerprint %v != --config=%s's %v; it must build the configuration it is meant to validate", row.name, row.owner, fp, row.configs[0], ownerFP)
	}

	// Every config in the row, including the owner's own (row.configs[0]),
	// must actually be run by the job listed for it -- the owner job itself
	// for row.configs[0] -- read from what each job's bazel commands
	// select, not from table order, exactly as the full-lane case checks
	// its non-owners.
	jobFor := func(job string) []string { return jobBazelConfigs(workflow, workflow.job(t, job)) }
	if !slices.Contains(jobFor(row.owner), row.configs[0]) {
		errorf("row %q owner %s runs no bazel test/build with --config=%s (its commands select %v)", row.name, row.owner, row.configs[0], jobFor(row.owner))
	}
	usedConfigs := map[string]bool{row.configs[0]: true}
	for _, job := range row.nonOwners {
		jobConfigs := jobFor(job)
		var laneConfigs []string
		for _, c := range jobConfigs {
			if slices.Contains(row.configs[1:], c) {
				laneConfigs = append(laneConfigs, c)
			}
		}
		if len(laneConfigs) == 0 {
			errorf("row %q non-owner %s runs none of the row's non-owner configs %v (its commands select %v)", row.name, job, row.configs[1:], jobConfigs)
		}
		for _, c := range laneConfigs {
			usedConfigs[c] = true
		}
	}
	for _, c := range row.configs {
		if !usedConfigs[c] {
			errorf("row %q config --config=%s is run by none of its jobs (owner %s, non-owners %v)", row.name, c, row.owner, row.nonOwners)
		}
	}
}

// T2 TestNogoOwnersAreRequired: each owner job is required by pr.yml's CI
// Gate (or, for a flag-gated lane, the flag that is pinned true) with no
// job-level continue-on-error, and its `if:` runs at least everywhere its
// non-owners' do — otherwise a mode could run a non-owner's compile with no
// owner validating it at all (f5-spec.md §9).
func TestNogoOwnersAreRequired(t *testing.T) {
	workflow := readCIWorkflow(t, bazelWorkflowName)
	gate := readCIWorkflow(t, "pr.yml").job(t, "ci-gate")
	gateEnv := gate.step(t, "Evaluate CI gate").Env
	required := strings.Fields(gateEnv["CI_GATE_REQUIRED"])

	ownerGateToken := map[string]string{
		bazelJobName:      "BAZEL_TEST",
		bazelPureJobName:  "BAZEL_PURE",
		bazelIntegJobName: "BAZEL_INTEGRATION",
	}

	events := []string{"pull_request", "merge_group", "pull_request_target", "push", "schedule", "workflow_dispatch"}

	for _, row := range nogoConfigurations {
		ownerJob := workflow.job(t, row.owner)
		if token, ok := ownerGateToken[row.owner]; ok && !contains(required, token) {
			t.Errorf("row %q owner %s: CI_GATE_REQUIRED %v lacks %s", row.name, row.owner, required, token)
		}
		if ownerJob.ContinueOnError {
			t.Errorf("row %q owner %s has continue-on-error", row.name, row.owner)
		}

		nonOwners := append([]string{}, row.nonOwners...)
		if row.name == "race" {
			nonOwners = append(nonOwners, nogoPackageGateJobs...)
		}
		for _, job := range nonOwners {
			if slices.Contains(nogoPackageGateJobs, job) {
				// The package gates' bazel build only ever runs in mode
				// remote (TestPackageGateJobs pins the surrounding `if:`),
				// and the owner's `if:` (mode != 'skip') is true whenever
				// mode == 'remote': "remote" is never "skip".
				step := workflow.job(t, job).step(t, "bazel build //cmd/bd:bd_for_tests")
				if !strings.Contains(step.If, "mode == 'remote'") {
					t.Errorf("%s's bazel-build step if %q no longer implies the owner's mode != 'skip'", job, step.If)
				}
				continue
			}
			nonOwnerJob := workflow.job(t, job)
			for _, mode := range bazelRBEModes {
				for _, event := range events {
					ctx := map[string]string{
						"needs.rbe.outputs.mode":    mode,
						"needs.rbe.outputs.enabled": bazelModeEnabled(mode),
						"github.event_name":         event,
					}
					if evalRRCIf(t, nonOwnerJob.If, ctx) && !evalRRCIf(t, ownerJob.If, ctx) {
						t.Errorf("row %q: %s runs in mode=%s event=%s but owner %s does not (owner if %q, non-owner if %q)",
							row.name, job, mode, event, row.owner, ownerJob.If, nonOwnerJob.If)
					}
				}
			}
		}
	}
}

// T3 TestNogoRaceOwnerBuildsEverything: the race owner's target set covers
// every file any race-group lane compiles (f5-spec.md §2.1): its test step
// runs over the whole tree with no "-//" exclusion, and test:ci's expansion
// narrows no target selection (--build_tests_only, --build_tag_filters)
// — only test execution (--test_tag_filters etc., which T1's fingerprint
// deliberately ignores).
func TestNogoRaceOwnerBuildsEverything(t *testing.T) {
	checkNogoRaceOwnerBuildsEverything(t, readNogoPolicyInputs(t), t.Errorf)
}

func checkNogoRaceOwnerBuildsEverything(t *testing.T, in nogoPolicyInputs, errorf func(string, ...any)) {
	t.Helper()
	workflow := in.workflow
	ownerJob := workflow.job(t, bazelJobName)
	test := ownerJob.step(t, "bazel test //... --config=ci")
	if !strings.Contains(test.Run, "bazel test //... --config=ci") {
		errorf("%s step does not run bazel test //... --config=ci:\n%s", bazelJobName, test.Run)
		return
	}
	// A negative target pattern anywhere in the command, including on a
	// backslash-continued line, narrows //.... Every argument of the
	// command is checked, not just its first line.
	for _, args := range bazelInvocations(test.Run, "test") {
		for _, a := range args {
			if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") {
				errorf("%s excludes a target (%s) from //...: it would stop validating what it excludes", bazelJobName, a)
			}
		}
	}
	// The race owner's validating step must be unconditional: a step-level
	// `if:` or continue-on-error here would let the suite report green while
	// race-group nogo silently did not run, with nothing else in this file
	// (or T1/T2, which only check job-level fields) noticing.
	if test.If != "" {
		errorf("%s step %q has if %q; the race owner's validating step must be unconditional", bazelJobName, test.Name, test.If)
	}
	if test.ContinueOnError != nil && test.ContinueOnError != false {
		errorf("%s step %q has continue-on-error %v; a failed race-group nogo finding there would never fail the job", bazelJobName, test.Name, test.ContinueOnError)
	}
	if ownerJob.ContinueOnError {
		errorf("%s job has continue-on-error; the race owner's job must be able to fail", bazelJobName)
	}

	// Every --config the owner's own command line selects, each expanded
	// recursively: not only "ci" (test:ci -> test:prcore), but also any
	// config bazel.yml adds through a shell variable, like BAZEL_SOLE_RUN's
	// test:sole-run --build_tests_only. An earlier version checked only
	// "ci" statically and so missed a narrowing flag reachable solely
	// through such a variable (review of #7482, should-fix 2).
	for _, c := range jobBazelConfigs(workflow, ownerJob) {
		for _, l := range expandBazelrcConfig(in.rc, c, map[string]bool{}) {
			if strings.Contains(l, "build_tests_only") || strings.Contains(l, "build_tag_filters") {
				errorf("%s's --config=%s (or a config it includes) narrows the target set: %q; the race owner must build //... whole", bazelJobName, c, l)
			}
		}
	}
}

// T4 TestNogoIntegrationStepCoversItsConfiguration: f5-spec.md §6 T4
// (Variant A). bazel-integration's nogo-only step's target set must be
// exactly the integration configuration's non-test library packages, found
// by a source scan (go/build's own tag matching, diffed with and without
// the "integration" tag) rather than hardcoded, so a new integration-tagged
// library file lands in the nogo-only step automatically instead of
// silently losing coverage, and a file that stops needing the tag drops
// out instead of the step validating a target gazelle no longer builds that
// way.
func TestNogoIntegrationStepCoversItsConfiguration(t *testing.T) {
	root := sourceRepoRoot(t)
	workflow := readCIWorkflow(t, bazelWorkflowName)
	job := workflow.job(t, bazelIntegJobName)

	var nogoArgs []string
	for _, st := range job.Steps {
		for _, args := range bazelInvocations(st.Run, "build") {
			if slices.Contains(args, "--output_groups=nogo_fix") {
				nogoArgs = args
			}
		}
	}
	if nogoArgs == nil {
		t.Fatalf("%s has no `bazel build --output_groups=nogo_fix` step", bazelIntegJobName)
	}

	got := map[string]bool{}
	for _, a := range nogoArgs {
		if strings.HasPrefix(a, "//") {
			pkg, _, _ := strings.Cut(a, ":")
			got[pkg] = true
		}
	}

	want := integrationOnlyLibraryPackages(t, root)
	if len(want) == 0 {
		t.Fatal("source scan found no integration-tagged library package; the scan itself is broken")
	}
	for _, pkg := range want {
		if !got[pkg] {
			t.Errorf("source scan found integration-tagged library package %s with no target in %s's nogo-only step (targets: %v)", pkg, bazelIntegJobName, nogoArgs)
		}
	}
	for pkg := range got {
		if !slices.Contains(want, pkg) {
			t.Errorf("%s's nogo-only step targets %s, which the source scan does not classify as an integration-tagged library package (scan found: %v)", bazelIntegJobName, pkg, want)
		}
	}
}

// integrationOnlyLibraryPackages scans root for non-test .go files that
// compile only when the "integration" build tag is set (go/build.Context's
// MatchFile, diffed with and without the tag), and returns their Bazel
// package labels ("//dir", repo root relative), sorted and deduplicated. A
// file that compiles either way (no integration constraint), or that exists
// only as a _test.go file, is not included: a _test.go file is not a
// library's ValidateNogo target either way (f5-spec.md §3's accepted loss).
func integrationOnlyLibraryPackages(t *testing.T, root string) []string {
	t.Helper()
	without := &build.Context{GOOS: "linux", GOARCH: "amd64", Compiler: "gc"}
	with := &build.Context{GOOS: "linux", GOARCH: "amd64", Compiler: "gc", BuildTags: []string{"integration", "gms_pure_go"}}

	seen := map[string]bool{}
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || strings.HasPrefix(n, "bazel-") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir, name := filepath.Split(path)
		matchesWithout, errW := without.MatchFile(dir, name)
		matchesWith, errT := with.MatchFile(dir, name)
		if errW != nil || errT != nil {
			return nil // an unparsable file is not this scan's concern
		}
		if !matchesWith || matchesWithout {
			return nil
		}
		rel, relErr := filepath.Rel(root, strings.TrimSuffix(dir, string(filepath.Separator)))
		if relErr != nil {
			return relErr
		}
		pkg := "//" + filepath.ToSlash(rel)
		if !seen[pkg] {
			seen[pkg] = true
			out = append(out, pkg)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan for integration-tagged library files under %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

// T5 TestNoUnconditionalValidationOff: no common/build/test line without a
// :config suffix touches run_validations (that would turn validation off
// everywhere, defeating every row's owner), and neither does build:nogo*,
// build:release-cross, build:js-wasm, scripts/ci/bazel-release-cross-compile.sh,
// or any row's own owner config (an owner opting itself out would leave its
// configuration with no validating lane at all).
func TestNoUnconditionalValidationOff(t *testing.T) {
	root := sourceRepoRoot(t)
	checkNoUnconditionalValidationOff(readNogoPolicyInputs(t), t.Errorf)
	if script := readPolicyFile(t, root, "scripts/ci/bazel-release-cross-compile.sh"); strings.Contains(script, "run_validations") {
		t.Error("scripts/ci/bazel-release-cross-compile.sh turns nogo validation off; release-cross has no non-owner duplicate to remove")
	}
}

func checkNoUnconditionalValidationOff(in nogoPolicyInputs, errorf func(string, ...any)) {
	rc := in.rc
	for _, raw := range strings.Split(rc, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		head, _, _ := strings.Cut(line, " ")
		cmd, _, hasCfg := strings.Cut(head, ":")
		if cmd != "common" && cmd != "build" && cmd != "test" {
			continue
		}
		if !hasCfg && strings.Contains(line, "run_validations") {
			errorf(".bazelrc %q: an unconfigured line must not touch run_validations; it would apply to every lane, including every row's owner", line)
		}
	}

	for _, prefix := range []string{"build:nogo", "build:release-cross", "build:js-wasm"} {
		for _, raw := range strings.Split(rc, "\n") {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, prefix) && strings.Contains(line, "validation") {
				errorf(".bazelrc %q changes validation for %s, which has no non-owner duplicate to remove", line, prefix)
			}
		}
	}

	for _, row := range nogoConfigurations {
		if row.ownerIsNogoOnlyStep {
			// Variant A (f5-spec.md §2.3): this row's owner config is
			// deliberately not the validating lane -- the owner job's
			// nogo-only step is -- so it is expected (and required
			// elsewhere, by T1/T6) to pass --norun_validations too.
			continue
		}
		if bazelrcConfigExpansionSkipsValidations(rc, row.configs[0]) {
			errorf("row %q owner config --config=%s (or a config it includes) passes --norun_validations; it is the row's only validating lane", row.name, row.configs[0])
		}
	}
}

// T6 TestRunValidationsOnlyOnAllowlistedLines: the only
// run_validations/norun_validations lines anywhere in .bazelrc or bazel.yml
// are exactly the ones nogoConfigurations' rows already require: one
// "test:<config> --norun_validations" line per non-owner config in a
// full-lane row (race), or per config (owner's own full lane included) in a
// nogo-only-step row (integ, f5-spec.md §5 S2 Variant A), plus the two
// package-gate jobs' bazel-build steps. Main's TestLintAndVetRunAsNogo used
// to ban run_validations outright, which caught a configuration outside
// T1's table quietly adding --norun_validations with no owner to notice. T5
// does not re-check that case: it only forbids unconfigured lines and a
// short prefix list. This allowlist restores the ban for every other line
// without re-banning the ones the rows require.
func TestRunValidationsOnlyOnAllowlistedLines(t *testing.T) {
	checkRunValidationsOnlyOnAllowlistedLines(t, readNogoPolicyInputs(t), t.Errorf)
}

func checkRunValidationsOnlyOnAllowlistedLines(t *testing.T, in nogoPolicyInputs, errorf func(string, ...any)) {
	t.Helper()
	// Every row classifies its own allowed lines: a full-lane row's
	// non-owner configs (configs[1:] -- the owner's own full lane must
	// still validate), or, for a nogo-only-step row (f5-spec.md §5 S2
	// Variant A), every config in it, owner's own full lane included,
	// since under Variant A none of them validates.
	allowed := map[string]bool{}
	for _, row := range nogoConfigurations {
		cfgs := row.configs[1:]
		if row.ownerIsNogoOnlyStep {
			cfgs = row.configs
		}
		for _, cfg := range cfgs {
			allowed["test:"+cfg+" --norun_validations"] = true
		}
	}
	if len(allowed) == 0 {
		t.Fatal("no row has a config to allowlist")
	}

	for _, raw := range strings.Split(in.rc, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "run_validations") && !allowed[line] {
			errorf(".bazelrc %q: run_validations is only allowed as one of %v; any other line (an unlisted config, an --integration config, or a bare --run_validations toggle) would turn validation off with no owner lane catching it", line, sortedKeys(allowed))
		}
	}

	// bazel.yml: only the two package-gate jobs' bazel-build steps may pass
	// --norun_validations (they spell the race build flags out on the
	// command line rather than through a .bazelrc --config; T1 pins their
	// content). Anywhere else -- a new job, a new step, a non-package-gate
	// job -- would turn a lane's validation off unnoticed.
	for name, j := range in.workflow.Jobs {
		for _, st := range j.Steps {
			if !strings.Contains(st.Run, "run_validations") {
				continue
			}
			if !slices.Contains(nogoPackageGateJobs, name) {
				stepLabel := st.Name
				if stepLabel == "" {
					stepLabel = st.Uses
				}
				errorf("%s job %q step %q: run_validations outside the package-gate build steps", bazelWorkflowName, name, stepLabel)
			}
		}
	}
}

// TestNogoPolicyRejectsMutations: each mutation below is a .bazelrc or
// bazel.yml edit that would leave some nogo configuration with no validating
// required lane (or a non-owner duplicating its owner), and must be rejected
// by T1, T3, T5 or T6 on its own -- not only by an unrelated exact-content
// pin elsewhere in //scripts that an author would update alongside it. The
// unmutated files must pass.
func TestNogoPolicyRejectsMutations(t *testing.T) {
	rc := readPolicyFile(t, bazelPolicyRoot(t), ".bazelrc")
	wf := readPolicyFile(t, sourceRepoRoot(t), filepath.Join(".github", "workflows", bazelWorkflowName))

	const ownerCmd = `bazel test //... --config=ci ${BAZEL_SOLE_RUN:+"$BAZEL_SOLE_RUN"} ${BAZEL_FRESH:+"$BAZEL_FRESH"} \` + "\n"
	const ownerProfile = `            --profile="$RUNNER_TEMP/bazel-profile.json" \` + "\n"
	addRC := func(line string) func(string, string) (string, string) {
		return func(rc, wf string) (string, string) { return rc + "\n" + line + "\n", wf }
	}
	replace := func(inRC bool, old, new string) func(string, string) (string, string) {
		return func(rc, wf string) (string, string) {
			target := &wf
			if inRC {
				target = &rc
			}
			if !strings.Contains(*target, old) {
				t.Fatalf("mutation anchor %q not found", old)
			}
			*target = strings.Replace(*target, old, new, 1)
			return rc, wf
		}
	}

	mutations := []struct {
		name   string
		mutate func(rc, wf string) (string, string)
	}{
		// 1a/1b: under F5 S2 Variant A, test:integration and
		// test:doltserver-cmd already pass --norun_validations (their row's
		// owner is bazel-integration's nogo-only step, not a full lane), so
		// adding the line again is a no-op, not a policy violation; see the
		// s2* mutations below for this row's own negative cases.
		{"1c unknown new lane skips validations", addRC("test:newlane --norun_validations")},
		{"1d owner command line skips validations", replace(false, ownerCmd,
			strings.Replace(ownerCmd, "--config=ci ", "--config=ci --norun_validations ", 1))},
		{"1d owner continuation line skips validations", replace(false, ownerProfile,
			ownerProfile+"            --norun_validations \\\n")},
		{"1e owner-inherited prcore skips validations", addRC("test:prcore --norun_validations")},
		{"1e owner command's sole-run skips validations", addRC("test:sole-run --norun_validations")},
		{"1e owner command's sole-run inherits a non-owner config", addRC("test:sole-run --config=doltserver")},
		{"1e owner-inherited prcore inherits a non-owner config", addRC("test:prcore --config=dolt-race")},
		{"1f non-owner embedded race=false", replace(true, "test:embedded --@rules_go//go/config:race\n",
			"test:embedded --@rules_go//go/config:race=false\n")},
		{"1f non-owner doltserver race=false", replace(true, "test:doltserver --@rules_go//go/config:race\n",
			"test:doltserver --@rules_go//go/config:race=false\n")},
		{"1f non-owner embedded pure=false", replace(true, "test:embedded --@rules_go//go/config:race\n",
			"test:embedded --@rules_go//go/config:race\ntest:embedded --@rules_go//go/config:pure=false\n")},
		{"1g owner excludes a target on its first line", replace(false, ownerCmd,
			strings.Replace(ownerCmd, "--config=ci ", "--config=ci -//cmd/bd/... ", 1))},
		{"1g owner excludes a target on a continuation line", replace(false, ownerProfile,
			ownerProfile+"            -//cmd/bd/... \\\n")},
		{"2 non-owner job runs the owner's config", replace(false,
			"bazel test //... --config=embedded ${BAZEL_FRESH", "bazel test //... --config=ci ${BAZEL_FRESH")},

		// F5 S2 Variant A (the "integ" row, ownerIsNogoOnlyStep): its owner
		// is bazel-integration's nogo-only `bazel build
		// --output_groups=nogo_fix` step, not a full lane.
		{"s2a nogo-only step removed", replace(false,
			"--keep_going \\\n            --output_groups=nogo_fix \\\n            -- //internal/testutil/integration:integration \\\n",
			"--keep_going \\\n            -- //internal/testutil/integration:integration \\\n")},
		{"s2b nogo-only step drifts onto a different configuration", replace(false,
			"bazel build --config=integration --@rules_go//go/config:race --keep_going \\\n",
			"bazel build --config=embedded --@rules_go//go/config:race --keep_going \\\n")},
		{"s2c nogo-only step has continue-on-error", replace(false,
			"        id: nogo-integration\n        if: ${{ always() && steps.test.outcome != 'skipped' }}\n        timeout-minutes: 10\n",
			"        id: nogo-integration\n        if: ${{ always() && steps.test.outcome != 'skipped' }}\n        continue-on-error: true\n        timeout-minutes: 10\n")},

		// Should-fix 1 (review of #7482): expandBazelrcConfig must follow a
		// --config= reference on a common: line, not only a whole line
		// beginning "build --config=" / "test --config=". Before the fix,
		// test:ci's own chain would never reach sf1lane, and this
		// --norun_validations line would have passed unnoticed.
		{"should-fix 1: owner inherits validations-off through a common: config", func(rc, wf string) (string, string) {
			return rc + "\ncommon:ci --config=sf1lane\ntest:sf1lane --norun_validations\n", wf
		}},
		// Should-fix 2 (review of #7482): TestNogoRaceOwnerBuildsEverything
		// must expand every config the owner's command line reaches,
		// including one selected only through a shell variable
		// (BAZEL_SOLE_RUN -> --config=sole-run), not only the literal "ci"
		// it types out. Before the fix, a narrowing flag reachable solely
		// through sole-run would have passed unnoticed.
		{"should-fix 2: sole-run (reached only via BAZEL_SOLE_RUN) narrows the target set", addRC("test:sole-run --build_tests_only")},
		// Should-fix 3 (review of #7482): the package-gate non-owner check
		// must fingerprint its bazel-build step's full argument list, not
		// substring-match the raw run text. Before the fix, an added
		// key-affecting flag (here --platforms, which changes what the
		// step actually builds) passed unnoticed as long as the race flag
		// and --norun_validations substrings were still present somewhere
		// in the line.
		{"should-fix 3: package gate build step gains an unfingerprinted key flag", replace(false,
			"bazel build --@rules_go//go/config:race //cmd/bd:bd_for_tests --remote_download_regex='.*/bin/cmd/bd/bd_for_tests/bd$' --norun_validations",
			"bazel build --@rules_go//go/config:race //cmd/bd:bd_for_tests --remote_download_regex='.*/bin/cmd/bd/bd_for_tests/bd$' --platforms=//tools/bazel:fake_platform --norun_validations")},
	}

	check := func(rc, wf string) []string {
		var workflow ciWorkflow
		if err := yaml.Unmarshal([]byte(wf), &workflow); err != nil {
			t.Fatalf("parse mutated %s: %v", bazelWorkflowName, err)
		}
		in := nogoPolicyInputs{rc: rc, workflow: workflow}
		var problems []string
		errorf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
		checkNogoConfigurationsHaveOneValidatingLane(t, in, errorf)
		checkNogoRaceOwnerBuildsEverything(t, in, errorf)
		checkNoUnconditionalValidationOff(in, errorf)
		checkRunValidationsOnlyOnAllowlistedLines(t, in, errorf)
		return problems
	}

	if problems := check(rc, wf); len(problems) != 0 {
		t.Fatalf("unmutated .bazelrc/%s fail the nogo policy: %q", bazelWorkflowName, problems)
	}
	for _, m := range mutations {
		mrc, mwf := m.mutate(rc, wf)
		if mrc == rc && mwf == wf {
			t.Fatalf("mutation %q changed nothing", m.name)
		}
		if problems := check(mrc, mwf); len(problems) == 0 {
			t.Errorf("mutation %q: accepted by T1/T3/T5/T6; it must be rejected", m.name)
		} else {
			t.Logf("mutation %q: rejected: %s", m.name, problems[0])
		}
	}
}
