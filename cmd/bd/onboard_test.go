package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestOnboardCommand(t *testing.T) {
	t.Run("onboard output contains key sections", func(t *testing.T) {
		var buf bytes.Buffer
		if err := renderOnboardInstructions(&buf); err != nil {
			t.Fatalf("renderOnboardInstructions() error = %v", err)
		}
		output := buf.String()

		// Verify output contains expected sections
		expectedSections := []string{
			"bd Onboarding",
			"AGENTS.md",
			"BEGIN AGENTS.MD CONTENT",
			"END AGENTS.MD CONTENT",
			"bd prime",
			"How it works",
		}

		for _, section := range expectedSections {
			if !strings.Contains(output, section) {
				t.Errorf("Expected output to contain '%s', but it was missing", section)
			}
		}
	})

	t.Run("agents content is minimal and points to bd prime", func(t *testing.T) {
		// Verify the agentsContent constant is minimal and points to bd prime
		if !strings.Contains(agentsContent, "bd prime") {
			t.Error("agentsContent should point to 'bd prime' for full workflow")
		}
		if !strings.Contains(agentsContent, "bd ready") {
			t.Error("agentsContent should include quick reference to 'bd ready'")
		}
		if !strings.Contains(agentsContent, "bd create") {
			t.Error("agentsContent should include quick reference to 'bd create'")
		}
		if !strings.Contains(agentsContent, "bd close") {
			t.Error("agentsContent should include quick reference to 'bd close'")
		}
		if !strings.Contains(agentsContent, "bd sync") {
			t.Error("agentsContent should include quick reference to 'bd sync'")
		}

		// Verify it includes pre-push quality gates to prevent CI failures
		if !strings.Contains(agentsContent, "Pre-Push Quality Gates") {
			t.Error("agentsContent should include Pre-Push Quality Gates section")
		}
		if !strings.Contains(agentsContent, "golangci-lint run") {
			t.Error("agentsContent should include golangci-lint check")
		}
		if !strings.Contains(agentsContent, "go test") {
			t.Error("agentsContent should include go test check")
		}
		if !strings.Contains(agentsContent, "CRITICAL") {
			t.Error("agentsContent should mark quality gates as CRITICAL")
		}

		// Verify it's actually minimal (less than 1200 chars with quality gates)
		if len(agentsContent) > 1200 {
			t.Errorf("agentsContent should be minimal (<1200 chars), got %d chars", len(agentsContent))
		}
	})

	t.Run("copilot instructions content is minimal", func(t *testing.T) {
		// Verify copilotInstructionsContent is also minimal
		if !strings.Contains(copilotInstructionsContent, "bd prime") {
			t.Error("copilotInstructionsContent should point to 'bd prime'")
		}

		// Verify it includes pre-push quality gates
		if !strings.Contains(copilotInstructionsContent, "Pre-Push Quality Gates") {
			t.Error("copilotInstructionsContent should include Pre-Push Quality Gates section")
		}
		if !strings.Contains(copilotInstructionsContent, "CRITICAL") {
			t.Error("copilotInstructionsContent should mark quality gates as CRITICAL")
		}

		// Verify it's minimal (less than 1200 chars with quality gates)
		if len(copilotInstructionsContent) > 1200 {
			t.Errorf("copilotInstructionsContent should be minimal (<1200 chars), got %d chars", len(copilotInstructionsContent))
		}
	})
}
