package main

import (
	"strings"
	"testing"
)

func TestClaimPathEnforcesSingleWIP(t *testing.T) {
	if updateCmd.Flags().Lookup("allow-multi-wip") == nil {
		t.Fatalf("update command missing --allow-multi-wip bypass flag")
	}

	if !strings.Contains(flowClaimNextCmd.Short, "WIP=1") {
		t.Fatalf("flow claim-next should advertise WIP=1 gate, got %q", flowClaimNextCmd.Short)
	}
}

func TestCloseSafePolicyEnforcement(t *testing.T) {
	if err := lintCloseReason("Implemented deterministic close checks; verified with go test", false); err != nil {
		t.Fatalf("expected safe success reason to pass, got: %v", err)
	}

	if err := lintCloseReason("Fixed error handling path", false); err == nil {
		t.Fatalf("expected unsafe keyword in success reason to fail")
	}

	if err := lintCloseReason("failed: integration did not pass gate", false); err == nil {
		t.Fatalf("expected failed: reason to require --allow-failure-reason")
	}

	if err := lintCloseReason("failed: integration did not pass gate", true); err != nil {
		t.Fatalf("expected failed: reason with allow flag to pass, got: %v", err)
	}
}

func TestIntakeAuditContract(t *testing.T) {
	audit := intakeCmd.Commands()
	found := false
	for _, c := range audit {
		if c.Name() == "audit" {
			found = true
			if c.Flags().Lookup("epic") == nil {
				t.Fatalf("intake audit missing --epic flag")
			}
			if c.Flags().Lookup("write-proof") == nil {
				t.Fatalf("intake audit missing --write-proof flag")
			}
		}
	}
	if !found {
		t.Fatalf("intake command missing audit subcommand")
	}
}

func TestResumeDeterministicOutput(t *testing.T) {
	if !strings.Contains(strings.ToLower(resumeCmd.Short), "deterministic") {
		t.Fatalf("resume command should advertise deterministic output, got %q", resumeCmd.Short)
	}
	if resumeCmd.Flags().Lookup("epic") == nil {
		t.Fatalf("resume command missing --epic flag")
	}
}

func TestLandDeterministicGates(t *testing.T) {
	if landCmd.Flags().Lookup("epic") == nil {
		t.Fatalf("land command missing --epic flag")
	}
	if landCmd.Flags().Lookup("check-only") == nil {
		t.Fatalf("land command missing --check-only flag")
	}
	if landCmd.Flags().Lookup("sync") == nil {
		t.Fatalf("land command missing --sync flag")
	}
	if landCmd.Flags().Lookup("push") == nil {
		t.Fatalf("land command missing --push flag")
	}
}

func TestReasonLintCommand(t *testing.T) {
	seenLint := false
	for _, c := range reasonCmd.Commands() {
		if c.Name() == "lint" {
			seenLint = true
			if c.Flags().Lookup("reason") == nil {
				t.Fatalf("reason lint missing --reason flag")
			}
			if c.Flags().Lookup("allow-failure-reason") == nil {
				t.Fatalf("reason lint missing --allow-failure-reason flag")
			}
		}
	}
	if !seenLint {
		t.Fatalf("reason command missing lint subcommand")
	}
}

func TestStrictDefaultsRequireExplicitOptOut(t *testing.T) {
	if updateCmd.Flags().Lookup("allow-multi-wip") == nil {
		t.Fatalf("update command missing explicit WIP opt-out flag")
	}

	if closeCmd.Flags().Lookup("allow-unsafe-reason") == nil {
		t.Fatalf("close command missing explicit unsafe-reason opt-out flag")
	}
	if closeCmd.Flags().Lookup("allow-missing-verified") == nil {
		t.Fatalf("close command missing explicit missing-verified opt-out flag")
	}
	if closeCmd.Flags().Lookup("allow-failure-reason") == nil {
		t.Fatalf("close command missing explicit failure-reason opt-out flag")
	}

	// Strict defaults should stay strict on flow wrappers: no broad bypass flags.
	if flowClaimNextCmd.Flags().Lookup("allow-multi-wip") != nil {
		t.Fatalf("flow claim-next should not expose allow-multi-wip bypass flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("allow-unsafe-reason") != nil {
		t.Fatalf("flow close-safe should not expose allow-unsafe-reason bypass flag")
	}
	if flowCloseSafeCmd.Flags().Lookup("allow-missing-verified") != nil {
		t.Fatalf("flow close-safe should not expose allow-missing-verified bypass flag")
	}

	// Limited, auditable exceptional path is still allowed for failure close reasons.
	if flowCloseSafeCmd.Flags().Lookup("allow-failure-reason") == nil {
		t.Fatalf("flow close-safe missing allow-failure-reason flag")
	}
}
