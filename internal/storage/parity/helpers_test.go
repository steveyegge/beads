//go:build (gms_pure_go && integration_pg) || integration_daemon

package parity

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	paritybdOnce sync.Once
	paritybdPath string
	paritybdErr  error
)

// buildBD returns the path to a bd binary built for parity tests.
// Uses BEADS_TEST_BD_BINARY if set, otherwise builds from source.
func buildBD(t *testing.T) string {
	t.Helper()
	paritybdOnce.Do(func() {
		if prebuilt := os.Getenv("BEADS_TEST_BD_BINARY"); prebuilt != "" {
			if _, err := os.Stat(prebuilt); err != nil {
				paritybdErr = fmt.Errorf("BEADS_TEST_BD_BINARY=%q not found: %w", prebuilt, err)
				return
			}
			paritybdPath = prebuilt
			return
		}
		name := "bd-parity"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		dir, err := os.MkdirTemp("", "bd-parity-build-*")
		if err != nil {
			paritybdErr = fmt.Errorf("tempdir: %w", err)
			return
		}
		paritybdPath = filepath.Join(dir, name)
		modRoot := findParityModuleRoot(t)
		cmd := exec.Command("go", "build", "-tags", "gms_pure_go", "-o", paritybdPath, "./cmd/bd")
		cmd.Dir = modRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			paritybdErr = fmt.Errorf("go build: %w\n%s", err, out)
		}
	})
	if paritybdErr != nil {
		t.Skipf("parity test: failed to build bd: %v", paritybdErr)
	}
	return paritybdPath
}

func findParityModuleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine file location")
	}
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod")
		}
		dir = parent
	}
}

// runBDEnv runs bd with env in dir with args, returns stdout, stderr, error.
func runBDEnv(bd, dir string, env []string, args []string) (stdout, stderr string, err error) {
	cmd := exec.Command(bd, args...)
	cmd.Dir = dir
	cmd.Env = env
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// isolatedTempDir returns a temporary directory for a parity test.
func isolatedTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// skipOnWindows skips the test on Windows.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("parity tests not supported on Windows")
	}
}

// paritybdEnv returns a clean environment for parity tests, isolating BEADS_* vars.
func paritybdEnv(dir string) []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "BEADS_") {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "HOME="+dir)
	return env
}

// initLifecyclePair initializes a Dolt-backed and a Postgres-backed beads
// directory for parity comparison. Returns their directories and environments.
// Skips if PG is not available.
func initLifecyclePair(t *testing.T, bd string) (doltDir, pgDir string, doltEnv, pgEnv []string) {
	t.Helper()
	pgDSN := os.Getenv("BEADS_TEST_POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("BEADS_TEST_POSTGRES_DSN not set; skipping PG parity test")
	}

	doltDir = initLifecycleDir(t, bd, "dolt", nil)
	doltEnv = paritybdEnv(doltDir)

	pgDir = initLifecycleDir(t, bd, "pg", []string{
		"BEADS_TEST_POSTGRES_DSN=" + pgDSN,
	})
	pgEnv = paritybdEnv(pgDir)
	pgEnv = append(pgEnv, "BEADS_TEST_POSTGRES_DSN="+pgDSN)

	return
}

func initLifecycleDir(t *testing.T, bd, tag string, extraEnv []string) string {
	t.Helper()
	dir := t.TempDir()
	// Create a git repo so bd init works.
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"config", "core.hooksPath", ".git/hooks"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s [%s]: %v\n%s", args[0], tag, err, out)
		}
	}

	env := paritybdEnv(dir)
	env = append(env, extraEnv...)

	initArgs := []string{"init", "--quiet", "--prefix", "parity" + tag, "--skip-hooks", "--skip-agents"}
	stdout, stderr, err := runBDEnv(bd, dir, env, initArgs)
	if err != nil {
		t.Fatalf("bd init [%s]: %v\nstdout: %s\nstderr: %s", tag, err, stdout, stderr)
	}
	return dir
}

// lifecycleCommand is a single step in a lifecycle scenario.
type lifecycleCommand struct {
	name      string
	args      []string
	captureID int // if > 0, capture the created issue ID into state.ids[captureID-1]
}

// lifecycleState holds mutable state accumulated during a lifecycle run.
type lifecycleState struct {
	ids []string
}

// applyLifecycleSteps runs a sequence of bd commands against dir and returns a
// transcript of all output.
func applyLifecycleSteps(t *testing.T, bd, dir string, env []string, state *lifecycleState, steps []lifecycleCommand) string {
	t.Helper()
	var transcript strings.Builder
	for _, step := range steps {
		args := make([]string, len(step.args))
		copy(args, step.args)
		// Substitute captured IDs into args.
		for i, a := range args {
			for j, id := range state.ids {
				args[i] = strings.ReplaceAll(a, fmt.Sprintf("{ID%d}", j+1), id)
			}
		}
		stdout, stderr, err := runBDEnv(bd, dir, env, args)
		transcript.WriteString(fmt.Sprintf("=== %s ===\n%s\n%s\n", step.name, stdout, stderr))
		if err != nil {
			t.Logf("step %q: %v (stderr: %s)", step.name, err, stderr)
		}
		if step.captureID > 0 && stdout != "" {
			id := extractIDFromJSON(stdout)
			for len(state.ids) < step.captureID {
				state.ids = append(state.ids, "")
			}
			state.ids[step.captureID-1] = id
		}
	}
	return transcript.String()
}

// extractIDFromJSON attempts to extract an "id" field from bd --json output.
func extractIDFromJSON(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, `"id"`) {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				id := strings.Trim(strings.TrimSpace(parts[1]), `",`)
				if id != "" {
					return id
				}
			}
		}
	}
	return ""
}

// unifiedDiff returns a minimal diff between two strings (line-by-line).
func unifiedDiff(a, b string) string {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")
	var out strings.Builder
	for i := 0; i < len(aLines) || i < len(bLines); i++ {
		var aLine, bLine string
		if i < len(aLines) {
			aLine = aLines[i]
		}
		if i < len(bLines) {
			bLine = bLines[i]
		}
		if aLine != bLine {
			if i < len(aLines) {
				fmt.Fprintf(&out, "- %s\n", aLine)
			}
			if i < len(bLines) {
				fmt.Fprintf(&out, "+ %s\n", bLine)
			}
		}
	}
	return out.String()
}

// truncate returns s truncated to at most n bytes with a marker if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("... [truncated %d bytes]", len(s)-n)
}

// substituteCapturedIDs replaces captured issue IDs in output with positional
// placeholders like {ID1}, {ID2} so outputs from different backends can be
// compared byte-identically.
func substituteCapturedIDs(s string, ids []string) string {
	result := s
	for i, id := range ids {
		if id == "" {
			continue
		}
		placeholder := fmt.Sprintf("{ID%d}", i+1)
		result = strings.ReplaceAll(result, id, placeholder)
	}
	return result
}

// normalizeOutput removes non-deterministic fields (timestamps, latencies) from
// bd JSON output so it can be compared byte-identically across backends.
func normalizeOutput(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		// Remove fields whose values vary across runs.
		if containsAny(line, `"created_at"`, `"updated_at"`, `"started_at"`,
			`"closed_at"`, `"elapsed_ms"`, `"latency_ms"`) {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
