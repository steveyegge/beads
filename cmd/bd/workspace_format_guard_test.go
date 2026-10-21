//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
)

// graphLinkWorkspaceMetadata is the metadata.json a graph_mode link workspace
// carries: the marker, its binding, and an ordinary embedded Dolt backend.
const graphLinkWorkspaceMetadata = `{
  "database": "",
  "backend": "dolt",
  "graph_mode": "link",
  "graph_scope_url": "https://example.test/scope",
  "graph_authority_id": "0123456789abcdef0123456789abcdef",
  "graph_workspace": "/work/repo/.beads",
  "graph_schema_version": 7,
  "graph_ready": true,
  "dolt_mode": "embedded",
  "dolt_database": "beads_graph_0123456789abcdef0123456789abcdef",
  "project_id": "0123456789abcdef0123456789abcdef"
}`

// graphStrippedWorkspaceMetadata is graphLinkWorkspaceMetadata as a bd that
// predates graph_mode leaves it after any Save: the graph_* keys are gone, and
// the database name still selects the graph database.
const graphStrippedWorkspaceMetadata = `{
  "database": "",
  "backend": "dolt",
  "dolt_mode": "embedded",
  "dolt_database": "beads_graph_0123456789abcdef0123456789abcdef",
  "project_id": "0123456789abcdef0123456789abcdef"
}`

// graphWorkspaceLayout is a .beads directory whose metadata declares a
// graph_mode, plus whatever else the case lays down beside it.
type graphWorkspaceLayout struct {
	// filename is the metadata file; "" lays down no metadata.
	filename string
	metadata string
	// marker adds the graph-preview-format sentinel graph-preview builds
	// write before metadata.json.
	marker bool
	// embedded adds the embedded Dolt root a graph database lives in.
	embedded bool
	// doltRoot adds a .beads/dolt root without a version witness, which
	// the legacy classification reads as a pre-1.0 Dolt workspace.
	doltRoot bool
	// sqlite adds a beads.db, which the legacy classification reads as a
	// historical SQLite workspace when nothing else selects Dolt.
	sqlite bool
}

