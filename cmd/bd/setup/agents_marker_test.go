package setup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
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

// TestAgentsSectionRemoveReaddIsStable pins what --remove and a later setup
// leave behind: removing the section leaves no blank-line hole, setup appends
// it again after one blank line, and from then on each --remove and each setup
// leaves the same bytes, however often they alternate. A file the user wrote
// gets back what it held before setup added the section.
func TestAgentsSectionRemoveReaddIsStable(t *testing.T) {
	stubDetectRenderOpts(t)
	integration := agentsIntegration{
		name:         "TestAgent",
		setupCommand: "bd setup testagent",
		profile:      agents.ProfileMinimal,
	}
	section := agents.RenderSection(agents.ProfileMinimal)
	scaffold := createNewAgentsFileWithProfile(agents.ProfileMinimal)

	tests := []struct {
		name        string
		seed        string // "" means setup creates the file
		wantRemoved string
	}{
		{
			name:        "file setup created",
			wantRemoved: strings.Replace(scaffold, section+"\n\n", "", 1),
		},
		{
			name:        "user file",
			seed:        "# My Project\n\nHouse rules live here.\n",
			wantRemoved: "# My Project\n\nHouse rules live here.\n",
		},
		{
			name:        "user file without a final newline",
			seed:        "# My Project",
			wantRemoved: "# My Project\n",
		},
		{
			name:        "user file ending in blank lines",
			seed:        "# My Project\n\n\n",
			wantRemoved: "# My Project\n",
		},
		{
			name:        "CRLF user file",
			seed:        "# My Project\r\n\r\nHouse rules live here.\r\n",
			wantRemoved: "# My Project\r\n\r\nHouse rules live here.\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, _, _ := newFactoryTestEnv(t)
			if tt.seed != "" {
				if err := os.WriteFile(env.agentsPath, []byte(tt.seed), 0o644); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			step := func(name string, run func(agentsEnv, agentsIntegration) error) string {
				t.Helper()
				if err := run(env, integration); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				data, err := os.ReadFile(env.agentsPath)
				if err != nil {
					t.Fatalf("read after %s: %v", name, err)
				}
				return string(data)
			}

			step("setup", installAgents)
			removed := step("remove", removeAgents)
			if removed != tt.wantRemoved {
				t.Fatalf("after remove:\ngot:  %q\nwant: %q", removed, tt.wantRemoved)
			}
			readded := step("setup again", installAgents)
			if want := removed + "\n" + section; readded != want {
				t.Fatalf("after setup again:\ngot:  %q\nwant: %q", readded, want)
			}
			for cycle := 2; cycle <= 3; cycle++ {
				if got := step("remove", removeAgents); got != removed {
					t.Fatalf("remove #%d changed the result:\ngot:  %q\nwant: %q", cycle, got, removed)
				}
				if got := step("setup", installAgents); got != readded {
					t.Fatalf("setup #%d changed the result:\ngot:  %q\nwant: %q", cycle+1, got, readded)
				}
			}
		})
	}
}

// readFileBytes is a test helper to read file content
func readFileBytes(path string) ([]byte, error) {
	return readFileBytesImpl(path)
}

func TestExistingBeadsProfileLegacy(t *testing.T) {
	content := "# Header\n<!-- BEGIN BEADS INTEGRATION -->\nContent\n<!-- END BEADS INTEGRATION -->\n"
	got := existingBeadsProfile(content)
	if got != agents.ProfileFull {
		t.Errorf("legacy marker should return ProfileFull, got %q", got)
	}
}

func TestExistingBeadsProfileFull(t *testing.T) {
	content := "<!-- BEGIN BEADS INTEGRATION profile:full hash:abcd1234 -->\nContent\n<!-- END BEADS INTEGRATION -->\n"
	got := existingBeadsProfile(content)
	if got != agents.ProfileFull {
		t.Errorf("expected ProfileFull, got %q", got)
	}
}

func TestExistingBeadsProfileMinimal(t *testing.T) {
	content := "<!-- BEGIN BEADS INTEGRATION profile:minimal hash:deadbeef -->\nContent\n<!-- END BEADS INTEGRATION -->\n"
	got := existingBeadsProfile(content)
	if got != agents.ProfileMinimal {
		t.Errorf("expected ProfileMinimal, got %q", got)
	}
}

