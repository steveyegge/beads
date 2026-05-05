//go:build integration_parity

// Package parity hosts cross-backend UX parity tests. It is intentionally
// distinct from internal/storage/postgres (which exercises the driver
// in isolation) and cmd/bd init tests (which exercise initialization
// against one backend at a time) — its job is to assert that the bd
// CLI surface presents the same user-visible output regardless of
// which backend serves it.
//
// The test is gated by build tag `integration_parity` per ADR be-l7t.6
// (FR-8). Activated by both legs of the CI matrix; skipped when neither
// backend can be reached (e.g. local dev without a Docker socket).
package parity

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage/postgres/testfixture"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the parity golden fixture from a successful run (PG only)")

// scenarioStep describes one bd CLI invocation in the parity scenario.
// args is interpolated against the scenario state (see runScenario for
// the variable substitution scheme): "{ID1}" expands to the first
// captured issue ID, "{ID2}" to the second, etc.
type scenarioStep struct {
	name string
	args []string
	// expectExit is the expected process exit code. 0 unless explicitly set.
	expectExit int
	// captureID, when non-zero, parses the resulting JSON output and stashes
	// the issue ID at scenarioState.ids[captureID-1] for later interpolation.
	captureID int
	// json controls whether stdout is parsed as JSON for ID extraction.
	json bool
	// extraEnv holds step-scoped environment additions appended to the
	// scenario-wide extraEnv. Used to exercise env-driven gates (e.g.
	// BD_BACKUP_ENABLED=true to trigger the be-xz4 PostRun path).
	extraEnv []string
}

// scenario is the canonical bd CLI sequence exercised in both legs.
// Order matters; later steps reference IDs captured by earlier steps.
//
// The "backup-status-fresh" and "auto-backup-trigger" steps exercise
// the be-xz4 gating sweep. Without the gates, `bd create` with
// BD_BACKUP_ENABLED=true fatal-exits on PG with "Dolt backend required"
// from PersistentPostRun → maybeAutoBackup → dVC.GetCurrentCommit.
// The auto-backup-trigger step is invoked with backup.enabled set via
// the environment (see runParityScenario) so no on-disk config.yaml is
// required.
var scenario = []scenarioStep{
	{name: "create-parent", args: []string{"create", "Parent issue", "-t", "task", "-p", "1", "--json"}, captureID: 1, json: true},
	{name: "create-child", args: []string{"create", "Child issue", "-t", "task", "-p", "2", "--json"}, captureID: 2, json: true},
	{name: "dep-add", args: []string{"dep", "add", "{ID2}", "{ID1}", "--type", "blocks"}},
	{name: "ready", args: []string{"ready", "--json"}},
	{name: "claim-parent", args: []string{"update", "{ID1}", "--claim", "--assignee", "tester", "--json"}},
	{name: "comment", args: []string{"comment", "{ID1}", "parity scenario comment"}},
	{name: "close-parent", args: []string{"close", "{ID1}", "--reason", "scenario complete"}},
	{name: "export", args: []string{"export"}},
	{name: "backup-status-fresh", args: []string{"backup", "status", "--json"}, extraEnv: []string{"BD_BACKUP_ENABLED=true"}},
	{name: "auto-backup-trigger", args: []string{"create", "Post-backup-enable issue", "-t", "task", "-p", "3", "--json"}, captureID: 3, json: true, extraEnv: []string{"BD_BACKUP_ENABLED=true"}},
}

// scenarioState carries values captured during the scenario for use in
// later interpolation steps.
type scenarioState struct {
	ids []string
}

// idPattern normalizes captured bd issue IDs to `<ID>` so byte-equality
// comparisons across backends remain stable. The tmpDir-derived prefix
// follows the shape `bd-parity-<digits>-<suffix>` where suffix is either
// a 6-char base32 hash (PG) or a numeric counter (Dolt). The regex
// anchors on the static `bd-parity-` prefix so generic words like
// `create-parent` are NOT touched.
var idPattern = regexp.MustCompile(`bd-parity-\d+-[a-zA-Z0-9]+`)

// timestampPattern normalizes RFC3339 timestamps in JSON output.
var timestampPattern = regexp.MustCompile(`"\w+_at"\s*:\s*"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})"`)

// uuidPattern normalizes random project IDs / clone IDs.
var uuidPattern = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

// pathPattern normalizes per-test directory paths (always under /tmp).
var pathPattern = regexp.MustCompile(`/tmp/[a-zA-Z0-9._-]+`)

// goldenFile names the path to the committed reference output, relative
// to this test package's directory (testdata/ co-located with the test).
const goldenFile = "internal/storage/parity/testdata/parity_scenario.golden.txt"