// write lays the layout down under beadsDir and returns the metadata path, or
// "" when the layout has no metadata.
func (l graphWorkspaceLayout) write(t *testing.T, beadsDir string) string {
	t.Helper()
	path := ""
	if l.filename != "" {
		path = filepath.Join(beadsDir, l.filename)
		writeFile(t, path, []byte(l.metadata))
	}
	if l.marker {
		writeFile(t, filepath.Join(beadsDir, "graph-preview-format"), []byte("link-preview-v9\n"))
	}
	if l.embedded {
		if err := os.MkdirAll(filepath.Join(beadsDir, "embeddeddolt", "beads_graph_0123456789abcdef0123456789abcdef", ".dolt"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if l.doltRoot {
		if err := os.Mkdir(filepath.Join(beadsDir, "dolt"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if l.sqlite {
		writeFile(t, filepath.Join(beadsDir, "beads.db"), []byte("SQLite format 3\x00"))
	}
	return path
}

func TestWorkspaceFormatGuardRefusesGraphWorkspaces(t *testing.T) {
	pinLegacyUpgradeGuardEnv(t)
	tests := []struct {
		name   string
		layout graphWorkspaceLayout
		mode   string
	}{
		{name: "link embedded", mode: "link",
			layout: graphWorkspaceLayout{filename: "metadata.json", metadata: graphLinkWorkspaceMetadata, embedded: true}},
		// The legacy classification admits this shape: a server workspace
		// with no local Dolt root.
		{name: "link server", mode: "link",
			layout: graphWorkspaceLayout{filename: "metadata.json", metadata: `{"backend":"dolt","dolt_mode":"server","graph_mode":"link"}`}},
		{name: "link legacy config.json", mode: "link",
			layout: graphWorkspaceLayout{filename: "config.json", metadata: graphLinkWorkspaceMetadata, embedded: true}},
		{name: "unknown marker", mode: "future-format",
			layout: graphWorkspaceLayout{filename: "metadata.json", metadata: `{"backend":"dolt","graph_mode":"future-format"}`, embedded: true}},
		{name: "marker is case-sensitive", mode: "Dependency",
			layout: graphWorkspaceLayout{filename: "metadata.json", metadata: `{"backend":"dolt","graph_mode":"Dependency"}`, embedded: true}},
		// Without the format check first, these two shapes are reported as
		// legacy workspaces to migrate.
		{name: "link over legacy dolt root", mode: "link",
			layout: graphWorkspaceLayout{filename: "metadata.json", metadata: `{"backend":"dolt","graph_mode":"link"}`, doltRoot: true}},
		{name: "link beside historical sqlite", mode: "link",
			layout: graphWorkspaceLayout{filename: "metadata.json", metadata: `{"graph_mode":"link"}`, sqlite: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beadsDir := t.TempDir()
			path := tt.layout.write(t, beadsDir)
			before := legacyUpgradeTreeDigest(t, beadsDir)

			err := guardLegacyUpgradeWorkspace(beadsDir)
			if !isUnsupportedWorkspaceFormat(err) {
				t.Fatalf("guardLegacyUpgradeWorkspace() = %v, want unsupported workspace format refusal", err)
			}
			for _, want := range []string{beadsDir, `graph_mode "` + tt.mode + `"`, "this bd build does not support", "no storage database was opened or modified"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal %q does not mention %q", err, want)
				}
			}
			if after := legacyUpgradeTreeDigest(t, beadsDir); after != before {
				t.Fatalf("guard changed the workspace")
			}
			if got, readErr := os.ReadFile(path); readErr != nil || string(got) != tt.layout.metadata {
				t.Fatalf("guard rewrote %s: %q, %v", tt.layout.filename, got, readErr)
			}
		})
	}
}

// A graph workspace whose metadata does not declare graph_mode is still
// refused by its format marker: graph-preview builds write the marker before
// metadata.json, and a bd that predates graph_mode drops the field on Save.
func TestWorkspaceFormatGuardRefusesFormatMarker(t *testing.T) {
	pinLegacyUpgradeGuardEnv(t)
	tests := []struct {
		name   string
		layout graphWorkspaceLayout
	}{
		// Graph init interrupted after the marker, before metadata.json.
		{name: "marker only", layout: graphWorkspaceLayout{marker: true}},
		{name: "metadata lost", layout: graphWorkspaceLayout{marker: true, embedded: true}},
		{name: "metadata stripped", layout: graphWorkspaceLayout{filename: "metadata.json", metadata: graphStrippedWorkspaceMetadata, marker: true, embedded: true}},
		{name: "legacy config.json stripped", layout: graphWorkspaceLayout{filename: "config.json", metadata: graphStrippedWorkspaceMetadata, marker: true, embedded: true}},
		{name: "server metadata stripped", layout: graphWorkspaceLayout{filename: "metadata.json", metadata: `{"backend":"dolt","dolt_mode":"server"}`, marker: true}},
		{name: "explicit dependency", layout: graphWorkspaceLayout{filename: "metadata.json", metadata: `{"backend":"dolt","graph_mode":"dependency"}`, marker: true, embedded: true}},
		// Metadata that does not parse (a conflicted merge, a truncated
		// write) cannot outrank the marker.
		{name: "unparseable metadata", layout: graphWorkspaceLayout{filename: "metadata.json", metadata: `{"backend":"dolt",`, marker: true, embedded: true}},
		// Without the format check first, this is reported as a legacy
		// Dolt workspace to migrate.
		{name: "marker over legacy dolt root", layout: graphWorkspaceLayout{marker: true, doltRoot: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beadsDir := t.TempDir()
			path := tt.layout.write(t, beadsDir)
			before := legacyUpgradeTreeDigest(t, beadsDir)

			err := guardLegacyUpgradeWorkspace(beadsDir)
			if !isUnsupportedWorkspaceFormat(err) {
				t.Fatalf("guardLegacyUpgradeWorkspace() = %v, want unsupported workspace format refusal", err)
			}
			for _, want := range []string{beadsDir, "graph-preview-format", "this bd build does not support", "no storage database was opened or modified"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal %q does not mention %q", err, want)
				}
			}
			if after := legacyUpgradeTreeDigest(t, beadsDir); after != before {
				t.Fatalf("guard changed the workspace")
			}
			if path != "" {
				if got, readErr := os.ReadFile(path); readErr != nil || string(got) != tt.layout.metadata {
					t.Fatalf("guard rewrote %s: %q, %v", tt.layout.filename, got, readErr)
				}
			}
		})
	}
	// The marker's presence is the signal, not its content.
	for _, content := range []string{"", "link-preview-v5\n", "not a generation"} {
		t.Run("content "+strconv.Quote(content), func(t *testing.T) {
			beadsDir := t.TempDir()
			writeFile(t, filepath.Join(beadsDir, "graph-preview-format"), []byte(content))
			if err := guardLegacyUpgradeWorkspace(beadsDir); !isUnsupportedWorkspaceFormat(err) {
				t.Fatalf("guardLegacyUpgradeWorkspace() = %v, want unsupported workspace format refusal", err)
			}
		})
	}
}

// With no workspace directory there is no marker to look for: an empty
// beadsDir must not probe the working directory.
func TestWorkspaceFormatCheckWithoutWorkspaceDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "graph-preview-format", []byte("link-preview-v9\n"))
	if err := checkWorkspaceFormat(nil, ""); err != nil {
		t.Fatalf("checkWorkspaceFormat(nil, \"\") = %v, want nil", err)
	}
	if err := validateConfiguredBackend(&configfile.Config{Backend: configfile.BackendDolt}, ""); err != nil {
		t.Fatalf("validateConfiguredBackend(dolt, \"\") = %v, want nil", err)
	}
}

// Bootstrap in a workspace with no metadata of its own takes its nearest
// ancestor's. An ancestor declared a graph workspace only by its format marker
// is refused like one declaring graph_mode: neither skipped for a more distant
// workspace or the defaults, nor reported as a metadata load failure.
func TestFindParentConfigRefusesMarkedAncestor(t *testing.T) {
	pinLegacyUpgradeGuardEnv(t)
	for name, layout := range map[string]graphWorkspaceLayout{
		"marker only":                  {marker: true},
		"marker, metadata unparseable": {filename: "metadata.json", metadata: `{"backend":`, marker: true, embedded: true},
		"marker, metadata stripped":    {filename: "metadata.json", metadata: graphStrippedWorkspaceMetadata, marker: true, embedded: true},
	} {
		t.Run(name, func(t *testing.T) {
			outer := t.TempDir()
			// An ordinary workspace further up must not be selected instead.
			writeFile(t, filepath.Join(outer, ".beads", "metadata.json"), []byte(`{"backend":"dolt","dolt_database":"outer"}`))
			graphDir := filepath.Join(outer, "graph")
			graphBeadsDir := filepath.Join(graphDir, ".beads")
			layout.write(t, graphBeadsDir)
			before := legacyUpgradeTreeDigest(t, graphBeadsDir)
			t.Setenv("HOME", outer)

			cfg, err := findParentConfig(filepath.Join(graphDir, "inner", ".beads"))
			if !isUnsupportedWorkspaceFormat(err) {
				t.Fatalf("findParentConfig() = %+v, %v, want unsupported workspace format refusal", cfg, err)
			}
			if !strings.Contains(err.Error(), "graph-preview-format") {
				t.Fatalf("refusal %q does not name the marker", err)
			}
			if after := legacyUpgradeTreeDigest(t, graphBeadsDir); after != before {
				t.Fatal("findParentConfig changed the graph workspace")
			}
		})
	}
}

func TestWorkspaceFormatGuardAdmitsDependencyWorkspaces(t *testing.T) {
	pinLegacyUpgradeGuardEnv(t)
	for _, metadata := range []string{
		`{"backend":"dolt"}`,
		`{"backend":"dolt","graph_mode":""}`,
		`{"backend":"dolt","graph_mode":"dependency"}`,
	} {
		t.Run(metadata, func(t *testing.T) {
			beadsDir := t.TempDir()
			writeFile(t, filepath.Join(beadsDir, "metadata.json"), []byte(metadata))
			if err := guardLegacyUpgradeWorkspace(beadsDir); err != nil {
				t.Fatalf("guardLegacyUpgradeWorkspace() = %v, want admission", err)
			}
		})
	}
	if err := guardLegacyUpgradeWorkspace(t.TempDir()); err != nil {
		t.Fatalf("guardLegacyUpgradeWorkspace(no metadata) = %v, want admission", err)
	}
}

// Routed writes, cross-repository reads and create --repo open another
// workspace's store without passing through command admission, so the store
// factories must refuse a graph workspace themselves, before any open. That
// includes one marked only by a legacy config.json, which configfile.Load
// would migrate into metadata.json before a check of the loaded metadata ran,
// and one declared only by the graph format marker, whose metadata is
// stripped, lost or unparseable.
func TestStoreFactoriesRefuseGraphWorkspace(t *testing.T) {
	pinLegacyUpgradeGuardEnv(t)
	factories := map[string]func(context.Context, string) (storage.DoltStorage, error){
		"read-write": newDoltStoreFromConfig,
		"routed":     newExistingDoltStoreFromConfig,
		"read-only":  newReadOnlyStoreFromConfig,
		"preview":    newPreviewStoreFromConfig,
	}
	layouts := map[string]graphWorkspaceLayout{
		"metadata.json graph_mode link": {filename: "metadata.json", metadata: graphLinkWorkspaceMetadata, embedded: true},
		"config.json graph_mode link":   {filename: "config.json", metadata: graphLinkWorkspaceMetadata, embedded: true},
		"marker, metadata stripped":     {filename: "metadata.json", metadata: graphStrippedWorkspaceMetadata, marker: true, embedded: true},
		// No metadata at all: the factories must not fall back to the
		// default database before looking for the marker.
		"marker, metadata lost": {marker: true, embedded: true},
		// Metadata that does not parse beside the marker: the marker alone
		// declares the format, so this is refused as that, not reported as
		// a metadata load failure.
		"marker, metadata unparseable": {filename: "metadata.json", metadata: `{"backend":`, marker: true, embedded: true},
	}
	for layoutName, layout := range layouts {
		for name, open := range factories {
			t.Run(layoutName+"/"+name, func(t *testing.T) {
				beadsDir := t.TempDir()
				path := layout.write(t, beadsDir)
				before := legacyUpgradeTreeDigest(t, beadsDir)
				store, err := open(t.Context(), beadsDir)
				if err == nil {
					_ = store.Close()
				}
				if !isUnsupportedWorkspaceFormat(err) {
					t.Fatalf("%s open = %v, want unsupported workspace format refusal", name, err)
				}
				if after := legacyUpgradeTreeDigest(t, beadsDir); after != before {
					t.Fatalf("%s open changed the workspace", name)
				}
				if path != "" {
					if got, readErr := os.ReadFile(path); readErr != nil || string(got) != layout.metadata {
						t.Fatalf("%s open rewrote %s: %q, %v", name, layout.filename, got, readErr)
					}
				}
			})
		}
	}
}

// Error framing and the bd dolt diagnostics read a workspace's metadata outside
// the store factories: framing names the backend of an open that may have just
// been refused, and bd dolt show and status, which admission guards already,
// load and validate it again. None of them may migrate a graph workspace's
// legacy config.json into metadata.json, and the diagnostics refuse it the way
// admission does.
func TestWorkspaceMetadataReadersLeaveGraphWorkspaceUnchanged(t *testing.T) {
	pinLegacyUpgradeGuardEnv(t)
	requireFormatExit := func(t *testing.T, name string, run func() error) {
		t.Helper()
		var err error
		captureStderr(t, func() { err = run() })
		if code, ok := exitCodeFromError(err); !ok || code != ExitUnsupportedWorkspaceFormat {
			t.Fatalf("%s exit code = %d (%v), want %d", name, code, ok, ExitUnsupportedWorkspaceFormat)
		}
	}
	readers := map[string]func(t *testing.T, beadsDir string){
		"error framing": func(t *testing.T, beadsDir string) {
			if got := backendNameForErrorFraming(beadsDir); got != configfile.BackendDolt {
				t.Fatalf("backendNameForErrorFraming = %q, want %q", got, configfile.BackendDolt)
			}
		},
		"dolt backend config": func(t *testing.T, beadsDir string) {
			if _, err := loadDoltBackendConfig(beadsDir); !isUnsupportedWorkspaceFormat(err) {
				t.Fatalf("loadDoltBackendConfig = %v, want unsupported workspace format refusal", err)
			}
		},
		"dolt show": func(t *testing.T, _ string) {
			requireFormatExit(t, "bd dolt show", func() error { return showDoltConfig(false) })
		},
		"dolt status": func(t *testing.T, _ string) {
			requireFormatExit(t, "bd dolt status", func() error { return doltStatusCmd.RunE(doltStatusCmd, nil) })
		},
	}
	for _, filename := range []string{"metadata.json", "config.json"} {
		for name, read := range readers {
			t.Run(filename+"/"+name, func(t *testing.T) {
				beadsDir := t.TempDir()
				// The embedded root lets workspace discovery select a
				// workspace marked only by config.json.
				path := graphWorkspaceLayout{filename: filename, metadata: graphLinkWorkspaceMetadata, embedded: true}.write(t, beadsDir)
				t.Setenv("BEADS_DIR", beadsDir)
				before := legacyUpgradeTreeDigest(t, beadsDir)
				read(t, beadsDir)
				if after := legacyUpgradeTreeDigest(t, beadsDir); after != before {
					t.Fatalf("%s changed the workspace", name)
				}
				if got, err := os.ReadFile(path); err != nil || string(got) != graphLinkWorkspaceMetadata {
					t.Fatalf("%s rewrote %s: %q, %v", name, filename, got, err)
				}
			})
		}
	}
}

func TestHandleWorkspaceGuardErrorExitCodes(t *testing.T) {
	if err := handleWorkspaceGuardError(nil); err != nil {
		t.Fatalf("handleWorkspaceGuardError(nil) = %v", err)
	}
	formatErr := &configfile.UnsupportedWorkspaceFormatError{BeadsDir: "/w/.beads", GraphMode: "link"}
	markerDir := t.TempDir()
	writeFile(t, filepath.Join(markerDir, "graph-preview-format"), []byte("link-preview-v9\n"))
	markerErr := checkWorkspaceFormat(nil, markerDir)
	if markerErr == nil {
		t.Fatalf("checkWorkspaceFormat(no metadata, format marker) = nil, want a refusal")
	}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "unsupported format", err: formatErr, want: ExitUnsupportedWorkspaceFormat},
		{name: "format marker", err: markerErr, want: ExitUnsupportedWorkspaceFormat},
		{name: "legacy refusal", err: legacyUpgradeRefusal("legacy Dolt workspace"), want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got error
			stderr := captureStderr(t, func() { got = handleWorkspaceGuardError(tt.err) })
			if code, ok := exitCodeFromError(got); !ok || code != tt.want {
				t.Fatalf("exit code = %d (%v), want %d", code, ok, tt.want)
			}
			if !strings.Contains(stderr, "Error: "+tt.err.Error()) {
				t.Fatalf("stderr does not carry the refusal:\n%s", stderr)
			}
		})
	}
	if ExitUnsupportedWorkspaceFormat != 16 {
		t.Fatalf("ExitUnsupportedWorkspaceFormat = %d; it is a stable scripting contract", ExitUnsupportedWorkspaceFormat)
	}
}