func TestExistingBeadsProfileNoMarker(t *testing.T) {
	content := "# Just a file\nNo markers\n"
	got := existingBeadsProfile(content)
	if got != agents.ProfileFull {
		t.Errorf("no marker should default to ProfileFull, got %q", got)
	}
}

func TestCheckAgentsDetectsStale(t *testing.T) {
	env, stdout, _ := newFactoryTestEnv(t)
	// Write a section with a bogus hash so it's stale
	content := "<!-- BEGIN BEADS INTEGRATION profile:full hash:00000000 -->\nOld content\n<!-- END BEADS INTEGRATION -->\n"
	if err := os.WriteFile(env.agentsPath, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	integration := agentsIntegration{
		name:         "TestAgent",
		setupCommand: "bd setup testagent",
		profile:      agents.ProfileFull,
	}
	err := checkAgents(env, integration)
	if !errors.Is(err, errBeadsSectionStale) {
		t.Fatalf("expected errBeadsSectionStale, got %v", err)
	}
	if !strings.Contains(stdout.String(), "stale") {
		t.Error("expected stale message in stdout")
	}
}

func TestCheckAgentsCurrent(t *testing.T) {
	stubDetectRenderOpts(t)

	env, stdout, _ := newFactoryTestEnv(t)
	section := agents.RenderSection(agents.ProfileFull)
	if err := os.WriteFile(env.agentsPath, []byte(section), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	integration := agentsIntegration{
		name:         "TestAgent",
		setupCommand: "bd setup testagent",
		profile:      agents.ProfileFull,
	}
	if err := checkAgents(env, integration); err != nil {
		t.Fatalf("expected nil error for current section, got %v", err)
	}
	if !strings.Contains(stdout.String(), "current") {
		t.Error("expected (current) in output")
	}
}

func TestCheckAgentsMinimalAcceptsFullProfile(t *testing.T) {
	stubDetectRenderOpts(t)

	env, _, _ := newFactoryTestEnv(t)
	section := agents.RenderSection(agents.ProfileFull)
	if err := os.WriteFile(env.agentsPath, []byte(section), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	integration := agentsIntegration{
		name:         "ClaudeCode",
		setupCommand: "bd setup claude",
		profile:      agents.ProfileMinimal,
	}
	if err := checkAgents(env, integration); err != nil {
		t.Fatalf("expected full profile to be accepted for minimal integration, got %v", err)
	}
}

func TestCheckAgentsMissingUsesTargetFileName(t *testing.T) {
	stdout := &bytes.Buffer{}
	env := agentsEnv{
		agentsPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		stdout:     stdout,
		stderr:     &bytes.Buffer{},
	}
	integration := agentsIntegration{name: "ClaudeCode", setupCommand: "bd setup claude", profile: agents.ProfileMinimal}
	err := checkAgents(env, integration)
	if !errors.Is(err, errAgentsFileMissing) {
		t.Fatalf("expected errAgentsFileMissing, got %v", err)
	}
	if !strings.Contains(stdout.String(), "CLAUDE.md not found") {
		t.Fatalf("expected target filename in output, got: %s", stdout.String())
	}
}

func TestInstallAgentsPreservesFullProfile(t *testing.T) {
	// Simulate: file already has full profile, requesting minimal install
	env, stdout, _ := newFactoryTestEnv(t)
	fullSection := agents.RenderSection(agents.ProfileFull)
	if err := os.WriteFile(env.agentsPath, []byte(fullSection), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	integration := agentsIntegration{
		name:         "MinimalAgent",
		setupCommand: "bd setup minimalagent",
		profile:      agents.ProfileMinimal,
	}
	if err := installAgents(env, integration); err != nil {
		t.Fatalf("installAgents: %v", err)
	}
	data, err := readFileBytes(env.agentsPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	// Should preserve full profile, not downgrade to minimal
	if !strings.Contains(content, "profile:full") {
		t.Error("should preserve full profile when minimal requested on file with full")
	}
	if !strings.Contains(stdout.String(), "preserving") {
		t.Error("expected informational message about preserving full profile")
	}
}

func TestInstallAgentsSymlinkSafety(t *testing.T) {
	dir := t.TempDir()
	realFile := filepath.Join(dir, "AGENTS.md")
	linkPath := filepath.Join(dir, "CLAUDE.md")

	// Seed the target with content that should remain unchanged.
	original := "# Existing target content\n"
	if err := os.WriteFile(realFile, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Create symlink
	if err := os.Symlink(realFile, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	env := agentsEnv{
		agentsPath: linkPath, // install targets the symlink
		stdout:     stdout,
		stderr:     stderr,
	}
	integration := agentsIntegration{
		name:         "ClaudeCode",
		setupCommand: "bd setup claude",
		profile:      agents.ProfileMinimal,
	}
	if err := installAgents(env, integration); err != nil {
		t.Fatalf("installAgents via symlink: %v", err)
	}

	// Symlink should still be a symlink.
	if info, err := os.Lstat(linkPath); err != nil {
		t.Fatalf("lstat symlink: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected CLAUDE.md to remain a symlink")
	}

	// Read the real file — should be untouched (no managed section injection).
	data, err := os.ReadFile(realFile)
	if err != nil {
		t.Fatalf("read real file: %v", err)
	}
	if string(data) != original {
		t.Errorf("symlink target changed:\n got: %q\nwant: %q", string(data), original)
	}
	if strings.Contains(string(data), "BEGIN BEADS INTEGRATION") {
		t.Error("symlink target should not receive managed section")
	}
	if stdout.String() != "Installing ClaudeCode integration...\n" {
		t.Errorf("expected stdout to contain only install start, got: %q", stdout.String())
	}
	stderrText := stderr.String()
	if !strings.Contains(stderrText, "CLAUDE.md is a symlink to "+realFile) {
		t.Errorf("expected warning to include symlink target, got: %s", stderrText)
	}
	if !strings.Contains(stderrText, "re-run 'bd setup claude'") {
		t.Errorf("expected warning to include corrective command, got: %s", stderrText)
	}
}

func TestInstallAgentsInspectError(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "not-dir")
	if err := os.WriteFile(notDir, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("write sentinel file: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	env := agentsEnv{
		agentsPath: filepath.Join(notDir, "AGENTS.md"),
		stdout:     stdout,
		stderr:     stderr,
	}
	integration := agentsIntegration{
		name:         "TestAgent",
		setupCommand: "bd setup testagent",
		profile:      agents.ProfileMinimal,
	}

	err := installAgents(env, integration)
	if err == nil {
		t.Fatal("expected inspect error")
	}
	if os.IsNotExist(err) {
		t.Fatalf("expected non-not-exist error, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "Error: failed to inspect") {
		t.Errorf("expected inspect error on stderr, got: %s", stderr.String())
	}
}

func TestLegacyToNewMigrationViaInstall(t *testing.T) {
	env, _, _ := newFactoryTestEnv(t)
	// Seed with legacy markers
	legacy := "# Header\n\n<!-- BEGIN BEADS INTEGRATION -->\nOld content\n<!-- END BEADS INTEGRATION -->\n\n# Footer"
	if err := os.WriteFile(env.agentsPath, []byte(legacy), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	integration := agentsIntegration{
		name:         "Factory.ai",
		setupCommand: "bd setup factory",
		profile:      agents.ProfileFull,
	}
	if err := installAgents(env, integration); err != nil {
		t.Fatalf("installAgents: %v", err)
	}
	data, err := readFileBytes(env.agentsPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	// Should now have versioned markers
	if !strings.Contains(content, "profile:full") {
		t.Error("legacy markers should be upgraded to versioned format")
	}
	if !strings.Contains(content, "hash:") {
		t.Error("upgraded section should contain hash")
	}
	if strings.Contains(content, "Old content") {
		t.Error("old legacy content should be replaced")
	}
	if !strings.Contains(content, "# Header") || !strings.Contains(content, "# Footer") {
		t.Error("surrounding content should be preserved")
	}
}