func TestUXParity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("parity scenario runs Unix-shell-style subprocesses; not portable to Windows in v1")
	}
	bd := buildBD(t)

	got, err := runParityScenario(t, bd)
	if err != nil {
		t.Fatalf("scenario: %v", err)
	}

	normalized := normalizeOutput(got)

	if *updateGolden {
		if err := os.WriteFile(filepath.Join(repoRoot(t), goldenFile), []byte(normalized), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("Updated golden: %s", goldenFile)
		return
	}

	want, err := os.ReadFile(filepath.Join(repoRoot(t), goldenFile))
	if err != nil {
		t.Fatalf("read golden: %v\n  hint: run with -update-golden after a known-good PG run", err)
	}

	if string(want) != normalized {
		t.Errorf("parity scenario diverged from golden\n--- want\n%s\n--- got\n%s",
			truncate(string(want), 2000), truncate(normalized, 2000))
	}
}

// runParityScenario executes the canonical bd CLI sequence against a
// freshly-initialized PG-backed bd, captures stdout for each step, and
// returns the concatenated transcript.
func runParityScenario(t *testing.T, bd string) (string, error) {
	t.Helper()
	rawDSN := testfixture.ForTest(t)
	dir := isolatedTempDir(t)

	// Extract password from raw DSN so subsequent (post-init) runs can
	// authenticate via BEADS_POSTGRES_PASSWORD. metadata.json carries the
	// stripped form by design (see store_factory.go).
	password := extractPasswordFromDSN(rawDSN)
	t.Logf("rawDSN=%q password=<redacted>", rawDSN)
	// Deterministic identity: parity goldens must be byte-identical across
	// rigs/CI/dev. The bd CLI derives created_by/owner/author from
	// BEADS_ACTOR, BD_ACTOR, GIT_AUTHOR_EMAIL, etc. Inheriting them from the
	// caller's shell contaminates the golden — see runBDEnv where these are
	// scrubbed from the inherited environment.
	extraEnv := []string{
		"BEADS_DOLT_AUTO_START=0",
		"BEADS_ACTOR=parity-author",
		"BD_ACTOR=parity-author",
		"GIT_AUTHOR_EMAIL=parity@bd.test",
		"GIT_AUTHOR_NAME=parity-author",
		"GIT_COMMITTER_EMAIL=parity@bd.test",
		"GIT_COMMITTER_NAME=parity-author",
	}
	if password != "" {
		extraEnv = append(extraEnv, "BEADS_POSTGRES_PASSWORD="+password)
	}

	state := &scenarioState{}
	var buf bytes.Buffer

	initStdout, initStderr, initErr := runBDEnv(bd, dir, extraEnv, []string{"init", "--backend", "postgres", "--dsn", rawDSN, "--quiet"})
	if initErr != nil {
		return "", fmt.Errorf("init: %w\nstdout: %s\nstderr: %s", initErr, initStdout, initStderr)
	}
	t.Logf("init stdout=%q stderr=%q", initStdout, initStderr)
	if entries, err := os.ReadDir(filepath.Join(dir, ".beads")); err != nil {
		t.Logf(".beads dir missing after init: %v", err)
	} else {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Logf(".beads contents: %v", names)
	}

	for _, step := range scenario {
		args := interpolate(step.args, state)
		stepEnv := extraEnv
		if len(step.extraEnv) > 0 {
			stepEnv = append(append([]string{}, extraEnv...), step.extraEnv...)
		}
		stdout, stderr, err := runBDEnv(bd, dir, stepEnv, args)
		exit := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exit = exitErr.ExitCode()
			} else {
				return "", fmt.Errorf("step %s: %w", step.name, err)
			}
		}

		if exit != step.expectExit {
			return "", fmt.Errorf("step %s: exit=%d want=%d\nstdout: %s\nstderr: %s",
				step.name, exit, step.expectExit, stdout, stderr)
		}

		fmt.Fprintf(&buf, "### step: %s\n", step.name)
		fmt.Fprintf(&buf, "$ bd %s\n", strings.Join(args, " "))
		fmt.Fprintf(&buf, "exit: %d\n", exit)
		fmt.Fprintf(&buf, "stdout:\n%s\n", stdout)
		fmt.Fprintf(&buf, "stderr:\n%s\n\n", stderr)

		if step.captureID > 0 && step.json {
			id, err := extractIDFromJSON(stdout)
			if err != nil {
				return "", fmt.Errorf("step %s: extract id: %w\nraw stdout: %s", step.name, err, stdout)
			}
			for len(state.ids) < step.captureID {
				state.ids = append(state.ids, "")
			}
			state.ids[step.captureID-1] = id
		}
	}

	return buf.String(), nil
}