// A create target without metadata.json is refused when it declares a graph
// format, and is otherwise left to the auto-initialization path as before,
// even when its legacy config.json cannot be parsed.
func TestEnsureBeadsDirForPathChecksWorkspaceFormat(t *testing.T) {
	malformedConfig := `{"backend":"dolt",`
	for _, tc := range []struct {
		name       string
		marker     bool
		wantFormat bool
	}{
		{name: "malformed config.json", marker: false, wantFormat: false},
		{name: "malformed config.json beside the marker", marker: true, wantFormat: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targetDir := t.TempDir()
			beadsDir := filepath.Join(targetDir, ".beads")
			if err := os.MkdirAll(beadsDir, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(beadsDir, "config.json"), []byte(malformedConfig), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.marker {
				if err := os.WriteFile(filepath.Join(beadsDir, "graph-preview-format"), []byte("1\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// allowCreate=false stops just after the format check, before
			// anything is created, so the result shows which branch ran.
			err := ensureBeadsDirForPath(context.Background(), targetDir, nil, false)
			if err == nil {
				t.Fatal("ensureBeadsDirForPath() = nil, want an error")
			}
			if got := isUnsupportedWorkspaceFormat(err); got != tc.wantFormat {
				t.Fatalf("ensureBeadsDirForPath() = %v, unsupported format = %v, want %v", err, got, tc.wantFormat)
			}
			if !tc.wantFormat && !strings.Contains(err.Error(), "no beads workspace found") {
				t.Fatalf("ensureBeadsDirForPath() = %v, want the not-auto-created refusal", err)
			}
		})
	}
}
