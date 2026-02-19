//go:build cgo && integration

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func parseCreatedIssueIDStrictDefaults(t *testing.T, out string) string {
	t.Helper()
	jsonStart := strings.Index(out, "{")
	if jsonStart == -1 {
		t.Fatalf("expected JSON object in output, got: %s", out)
	}

	var issue map[string]interface{}
	if err := json.Unmarshal([]byte(out[jsonStart:]), &issue); err != nil {
		t.Fatalf("failed to parse create JSON output: %v\n%s", err, out)
	}
	id, _ := issue["id"].(string)
	if id == "" {
		t.Fatalf("create output missing issue id: %v", issue)
	}
	return id
}

func setStrictDefaultsActor(t *testing.T, value string) {
	t.Helper()

	oldActor, hadActor := os.LookupEnv("BD_ACTOR")
	oldCompatActor, hadCompatActor := os.LookupEnv("BEADS_ACTOR")
	if err := os.Setenv("BD_ACTOR", value); err != nil {
		t.Fatalf("set BD_ACTOR: %v", err)
	}
	if err := os.Setenv("BEADS_ACTOR", value); err != nil {
		t.Fatalf("set BEADS_ACTOR: %v", err)
	}
	t.Cleanup(func() {
		if hadActor {
			_ = os.Setenv("BD_ACTOR", oldActor)
		} else {
			_ = os.Unsetenv("BD_ACTOR")
		}
		if hadCompatActor {
			_ = os.Setenv("BEADS_ACTOR", oldCompatActor)
		} else {
			_ = os.Unsetenv("BEADS_ACTOR")
		}
	})
}

func strictDefaultsExecEnv(actor string) []string {
	return append(
		os.Environ(),
		"BEADS_NO_DAEMON=1",
		"BD_ACTOR="+actor,
		"BEADS_ACTOR="+actor,
	)
}

func TestCLI_UpdateClaimWIPGate_StrictDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping strict-defaults claim test in short mode")
	}

	tmpDir := setupCLITestDB(t)
	const testActor = "strict-defaults-claim"
	setStrictDefaultsActor(t, testActor)

	firstID := parseCreatedIssueIDStrictDefaults(t, runBDInProcess(t, tmpDir, "create", "First claim target", "-p", "1", "--json"))
	secondID := parseCreatedIssueIDStrictDefaults(t, runBDInProcess(t, tmpDir, "create", "Second claim target", "-p", "1", "--json"))

	runBDInProcess(t, tmpDir, "update", firstID, "--claim")

	out, err := runBDExecAllowErrorWithEnv(t, tmpDir, strictDefaultsExecEnv(testActor), "update", secondID, "--claim")
	if err == nil {
		t.Fatalf("expected WIP gate to reject second claim; output: %s", out)
	}
	if !strings.Contains(out, "WIP=1 gate blocked claim") {
		t.Fatalf("expected WIP gate message, got: %s", out)
	}

	showOut := runBDInProcess(t, tmpDir, "show", secondID, "--json")
	var issues []map[string]interface{}
	if err := json.Unmarshal([]byte(showOut), &issues); err != nil {
		t.Fatalf("parse show output: %v\n%s", err, showOut)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one issue from show, got %d", len(issues))
	}
	if got, _ := issues[0]["status"].(string); got != "open" {
		t.Fatalf("expected second issue to remain open, got status=%q", got)
	}
}