// interpolate substitutes "{IDN}" placeholders in args with the captured
// scenarioState.ids[N-1] value.
func interpolate(args []string, state *scenarioState) []string {
	out := make([]string, len(args))
	for i, a := range args {
		for j, id := range state.ids {
			a = strings.ReplaceAll(a, fmt.Sprintf("{ID%d}", j+1), id)
		}
		out[i] = a
	}
	return out
}

// extractIDFromJSON pulls the "id" field from a bd JSON response. Handles
// both single-object responses ({"id": ...}) and the bd create batch shape.
func extractIDFromJSON(stdout string) (string, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return "", fmt.Errorf("empty stdout")
	}
	var single struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout), &single); err == nil && single.ID != "" {
		return single.ID, nil
	}
	// Some bd commands return arrays; take the first id.
	var arr []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout), &arr); err == nil && len(arr) > 0 && arr[0].ID != "" {
		return arr[0].ID, nil
	}
	return "", fmt.Errorf("no id in JSON: %s", truncate(stdout, 200))
}

// runBDEnv invokes bd with args + caller-supplied env additions. The
// environment scrubs BEADS_DIR (so subprocess uses the supplied dir) and
// every identity-bearing var so the parity golden stays byte-identical
// across rigs/CI/dev. extraEnv (set by runParityScenario) replaces the
// scrubbed identity vars with deterministic values.
func runBDEnv(bd, dir string, extraEnv, args []string) (string, string, error) {
	cmd := exec.Command(bd, args...)
	cmd.Dir = dir
	scrub := []string{
		"BEADS_DIR",
		"BEADS_ACTOR",
		"BD_ACTOR",
		"GIT_AUTHOR_EMAIL",
		"GIT_AUTHOR_NAME",
		"GIT_COMMITTER_EMAIL",
		"GIT_COMMITTER_NAME",
		"USER",
		"LOGNAME",
		"EMAIL",
	}
	cmd.Env = append(filterEnv(os.Environ(), scrub...), extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// extractPasswordFromDSN returns the password component of a postgres URI
// or keyword DSN, or "" if absent. Used to thread BEADS_POSTGRES_PASSWORD
// to subprocess invocations after init has stripped the credential from
// metadata.json.
func extractPasswordFromDSN(rawDSN string) string {
	// Quick path: URI form.
	if strings.HasPrefix(rawDSN, "postgres://") || strings.HasPrefix(rawDSN, "postgresql://") {
		// Parse out user:pass@host
		rest := strings.TrimPrefix(rawDSN, "postgres://")
		rest = strings.TrimPrefix(rest, "postgresql://")
		atIdx := strings.Index(rest, "@")
		if atIdx < 0 {
			return ""
		}
		creds := rest[:atIdx]
		colonIdx := strings.Index(creds, ":")
		if colonIdx < 0 {
			return ""
		}
		return creds[colonIdx+1:]
	}
	// Keyword form: scan for `password=...`.
	for _, kv := range strings.Fields(rawDSN) {
		if strings.HasPrefix(kv, "password=") {
			return strings.TrimPrefix(kv, "password=")
		}
	}
	return ""
}

// normalizeOutput applies the documented normalization passes so byte
// comparisons across backends are stable. The passes preserve all
// surrounding text, only rewriting volatile substrings.
func normalizeOutput(raw string) string {
	out := raw
	out = uuidPattern.ReplaceAllString(out, "<UUID>")
	out = timestampPattern.ReplaceAllStringFunc(out, func(match string) string {
		// Keep the field name, replace the value.
		i := strings.Index(match, ":")
		if i < 0 {
			return match
		}
		return match[:i+1] + ` "<TIMESTAMP>"`
	})
	out = pathPattern.ReplaceAllString(out, "<TMPPATH>")
	out = idPattern.ReplaceAllString(out, "<ID>")
	return out
}

func filterEnv(env []string, drop ...string) []string {
	out := make([]string, 0, len(env))
outer:
	for _, kv := range env {
		for _, key := range drop {
			if strings.HasPrefix(kv, key+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

func isolatedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "bd-parity-")
	if err != nil {
		t.Fatalf("isolatedTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func buildBD(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	bd := filepath.Join(root, "bd")
	if info, err := os.Stat(bd); err == nil && info.Size() > 0 {
		return bd
	}
	cmd := exec.Command("go", "build", "-tags", "gms_pure_go", "-o", bd, "./cmd/bd/")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build bd: %v\n%s", err, out)
	}
	return bd
}

func repoRoot(t *testing.T) string {
	t.Helper()
	// Walk up from the test file's package directory until we find go.mod.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for dir != "/" {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate go.mod above %s", dir)
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...<truncated>"
}
