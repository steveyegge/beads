package deprecation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheck_CleanConfig(t *testing.T) {
	dir := t.TempDir()
	writeMetadata(t, dir, `{"database": "dolt"}`)

	warnings := Check(dir)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for clean config, got %d: %v", len(warnings), warnings)
	}
}

func TestCheck_ExplicitEmbeddedMode(t *testing.T) {
	dir := t.TempDir()
	writeMetadata(t, dir, `{"dolt_mode": "embedded"}`)

	warnings := Check(dir)
	if !hasWarning(warnings, "embedded-mode") {
		t.Errorf("expected embedded-mode warning, got %v", ids(warnings))
	}
}

func TestCheck_MissingDoltMode_NoWarning(t *testing.T) {
	dir := t.TempDir()
	writeMetadata(t, dir, `{"database": "dolt"}`)

	warnings := Check(dir)
	if hasWarning(warnings, "embedded-mode") {
		t.Error("missing dolt_mode should NOT produce warning (defaults to server)")
	}
}

func TestCheck_SQLiteBackend(t *testing.T) {
	dir := t.TempDir()
	writeMetadata(t, dir, `{"backend": "sqlite"}`)

	warnings := Check(dir)
	if !hasWarning(warnings, "sqlite-backend") {
		t.Errorf("expected sqlite-backend warning, got %v", ids(warnings))
	}
}

func TestCheck_DoltBackend_NoWarning(t *testing.T) {
	dir := t.TempDir()
	writeMetadata(t, dir, `{"backend": "dolt"}`)

	warnings := Check(dir)
	if hasWarning(warnings, "sqlite-backend") {
		t.Error("dolt backend should NOT produce sqlite-backend warning")
	}
}

func TestCheck_SQLiteArtifacts(t *testing.T) {
	dir := t.TempDir()
	writeMetadata(t, dir, `{}`)
	if err := os.WriteFile(filepath.Join(dir, "beads.db"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	warnings := Check(dir)
	if !hasWarning(warnings, "sqlite-artifacts") {
		t.Errorf("expected sqlite-artifacts warning, got %v", ids(warnings))
	}
}

func TestCheck_ServerModeEnv(t *testing.T) {
	dir := t.TempDir()
	writeMetadata(t, dir, `{}`)
	t.Setenv("BEADS_DOLT_SERVER_MODE", "1")

	warnings := Check(dir)
	if !hasWarning(warnings, "server-mode-env") {
		t.Errorf("expected server-mode-env warning, got %v", ids(warnings))
	}
}

func TestCheck_JSONLSyncFiles(t *testing.T) {
	dir := t.TempDir()
	writeMetadata(t, dir, `{}`)
	if err := os.WriteFile(filepath.Join(dir, "issues.jsonl"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	warnings := Check(dir)
	if !hasWarning(warnings, "jsonl-sync-files") {
		t.Errorf("expected jsonl-sync-files warning, got %v", ids(warnings))
	}
}

func TestCheck_NonExistentDir(t *testing.T) {
	warnings := Check("/nonexistent/path/that/does/not/exist")
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for nonexistent dir, got %d", len(warnings))
	}
}

func TestCheck_EmptyDir(t *testing.T) {
	warnings := Check("")
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for empty dir, got %d", len(warnings))
	}
}

func TestPrintWarnings_Suppressed_JSON(t *testing.T) {
	warnings := []Warning{{ID: "test", Summary: "test"}}
	if PrintWarnings(warnings, true) {
		t.Error("expected false when jsonMode=true")
	}
}

func TestPrintWarnings_Empty(t *testing.T) {
	if PrintWarnings(nil, false) {
		t.Error("expected false for nil warnings")
	}
}

// helpers

func writeMetadata(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func hasWarning(warnings []Warning, id string) bool {
	for _, w := range warnings {
		if w.ID == id {
			return true
		}
	}
	return false
}

func ids(warnings []Warning) []string {
	var out []string
	for _, w := range warnings {
		out = append(out, w.ID)
	}
	return out
}
