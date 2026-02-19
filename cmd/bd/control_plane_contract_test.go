package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func findRepoRootForContractTest(t *testing.T) string {
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
			t.Fatalf("could not locate repo root from %q", wd)
		}
		dir = parent
	}
}

func TestControlPlaneContract(t *testing.T) {
	root := findRepoRootForContractTest(t)
	contractPath := filepath.Join(root, "docs", "CONTROL_PLANE_CONTRACT.md")

	data, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read contract artifact: %v", err)
	}
	contract := string(data)

	requiredDocTokens := []string{
		"# Control-Plane Contract (v1)",
		"## Command Map",
		"`flow claim-next`",
		"`flow create-discovered`",
		"`flow block-with-context`",
		"`flow close-safe`",
		"`flow transition`",
		"`intake audit`",
		"`resume`",
		"`land`",
		"`reason lint`",
		"## JSON Envelope",
		"\"ok\"",
		"\"command\"",
		"\"result\"",
		"\"issue_id\"",
		"\"details\"",
		"\"recovery_command\"",
		"\"events\"",
		"## Exit-Code Policy",
		"`3`: `policy_violation`",
		"`4`: `partial_state`",
	}

	for _, token := range requiredDocTokens {
		if !strings.Contains(contract, token) {
			t.Fatalf("contract is missing required token %q", token)
		}
	}

	envelopeType := reflect.TypeOf(commandEnvelope{})
	requiredFields := []string{"OK", "Command", "Result", "IssueID", "Details", "RecoveryCommand", "Events"}
	for _, field := range requiredFields {
		if _, ok := envelopeType.FieldByName(field); !ok {
			t.Fatalf("commandEnvelope missing field %q", field)
		}
	}

	if exitCodePolicyViolation != 3 {
		t.Fatalf("exitCodePolicyViolation = %d, want 3", exitCodePolicyViolation)
	}
	if exitCodePartialState != 4 {
		t.Fatalf("exitCodePartialState = %d, want 4", exitCodePartialState)
	}
}
