package setup

import (
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/templates/agents"
)

func TestContainsBeadsMarkerLegacy(t *testing.T) {
	content := "# Header\n\n<!-- BEGIN BEADS INTEGRATION -->\nContent\n<!-- END BEADS INTEGRATION -->\n"
	if !containsBeadsMarker(content) {
		t.Error("should detect legacy marker")
	}
}

func TestContainsBeadsMarkerNew(t *testing.T) {
	content := "# Header\n\n<!-- BEGIN BEADS INTEGRATION profile:full hash:a1b2c3d4 -->\nContent\n<!-- END BEADS INTEGRATION -->\n"
	if !containsBeadsMarker(content) {
		t.Error("should detect new-format marker")
	}
}

func TestContainsBeadsMarkerAbsent(t *testing.T) {
	content := "# Header\n\nNo beads here\n"
	if containsBeadsMarker(content) {
		t.Error("should not detect marker when absent")
	}
}

func TestUpdateBeadsSectionLegacyToNew(t *testing.T) {
	// Legacy marker should be replaced with new-format marker
	legacy := `# Header

<!-- BEGIN BEADS INTEGRATION -->
Old content here
<!-- END BEADS INTEGRATION -->

# Footer`

	updated := updateBeadsSection(legacy)

	// Should have new-format marker with profile and hash
	if !strings.Contains(updated, "profile:full") {
		t.Error("updated section should use new-format marker with profile:full")
	}
	if !strings.Contains(updated, "hash:") {
		t.Error("updated section should use new-format marker with hash")
	}

	// Should preserve surrounding content
	if !strings.Contains(updated, "# Header") {
		t.Error("should preserve header")
	}
	if !strings.Contains(updated, "# Footer") {
		t.Error("should preserve footer")
	}

	// Old content should be replaced
	if strings.Contains(updated, "Old content here") {
		t.Error("old content should be replaced")
	}
}

func TestUpdateBeadsSectionNewFormatUpdate(t *testing.T) {
	// New-format marker should also be replaceable
	content := `# Header

<!-- BEGIN BEADS INTEGRATION profile:full hash:oldoldhash -->
Stale content
<!-- END BEADS INTEGRATION -->

# Footer`

	updated := updateBeadsSection(content)

	if strings.Contains(updated, "oldoldhash") {
		t.Error("old hash should be replaced")
	}
	if strings.Contains(updated, "Stale content") {
		t.Error("stale content should be replaced")
	}
	if !strings.Contains(updated, "# Header") || !strings.Contains(updated, "# Footer") {
		t.Error("surrounding content should be preserved")
	}
}

func TestRemoveBeadsSectionLegacy(t *testing.T) {
	content := "Header\n<!-- BEGIN BEADS INTEGRATION -->\nContent\n<!-- END BEADS INTEGRATION -->\nFooter"
	result := removeBeadsSection(content)
	if strings.Contains(result, "BEGIN BEADS") {
		t.Error("markers should be removed")
	}
	if !strings.Contains(result, "Header") || !strings.Contains(result, "Footer") {
		t.Error("surrounding content should be preserved")
	}
}

func TestRemoveBeadsSectionNewFormat(t *testing.T) {
	content := "Header\n<!-- BEGIN BEADS INTEGRATION profile:full hash:a1b2c3d4 -->\nContent\n<!-- END BEADS INTEGRATION -->\nFooter"
	result := removeBeadsSection(content)
	if strings.Contains(result, "BEGIN BEADS") {
		t.Error("markers should be removed")
	}
	if !strings.Contains(result, "Header") || !strings.Contains(result, "Footer") {
		t.Error("surrounding content should be preserved")
	}
}

func TestUpdateBeadsSectionWithProfile(t *testing.T) {
	// Test with explicit profile parameter
	content := `# Header

<!-- BEGIN BEADS INTEGRATION -->
Old content
<!-- END BEADS INTEGRATION -->

# Footer`

	updated := updateBeadsSectionWithProfile(content, agents.ProfileMinimal)
	if !strings.Contains(updated, "profile:minimal") {
		t.Error("should use minimal profile")
	}
	if !strings.Contains(updated, "# Header") || !strings.Contains(updated, "# Footer") {
		t.Error("should preserve surrounding content")
	}
}

func TestInstallAgentsWithProfileCreatesNew(t *testing.T) {
	env, _, _ := newFactoryTestEnv(t)
	integration := agentsIntegration{
		name:         "TestAgent",
		setupCommand: "bd setup testagent",
		profile:      agents.ProfileMinimal,
	}
	if err := installAgents(env, integration); err != nil {
		t.Fatalf("installAgents returned error: %v", err)
	}
	data, err := readFileBytes(env.agentsPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "profile:minimal") {
		t.Error("new file should use minimal profile from integration")
	}
}

func TestInstallAgentsDefaultsToFullProfile(t *testing.T) {
	env, _, _ := newFactoryTestEnv(t)
	integration := agentsIntegration{
		name:         "TestAgent",
		setupCommand: "bd setup testagent",
		// no profile set — should default to full
	}
	if err := installAgents(env, integration); err != nil {
		t.Fatalf("installAgents returned error: %v", err)
	}
	data, err := readFileBytes(env.agentsPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "profile:full") {
		t.Error("default profile should be full")
	}
}

// readFileBytes is a test helper to read file content
func readFileBytes(path string) ([]byte, error) {
	return readFileBytesImpl(path)
}