func TestCLI_UpdateClaimWIPGate_OptOut(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping strict-defaults claim opt-out test in short mode")
	}

	tmpDir := setupCLITestDB(t)
	const testActor = "strict-defaults-claim-optout"
	setStrictDefaultsActor(t, testActor)

	firstID := parseCreatedIssueIDStrictDefaults(t, runBDInProcess(t, tmpDir, "create", "First claim target", "-p", "1", "--json"))
	secondID := parseCreatedIssueIDStrictDefaults(t, runBDInProcess(t, tmpDir, "create", "Second claim target", "-p", "1", "--json"))

	runBDInProcess(t, tmpDir, "update", firstID, "--claim")

	out, err := runBDExecAllowErrorWithEnv(
		t,
		tmpDir,
		strictDefaultsExecEnv(testActor),
		"update",
		secondID,
		"--claim",
		"--allow-multi-wip",
	)
	if err != nil {
		t.Fatalf("expected claim opt-out to succeed: %v\n%s", err, out)
	}

	showOut := runBDInProcess(t, tmpDir, "show", secondID, "--json")
	var issues []map[string]interface{}
	if err := json.Unmarshal([]byte(showOut), &issues); err != nil {
		t.Fatalf("parse show output: %v\n%s", err, showOut)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one issue from show, got %d", len(issues))
	}
	if got, _ := issues[0]["status"].(string); got != "in_progress" {
		t.Fatalf("expected second issue to be in_progress with opt-out, got status=%q", got)
	}
}

func TestCLI_Close_StrictDefaultsAndOptOut(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping strict-defaults close test in short mode")
	}

	tmpDir := setupCLITestDB(t)
	const testActor = "strict-defaults-close"
	setStrictDefaultsActor(t, testActor)

	issueID := parseCreatedIssueIDStrictDefaults(t, runBDInProcess(t, tmpDir, "create", "Close policy target", "-p", "1", "--json"))

	out, err := runBDExecAllowErrorWithEnv(t, tmpDir, strictDefaultsExecEnv(testActor), "close", issueID, "--reason", "Done")
	if err == nil {
		t.Fatalf("expected unsafe close reason to fail without opt-out; output: %s", out)
	}
	if !strings.Contains(out, "close reason policy violation") {
		t.Fatalf("expected close reason policy error, got: %s", out)
	}

	out, err = runBDExecAllowErrorWithEnv(
		t,
		tmpDir,
		strictDefaultsExecEnv(testActor),
		"close",
		issueID,
		"--reason",
		"Implemented strict close policy",
	)
	if err == nil {
		t.Fatalf("expected missing --verified evidence to fail; output: %s", out)
	}
	if !strings.Contains(out, "at least one --verified entry is required") {
		t.Fatalf("expected missing verification error, got: %s", out)
	}

	runBDInProcess(t, tmpDir, "close", issueID, "--reason", "Implemented strict close policy", "--verified", "TestCLI_Close_StrictDefaultsAndOptOut")
	showOut := runBDInProcess(t, tmpDir, "show", issueID, "--json")
	var issues []map[string]interface{}
	if err := json.Unmarshal([]byte(showOut), &issues); err != nil {
		t.Fatalf("parse show output: %v\n%s", err, showOut)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one issue from show, got %d", len(issues))
	}
	if got, _ := issues[0]["status"].(string); got != "closed" {
		t.Fatalf("expected issue closed, got status=%q", got)
	}
	notes, _ := issues[0]["notes"].(string)
	if !strings.Contains(notes, "Verified: TestCLI_Close_StrictDefaultsAndOptOut") {
		t.Fatalf("expected verification evidence in notes, got: %q", notes)
	}

	optOutID := parseCreatedIssueIDStrictDefaults(t, runBDInProcess(t, tmpDir, "create", "Close policy opt-out", "-p", "1", "--json"))
	runBDInProcess(
		t,
		tmpDir,
		"close",
		optOutID,
		"--reason",
		"Done",
		"--allow-unsafe-reason",
		"--allow-missing-verified",
	)
	optOutShow := runBDInProcess(t, tmpDir, "show", optOutID, "--json")
	var optOutIssues []map[string]interface{}
	if err := json.Unmarshal([]byte(optOutShow), &optOutIssues); err != nil {
		t.Fatalf("parse opt-out show output: %v\n%s", err, optOutShow)
	}
	if len(optOutIssues) != 1 {
		t.Fatalf("expected one opt-out issue from show, got %d", len(optOutIssues))
	}
	if got, _ := optOutIssues[0]["status"].(string); got != "closed" {
		t.Fatalf("expected opt-out issue closed, got status=%q", got)
	}
}
