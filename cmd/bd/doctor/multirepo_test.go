package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTypesFromYAML_FlowSequence(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "types:\n  custom: [bug-report, feature-request, hotfix]\n"
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	types, err := readTypesFromYAML(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"bug-report", "feature-request", "hotfix"}
	if len(types) != len(want) {
		t.Fatalf("got %d types, want %d", len(types), len(want))
	}
	for i, got := range types {
		if got != want[i] {
			t.Errorf("types[%d] = %q, want %q", i, got, want[i])
		}
	}
}

func TestReadTypesFromYAML_BlockSequence(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "types:\n  custom:\n    - mail\n    - molecule\n    - wisp\n"
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	types, err := readTypesFromYAML(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"mail", "molecule", "wisp"}
	if len(types) != len(want) {
		t.Fatalf("got %d types, want %d", len(types), len(want))
	}
	for i, got := range types {
		if got != want[i] {
			t.Errorf("types[%d] = %q, want %q", i, got, want[i])
		}
	}
}

func TestReadTypesFromYAML_Empty(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "types:\n  custom: []\n"
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	types, err := readTypesFromYAML(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if types != nil {
		t.Errorf("expected nil, got %v", types)
	}
}

func TestReadTypesFromYAML_NoTypesSection(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "backend: dolt\nsync-branch: main\n"
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	types, err := readTypesFromYAML(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if types != nil {
		t.Errorf("expected nil, got %v", types)
	}
}

func TestReadTypesFromYAML_MissingFile(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := readTypesFromYAML(beadsDir)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadTypesFromYAML_OtherKeysIgnored(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "backend: dolt\ntypes:\n  custom:\n    - mr\n  builtin:\n    - bug\nother: value\n"
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	types, err := readTypesFromYAML(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(types) != 1 || types[0] != "mr" {
		t.Errorf("got %v, want [mr]", types)
	}
}
