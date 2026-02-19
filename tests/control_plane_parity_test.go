package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type cliResult struct {
	Output   string
	ExitCode int
	Payload  map[string]interface{}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %s", wd)
		}
		dir = parent
	}
}

func buildBDBinary(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bd-test")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/bd")
	cmd.Dir = root
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build bd binary failed: %v\n%s", err, string(out))
	}
	return bin
}

func probeBDBinary(bin string) error {
	work, err := os.MkdirTemp("", "bd-parity-probe-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(work)
	}()

	initCmd := exec.Command(bin, "init", "--prefix", "test", "--quiet")
	initCmd.Dir = work
	initCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	if out, err := initCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("probe init failed: %v | %s", err, strings.TrimSpace(string(out)))
	}

	createCmd := exec.Command(bin, "--json", "create", "probe", "--description", "probe")
	createCmd.Dir = work
	createCmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1", "BD_ACTOR=probe", "BEADS_ACTOR=probe")
	if out, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("probe create failed: %v | %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}

func resolveBDBinary(t *testing.T, root string) string {
	t.Helper()

	if explicit := strings.TrimSpace(os.Getenv("BD_PARITY_BIN")); explicit != "" {
		if err := probeBDBinary(explicit); err == nil {
			return explicit
		}
	}

	built := buildBDBinary(t, root)
	if err := probeBDBinary(built); err == nil {
		return built
	}

	fallbacks := []string{"/tmp/beads-bd-new2"}
	if inPath, err := exec.LookPath("bd"); err == nil {
		fallbacks = append(fallbacks, inPath)
	}
	for _, candidate := range fallbacks {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if err := probeBDBinary(candidate); err == nil {
			return candidate
		}
	}

	t.Fatalf("no usable bd binary found; set BD_PARITY_BIN to a runnable binary")
	return ""
}

func runCLI(t *testing.T, bin, dir, actor string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"BEADS_NO_DAEMON=1",
		"BD_ACTOR="+actor,
		"BEADS_ACTOR="+actor,
	)
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if ok := errors.As(err, &exitErr); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("command failed before exit code capture: %v\n%s", err, string(out))
		}
	}

	output := string(out)
	payload := parseFirstJSON(t, output)
	return cliResult{
		Output:   output,
		ExitCode: exitCode,
		Payload:  payload,
	}
}

func runRaw(t *testing.T, bin, dir, actor string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"BEADS_NO_DAEMON=1",
		"BD_ACTOR="+actor,
		"BEADS_ACTOR="+actor,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, string(out))
	}
	return string(out)
}

func parseFirstJSON(t *testing.T, output string) map[string]interface{} {
	t.Helper()
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end < start {
		t.Fatalf("expected JSON object in output, got:\n%s", output)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(output[start:end+1]), &payload); err != nil {
		t.Fatalf("parse JSON payload failed: %v\n%s", err, output)
	}
	return payload
}

func mustIssueID(t *testing.T, payload map[string]interface{}) string {
	t.Helper()
	id, _ := payload["id"].(string)
	if strings.TrimSpace(id) == "" {
		t.Fatalf("missing issue id in payload: %#v", payload)
	}
	return id
}

func mustResult(t *testing.T, got cliResult, want string) {
	t.Helper()
	result, _ := got.Payload["result"].(string)
	if result != want {
		t.Fatalf("result mismatch: got=%q want=%q output=%s", result, want, got.Output)
	}
}

func TestCLIControlPlaneParity(t *testing.T) {
	root := repoRoot(t)
	bin := resolveBDBinary(t, root)
	work := t.TempDir()

	runRaw(t, bin, work, "parity-init", "init", "--prefix", "test", "--quiet")

	createOut := runRaw(
		t,
		bin,
		work,
		"parity-seed",
		"--json",
		"create",
		"Parity issue",
		"--description", "control-plane parity issue",
	)
	issueID := mustIssueID(t, parseFirstJSON(t, createOut))

	claimed := runCLI(t, bin, work, "parity-actor", "--json", "flow", "claim-next", "--limit", "5")
	if claimed.ExitCode != 0 {
		t.Fatalf("expected claimed exit 0, got %d: %s", claimed.ExitCode, claimed.Output)
	}
	mustResult(t, claimed, "claimed")

	wipBlocked := runCLI(t, bin, work, "parity-actor", "--json", "flow", "claim-next", "--limit", "5")
	if wipBlocked.ExitCode != 0 {
		t.Fatalf("expected wip_blocked exit 0, got %d: %s", wipBlocked.ExitCode, wipBlocked.Output)
	}
	mustResult(t, wipBlocked, "wip_blocked")

	runRaw(
		t,
		bin,
		work,
		"parity-actor",
		"close",
		issueID,
		"--reason", "Implemented control-plane parity lifecycle check",
		"--verified", "TestCLIControlPlaneParity claimed-wip",
	)

	noReady := runCLI(t, bin, work, "parity-actor", "--json", "flow", "claim-next", "--limit", "5")
	if noReady.ExitCode != 0 {
		t.Fatalf("expected no_ready exit 0, got %d: %s", noReady.ExitCode, noReady.Output)
	}
	mustResult(t, noReady, "no_ready")

	policyViolation := runCLI(
		t,
		bin,
		work,
		"parity-actor",
		"--json",
		"flow",
		"close-safe",
		"--issue", "bd-missing",
		"--reason", "Done",
		"--verified", "TestCLIControlPlaneParity policy",
	)
	if policyViolation.ExitCode != 3 {
		t.Fatalf("expected policy_violation exit 3, got %d: %s", policyViolation.ExitCode, policyViolation.Output)
	}
	mustResult(t, policyViolation, "policy_violation")

	mainOut := runRaw(
		t,
		bin,
		work,
		"parity-seed",
		"--json",
		"create",
		"Partial state main",
		"--description", "main issue for partial state",
	)
	mainIssueID := mustIssueID(t, parseFirstJSON(t, mainOut))

	partialState := runCLI(
		t,
		bin,
		work,
		"parity-actor",
		"--json",
		"flow",
		"block-with-context",
		"--issue", mainIssueID,
		"--context-pack", "self blocker triggers cycle",
		"--blocker", mainIssueID,
	)
	if partialState.ExitCode != 4 {
		t.Fatalf("expected partial_state exit 4, got %d: %s", partialState.ExitCode, partialState.Output)
	}
	mustResult(t, partialState, "partial_state")

	flowSource, err := os.ReadFile(filepath.Join(root, "cmd", "bd", "flow.go"))
	if err != nil {
		t.Fatalf("read flow.go: %v", err)
	}
	if !strings.Contains(string(flowSource), `Result:  "contention"`) {
		t.Fatalf("flow claim-next must expose contention state in control-plane contract")
	}
}
