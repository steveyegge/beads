package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
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
	if repos.Additional[0].Path != "~/beads-planning" {
		t.Errorf("expected first additional path='~/beads-planning', got %q", repos.Additional[0].Path)
	}
	if repos.Additional[1].Path != "/path/to/other" {
		t.Errorf("expected second additional path='/path/to/other', got %q", repos.Additional[1].Path)
	}
}

func TestSetReposInYAML_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	repos := &ReposConfig{
		Primary:    ".",
		Additional: []AdditionalRepo{{Path: "~/test-repo"}},
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
	if len(readRepos.Additional) != 1 || readRepos.Additional[0].Path != "~/test-repo" {
		t.Errorf("expected additional path='~/test-repo', got %v", readRepos.Additional)
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
		Additional: []AdditionalRepo{{Path: "~/test-repo"}},
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
	if len(repos.Additional) != 1 || repos.Additional[0].Path != "~/first-repo" {
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
	if len(repos.Additional) != 1 || repos.Additional[0].Path != "~/second" {
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

// T016: Backwards compatibility - string array format
func TestGetReposFromYAML_StringArray(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	config := `repos:
  primary: "."
  additional:
    - "oss/"
    - "specs/"
`
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}

	repos, err := GetReposFromYAML(configPath)
	if err != nil {
		t.Fatalf("GetReposFromYAML failed: %v", err)
	}

	if len(repos.Additional) != 2 {
		t.Fatalf("expected 2 additional repos, got %d", len(repos.Additional))
	}

	// Verify string format parsed correctly
	if repos.Additional[0].Path != "oss/" {
		t.Errorf("expected path='oss/', got %q", repos.Additional[0].Path)
	}
	if repos.Additional[0].Prefix != "" {
		t.Errorf("expected empty prefix for string format, got %q", repos.Additional[0].Prefix)
	}
	if len(repos.Additional[0].CustomTypes) != 0 {
		t.Errorf("expected empty custom_types for string format, got %v", repos.Additional[0].CustomTypes)
	}

	// Verify inferred prefix
	if repos.Additional[0].InferredPrefix() != "oss" {
		t.Errorf("expected inferred prefix='oss', got %q", repos.Additional[0].InferredPrefix())
	}
	if repos.Additional[1].InferredPrefix() != "specs" {
		t.Errorf("expected inferred prefix='specs', got %q", repos.Additional[1].InferredPrefix())
	}
}

// T017: New object array format
func TestGetReposFromYAML_ObjectArray(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	config := `repos:
  primary: "."
  additional:
    - path: "specs/"
      prefix: "spec"
    - path: "~/other"
      prefix: "proj"
      custom_types: ["pm", "llm"]
`
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}

	repos, err := GetReposFromYAML(configPath)
	if err != nil {
		t.Fatalf("GetReposFromYAML failed: %v", err)
	}

	if len(repos.Additional) != 2 {
		t.Fatalf("expected 2 additional repos, got %d", len(repos.Additional))
	}

	// First repo: explicit prefix, no custom_types
	if repos.Additional[0].Path != "specs/" {
		t.Errorf("expected path='specs/', got %q", repos.Additional[0].Path)
	}
	if repos.Additional[0].Prefix != "spec" {
		t.Errorf("expected prefix='spec', got %q", repos.Additional[0].Prefix)
	}
	if repos.Additional[0].InferredPrefix() != "spec" {
		t.Errorf("expected inferred prefix='spec', got %q", repos.Additional[0].InferredPrefix())
	}

	// Second repo: explicit prefix and custom_types
	if repos.Additional[1].Path != "~/other" {
		t.Errorf("expected path='~/other', got %q", repos.Additional[1].Path)
	}
	if repos.Additional[1].Prefix != "proj" {
		t.Errorf("expected prefix='proj', got %q", repos.Additional[1].Prefix)
	}
	if len(repos.Additional[1].CustomTypes) != 2 {
		t.Fatalf("expected 2 custom_types, got %d", len(repos.Additional[1].CustomTypes))
	}
	if repos.Additional[1].CustomTypes[0] != "pm" || repos.Additional[1].CustomTypes[1] != "llm" {
		t.Errorf("expected custom_types=['pm','llm'], got %v", repos.Additional[1].CustomTypes)
	}
}

// Mixed format: both string and object in same array
func TestGetReposFromYAML_MixedArray(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	config := `repos:
  primary: "."
  additional:
    - "oss/"
    - path: "specs/"
      prefix: "spec"
`
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}

	repos, err := GetReposFromYAML(configPath)
	if err != nil {
		t.Fatalf("GetReposFromYAML failed: %v", err)
	}

	if len(repos.Additional) != 2 {
		t.Fatalf("expected 2 additional repos, got %d", len(repos.Additional))
	}

	// First: string format
	if repos.Additional[0].Path != "oss/" {
		t.Errorf("expected path='oss/', got %q", repos.Additional[0].Path)
	}
	if repos.Additional[0].Prefix != "" {
		t.Errorf("expected empty explicit prefix, got %q", repos.Additional[0].Prefix)
	}

	// Second: object format
	if repos.Additional[1].Path != "specs/" {
		t.Errorf("expected path='specs/', got %q", repos.Additional[1].Path)
	}
	if repos.Additional[1].Prefix != "spec" {
		t.Errorf("expected prefix='spec', got %q", repos.Additional[1].Prefix)
	}
}

// Test AdditionalRepo custom unmarshaler directly
func TestAdditionalRepo_UnmarshalYAML(t *testing.T) {
	tests := map[string]struct {
		yaml     string
		wantPath string
		wantPfx  string
		wantErr  bool
	}{
		"string": {
			yaml:     `"oss/"`,
			wantPath: "oss/",
			wantPfx:  "",
		},
		"object_path_only": {
			yaml:     `path: "specs/"`,
			wantPath: "specs/",
			wantPfx:  "",
		},
		"object_with_prefix": {
			yaml:     "path: \"specs/\"\nprefix: \"spec\"",
			wantPath: "specs/",
			wantPfx:  "spec",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var repo AdditionalRepo
			err := yaml.Unmarshal([]byte(tc.yaml), &repo)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo.Path != tc.wantPath {
				t.Errorf("path: got %q, want %q", repo.Path, tc.wantPath)
			}
			if repo.Prefix != tc.wantPfx {
				t.Errorf("prefix: got %q, want %q", repo.Prefix, tc.wantPfx)
			}
		})
	}
}

