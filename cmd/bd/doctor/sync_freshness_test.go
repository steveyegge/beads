package doctor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheckSyncFreshness_DoltNative(t *testing.T) {
	// When sync mode is dolt-native, should return N/A
	// This test requires config.GetSyncMode() to return dolt-native,
	// which depends on Viper config. We test the non-dolt path instead.
	// See TestCheckSyncFreshness_NoIssues for the default path.
}

func TestCheckSyncFreshness_NoIssues(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := CheckSyncFreshness(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q", check.Status, StatusOK)
	}
	if check.Name != "Sync Freshness" {
		t.Errorf("Name = %q, want %q", check.Name, "Sync Freshness")
	}
}

func TestCheckJSONLUncommitted(t *testing.T) {
	t.Run("no JSONL file", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		result := checkJSONLUncommitted(tmpDir, beadsDir)
		if result != "" {
			t.Errorf("Expected empty string, got %q", result)
		}
	})

	t.Run("JSONL uncommitted in git", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Init git repo
		cmd := exec.Command("git", "init", "--initial-branch=main")
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git init failed: %v", err)
		}

		// Configure git user for commits
		for _, args := range [][]string{
			{"config", "user.email", "test@test.com"},
			{"config", "user.name", "Test User"},
		} {
			cmd = exec.Command("git", args...)
			cmd.Dir = tmpDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("git config failed: %v", err)
			}
		}

		// Create .beads dir and JSONL file
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1"}`+"\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Stage but don't commit — file is uncommitted
		result := checkJSONLUncommitted(tmpDir, beadsDir)
		if result != "JSONL file has uncommitted changes" {
			t.Errorf("Expected uncommitted warning, got %q", result)
		}
	})

	t.Run("JSONL committed and clean", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Init git repo
		cmd := exec.Command("git", "init", "--initial-branch=main")
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git init failed: %v", err)
		}

		// Configure git user
		for _, args := range [][]string{
			{"config", "user.email", "test@test.com"},
			{"config", "user.name", "Test User"},
		} {
			cmd = exec.Command("git", args...)
			cmd.Dir = tmpDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("git config failed: %v", err)
			}
		}

		// Create .beads dir and JSONL file
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1"}`+"\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Commit the file
		cmd = exec.Command("git", "add", ".beads/issues.jsonl")
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git add failed: %v", err)
		}
		cmd = exec.Command("git", "commit", "-m", "add JSONL")
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git commit failed: %v", err)
		}

		result := checkJSONLUncommitted(tmpDir, beadsDir)
		if result != "" {
			t.Errorf("Expected empty string for committed file, got %q", result)
		}
	})

	t.Run("not in git repo", func(t *testing.T) {
		tmpDir := t.TempDir()
		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.Mkdir(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1"}`+"\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// No git init — should fail-safe to empty string
		result := checkJSONLUncommitted(tmpDir, beadsDir)
		if result != "" {
			t.Errorf("Expected empty string outside git repo, got %q", result)
		}
	})
}

func TestCheckSyncConflicts(t *testing.T) {
	t.Run("no conflict file", func(t *testing.T) {
		tmpDir := t.TempDir()
		result := checkSyncConflicts(tmpDir)
		if result != "" {
			t.Errorf("Expected empty string, got %q", result)
		}
	})

	t.Run("empty array", func(t *testing.T) {
		tmpDir := t.TempDir()
		conflictPath := filepath.Join(tmpDir, "sync_conflicts.json")
		if err := os.WriteFile(conflictPath, []byte("[]"), 0644); err != nil {
			t.Fatal(err)
		}

		result := checkSyncConflicts(tmpDir)
		if result != "" {
			t.Errorf("Expected empty string for empty conflicts, got %q", result)
		}
	})

	t.Run("array with conflicts", func(t *testing.T) {
		tmpDir := t.TempDir()
		conflicts := []map[string]string{
			{"id": "test-1", "field": "title"},
			{"id": "test-2", "field": "status"},
		}
		data, _ := json.Marshal(conflicts)
		conflictPath := filepath.Join(tmpDir, "sync_conflicts.json")
		if err := os.WriteFile(conflictPath, data, 0644); err != nil {
			t.Fatal(err)
		}

		result := checkSyncConflicts(tmpDir)
		if result != "2 unresolved sync conflict(s)" {
			t.Errorf("Expected '2 unresolved sync conflict(s)', got %q", result)
		}
	})

	t.Run("object with conflicts key", func(t *testing.T) {
		tmpDir := t.TempDir()
		wrapper := map[string]any{
			"conflicts": []map[string]string{
				{"id": "test-1"},
			},
		}
		data, _ := json.Marshal(wrapper)
		conflictPath := filepath.Join(tmpDir, "sync_conflicts.json")
		if err := os.WriteFile(conflictPath, data, 0644); err != nil {
			t.Fatal(err)
		}

		result := checkSyncConflicts(tmpDir)
		if result != "1 unresolved sync conflict(s)" {
			t.Errorf("Expected '1 unresolved sync conflict(s)', got %q", result)
		}
	})

	t.Run("object with empty conflicts", func(t *testing.T) {
		tmpDir := t.TempDir()
		wrapper := map[string]any{
			"conflicts": []any{},
		}
		data, _ := json.Marshal(wrapper)
		conflictPath := filepath.Join(tmpDir, "sync_conflicts.json")
		if err := os.WriteFile(conflictPath, data, 0644); err != nil {
			t.Fatal(err)
		}

		result := checkSyncConflicts(tmpDir)
		if result != "" {
			t.Errorf("Expected empty string for empty conflicts object, got %q", result)
		}
	})
}

func TestCheckSyncFreshness_WithWarnings(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create conflict file with entries
	conflicts := []map[string]string{{"id": "test-1"}}
	data, _ := json.Marshal(conflicts)
	conflictPath := filepath.Join(beadsDir, "sync_conflicts.json")
	if err := os.WriteFile(conflictPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	check := CheckSyncFreshness(tmpDir)

	if check.Status != StatusWarning {
		t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
	}
	if check.Fix == "" {
		t.Error("Expected Fix to contain remediation")
	}
}

func TestCheckDaemonFreshness_NoDaemon(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := CheckDaemonFreshness(tmpDir)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q", check.Status, StatusOK)
	}
	if check.Name != "Daemon Freshness" {
		t.Errorf("Name = %q, want %q", check.Name, "Daemon Freshness")
	}
	if check.Message != "No daemon running (N/A)" {
		t.Errorf("Message = %q, want 'No daemon running (N/A)'", check.Message)
	}
}
