package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetReposFromYAML_Empty(t *testing.T) {
	// Create temp dir with empty config.yaml
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("# empty config\n"), 0600); err != nil {
		t.Fatal(err)
	}

	repos, err := GetReposFromYAML(configPath)
	if err != nil {
		t.Fatalf("GetReposFromYAML failed: %v", err)
	}

	if repos.Primary != "" {
		t.Errorf("expected empty primary, got %q", repos.Primary)
	}
	if len(repos.Additional) != 0 {
		t.Errorf("expected empty additional, got %v", repos.Additional)
	}
}

func TestGetReposFromYAML_WithRepos(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	config := `repos:
  primary: "."
  additional:
    - ~/beads-planning
    - /path/to/other
`
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}

	repos, err := GetReposFromYAML(configPath)
	if err != nil {
		t.Fatalf("GetReposFromYAML failed: %v", err)
	}

	if repos.Primary != "." {
		t.Errorf("expected primary='.', got %q", repos.Primary)
	}
	if len(repos.Additional) != 2 {
		t.Fatalf("expected 2 additional repos, got %d", len(repos.Additional))
	}
	if repos.Additional[0] != "~/beads-planning" {
		t.Errorf("expected first additional='~/beads-planning', got %q", repos.Additional[0])
	}
	if repos.Additional[1] != "/path/to/other" {
		t.Errorf("expected second additional='/path/to/other', got %q", repos.Additional[1])
	}
}

func TestSetReposInYAML_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	repos := &ReposConfig{
		Primary:    ".",
		Additional: []string{"~/test-repo"},
	}

	if err := SetReposInYAML(configPath, repos); err != nil {
		t.Fatalf("SetReposInYAML failed: %v", err)
	}

	// Verify by reading back
	readRepos, err := GetReposFromYAML(configPath)
	if err != nil {
		t.Fatalf("GetReposFromYAML failed: %v", err)
	}

	if readRepos.Primary != "." {
		t.Errorf("expected primary='.', got %q", readRepos.Primary)
	}
	if len(readRepos.Additional) != 1 || readRepos.Additional[0] != "~/test-repo" {
		t.Errorf("expected additional=['~/test-repo'], got %v", readRepos.Additional)
	}
}

func TestSetReposInYAML_PreservesOtherConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write initial config with other settings
	initial := `issue-prefix: "test"
sync-branch: "beads-sync"
json: false
`
	if err := os.WriteFile(configPath, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}

	// Add repos
	repos := &ReposConfig{
		Primary:    ".",
		Additional: []string{"~/test-repo"},
	}
	if err := SetReposInYAML(configPath, repos); err != nil {
		t.Fatalf("SetReposInYAML failed: %v", err)
	}

	// Verify content still has other settings
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Check that original settings are preserved
	if !contains(content, "issue-prefix") {
		t.Error("issue-prefix setting was lost")
	}
	if !contains(content, "sync-branch") {
		t.Error("sync-branch setting was lost")
	}
	if !contains(content, "json") {
		t.Error("json setting was lost")
	}

	// Check that repos section was added
	if !contains(content, "repos:") {
		t.Error("repos section not found")
	}
}

func TestAddRepo(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("# config\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Add first repo
	if err := AddRepo(configPath, "~/first-repo"); err != nil {
		t.Fatalf("AddRepo failed: %v", err)
	}

	repos, err := GetReposFromYAML(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if repos.Primary != "." {
		t.Errorf("expected primary='.', got %q", repos.Primary)
	}
	if len(repos.Additional) != 1 || repos.Additional[0] != "~/first-repo" {
		t.Errorf("unexpected additional: %v", repos.Additional)
	}

	// Add second repo
	if err := AddRepo(configPath, "/path/to/second"); err != nil {
		t.Fatalf("AddRepo failed: %v", err)
	}

	repos, err = GetReposFromYAML(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos.Additional) != 2 {
		t.Fatalf("expected 2 additional repos, got %d", len(repos.Additional))
	}
}

func TestAddRepo_Duplicate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("# config\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Add repo
	if err := AddRepo(configPath, "~/test-repo"); err != nil {
		t.Fatalf("AddRepo failed: %v", err)
	}

	// Try to add same repo again - should fail
	err := AddRepo(configPath, "~/test-repo")
	if err == nil {
		t.Error("expected error for duplicate repo, got nil")
	}
}

func TestRemoveRepo(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	config := `repos:
  primary: "."
  additional:
    - ~/first
    - ~/second
`
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}

	// Remove first repo
	if err := RemoveRepo(configPath, "~/first"); err != nil {
		t.Fatalf("RemoveRepo failed: %v", err)
	}

	repos, err := GetReposFromYAML(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos.Additional) != 1 || repos.Additional[0] != "~/second" {
		t.Errorf("unexpected additional after remove: %v", repos.Additional)
	}

	// Remove last repo - should clear primary too
	if err := RemoveRepo(configPath, "~/second"); err != nil {
		t.Fatalf("RemoveRepo failed: %v", err)
	}

	repos, err = GetReposFromYAML(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if repos.Primary != "" {
		t.Errorf("expected empty primary after removing all repos, got %q", repos.Primary)
	}
	if len(repos.Additional) != 0 {
		t.Errorf("expected empty additional after removing all repos, got %v", repos.Additional)
	}
}

func TestRemoveRepo_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("# config\n"), 0600); err != nil {
		t.Fatal(err)
	}

	err := RemoveRepo(configPath, "~/nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent repo, got nil")
	}
}

func TestFindConfigYAMLPath(t *testing.T) {
	// Create temp dir with .beads/config.yaml
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(beadsDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("# config\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Change to the temp dir
	oldWd, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Logf("warning: failed to restore working directory: %v", err)
		}
	}()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	found, err := FindConfigYAMLPath()
	if err != nil {
		t.Fatalf("FindConfigYAMLPath failed: %v", err)
	}

	// Verify path ends with .beads/config.yaml
	if filepath.Base(found) != "config.yaml" {
		t.Errorf("expected path ending with config.yaml, got %s", found)
	}
	if filepath.Base(filepath.Dir(found)) != ".beads" {
		t.Errorf("expected path in .beads dir, got %s", found)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr)))
}