// Test inferPrefixFromPath helper
func TestInferPrefixFromPath(t *testing.T) {
	tests := map[string]struct {
		path string
		want string
	}{
		"with_slash":    {path: "oss/", want: "oss"},
		"without_slash": {path: "oss", want: "oss"},
		"nested":        {path: "path/to/repo/", want: "repo"},
		"absolute":      {path: "/home/user/project", want: "project"},
		"tilde":         {path: "~/beads-planning", want: "beads-planning"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := inferPrefixFromPath(tc.path)
			if got != tc.want {
				t.Errorf("inferPrefixFromPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// Test that simple repos serialize back as strings (backwards compat output)
func TestSetReposInYAML_OutputFormat(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	repos := &ReposConfig{
		Primary: ".",
		Additional: []AdditionalRepo{
			{Path: "oss/"},                                         // Should output as string
			{Path: "specs/", Prefix: "spec"},                       // Should output as object
			{Path: "~/other", Prefix: "proj", CustomTypes: []string{"pm"}}, // Should output as object
		},
	}

	if err := SetReposInYAML(configPath, repos); err != nil {
		t.Fatalf("SetReposInYAML failed: %v", err)
	}

	// Read raw content to verify format
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// String format should just be the path (no "path:" key)
	if !contains(content, `"oss/"`) {
		t.Error("expected string format for simple path")
	}

	// Object format should have "path:" key
	if !contains(content, "path:") {
		t.Error("expected object format with 'path:' key")
	}

	// Verify round-trip
	readRepos, err := GetReposFromYAML(configPath)
	if err != nil {
		t.Fatalf("GetReposFromYAML failed: %v", err)
	}
	if len(readRepos.Additional) != 3 {
		t.Fatalf("expected 3 repos after round-trip, got %d", len(readRepos.Additional))
	}
}
