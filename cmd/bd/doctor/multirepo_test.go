package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
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

// snapshotBeadsDir returns every entry under beadsDir, keyed by its
// slash-separated relative path: a file maps to its bytes, a directory to "/".
func snapshotBeadsDir(t *testing.T, beadsDir string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(beadsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(beadsDir, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			snapshot[filepath.ToSlash(rel)] = "/"
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G304 -- test-owned temp tree
		if err != nil {
			return err
		}
		snapshot[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// TestDiscoverChildTypesLeavesGraphWorkspaceUnchanged pins that bd doctor's
// multi-repo type discovery refuses an additional repo in a format this build
// cannot open before anything migrates its metadata. Command admission only
// checks the workspace doctor runs in, never the repos listed under
// repos.additional, so the migrating configfile.Load in readTypesFromDB used
// to turn a graph workspace's legacy config.json into metadata.json (dropping
// graph_* keys this build does not know) before the Dolt constructor refused
// it. Each layout runs with a .beads/dolt directory, so that discovery would
// reach that constructor, and without one, so that discovery's own refusal is
// what the test sees; each carries a config.yaml whose types the YAML
// fallback still reports.
func TestDiscoverChildTypesLeavesGraphWorkspaceUnchanged(t *testing.T) {
	// If the refusal were missing, the open must fail fast rather than reach
	// the suite's shared test server.
	t.Setenv("BEADS_DOLT_SERVER_HOST", "127.0.0.1")
	t.Setenv("BEADS_DOLT_SERVER_PORT", "1")
	const graphMetadata = `{"backend":"dolt","dolt_mode":"server","dolt_database":"beads_graph_0123","graph_mode":"%s","graph_future_binding":"keep-me"}`
	const strippedMetadata = `{"backend":"dolt","dolt_mode":"server","dolt_database":"beads_graph_0123"}`
	layouts := []struct {
		name      string
		files     map[string]string
		graphMode string // the refused graph_mode; "" for the marker refusal
	}{
		{name: "graph_mode in legacy config.json", files: map[string]string{"config.json": fmt.Sprintf(graphMetadata, configfile.GraphModeLink)}, graphMode: configfile.GraphModeLink},
		{name: "future graph_mode in legacy config.json", files: map[string]string{"config.json": fmt.Sprintf(graphMetadata, "future-format")}, graphMode: "future-format"},
		{name: "marker beside legacy config.json", files: map[string]string{"config.json": strippedMetadata, configfile.GraphFormatMarkerFile: "link-preview-v9\n"}},
		{name: "graph_mode in metadata.json", files: map[string]string{configfile.ConfigFileName: fmt.Sprintf(graphMetadata, configfile.GraphModeLink)}, graphMode: configfile.GraphModeLink},
		{name: "marker beside stripped metadata.json", files: map[string]string{configfile.ConfigFileName: strippedMetadata, configfile.GraphFormatMarkerFile: "link-preview-v9\n"}},
	}
	for _, layout := range layouts {
		for _, doltRoot := range []bool{true, false} {
			subtest := layout.name + "/no dolt root"
			if doltRoot {
				subtest = layout.name + "/dolt root"
			}
			t.Run(subtest, func(t *testing.T) {
				repo := t.TempDir()
				beadsDir := filepath.Join(repo, ".beads")
				if err := os.MkdirAll(beadsDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if doltRoot {
					if err := os.Mkdir(filepath.Join(beadsDir, "dolt"), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				files := map[string]string{"config.yaml": "types:\n  custom: [graph-node]\n"}
				for name, content := range layout.files {
					files[name] = content
				}
				for name, content := range files {
					if err := os.WriteFile(filepath.Join(beadsDir, name), []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				want := snapshotBeadsDir(t, beadsDir)

				_, err := readTypesFromDB(beadsDir)
				var formatErr *configfile.UnsupportedWorkspaceFormatError
				if !errors.As(err, &formatErr) || formatErr.BeadsDir != beadsDir ||
					formatErr.GraphMode != layout.graphMode || formatErr.FormatMarker != (layout.graphMode == "") {
					t.Fatalf("readTypesFromDB error = %v, want UnsupportedWorkspaceFormatError for %s", err, beadsDir)
				}
				if got := snapshotBeadsDir(t, beadsDir); !reflect.DeepEqual(got, want) {
					t.Fatalf("readTypesFromDB changed the workspace:\n got %q\nwant %q", got, want)
				}

				if got := discoverChildTypes(repo); !reflect.DeepEqual(got, []string{"graph-node"}) {
					t.Fatalf("discoverChildTypes = %q, want the config.yaml fallback [graph-node]", got)
				}
				if got := snapshotBeadsDir(t, beadsDir); !reflect.DeepEqual(got, want) {
					t.Fatalf("discoverChildTypes changed the workspace:\n got %q\nwant %q", got, want)
				}
			})
		}
	}
}

// TestDiscoverChildTypesLeavesLegacyConfigUnopenedUnchanged pins that type
// discovery reads an additional repo's legacy config.json without migrating
// it: only the store open itself may migrate it, so a child with no Dolt
// database for discovery to open keeps its config.json.
func TestDiscoverChildTypesLeavesLegacyConfigUnopenedUnchanged(t *testing.T) {
	repo := t.TempDir()
	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.json": `{"database":"beads.db","backend":"dolt","dolt_mode":"server","dolt_database":"beads_legacy_0123"}`,
		"config.yaml": "types:\n  custom: [legacy-node]\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(beadsDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want := snapshotBeadsDir(t, beadsDir)

	if _, err := readTypesFromDB(beadsDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("readTypesFromDB error = %v, want the missing Dolt database", err)
	}
	if got := discoverChildTypes(repo); !reflect.DeepEqual(got, []string{"legacy-node"}) {
		t.Fatalf("discoverChildTypes = %q, want the config.yaml fallback [legacy-node]", got)
	}
	if got := snapshotBeadsDir(t, beadsDir); !reflect.DeepEqual(got, want) {
		t.Fatalf("type discovery changed the workspace:\n got %q\nwant %q", got, want)
	}
}
