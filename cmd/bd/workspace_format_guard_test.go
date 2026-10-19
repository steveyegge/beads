//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// graphWorkspaceLayout is a .beads directory whose metadata declares a
// graph_mode, plus whatever else the case lays down beside it.
type graphWorkspaceLayout struct {
	filename string
	metadata string
	// embedded adds the embedded Dolt root a graph database lives in.
	embedded bool
	// doltRoot adds a .beads/dolt root without a version witness, which
	// the legacy classification reads as a pre-1.0 Dolt workspace.
	doltRoot bool
	// sqlite adds a beads.db, which the legacy classification reads as a
	// historical SQLite workspace when nothing else selects Dolt.
	sqlite bool
}

// write lays the layout down under beadsDir and returns the metadata path.
func (l graphWorkspaceLayout) write(t *testing.T, beadsDir string) string {
	t.Helper()
	path := filepath.Join(beadsDir, l.filename)
	writeFile(t, path, []byte(l.metadata))
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
// factories must refuse a graph workspace themselves, before any open.
func TestStoreFactoriesRefuseGraphWorkspace(t *testing.T) {
	pinLegacyUpgradeGuardEnv(t)
	factories := map[string]func(context.Context, string) (storage.DoltStorage, error){
		"read-write": newDoltStoreFromConfig,
		"routed":     newExistingDoltStoreFromConfig,
		"read-only":  newReadOnlyStoreFromConfig,
		"preview":    newPreviewStoreFromConfig,
	}
	for name, open := range factories {
		t.Run(name, func(t *testing.T) {
			beadsDir := t.TempDir()
			path := graphWorkspaceLayout{filename: "metadata.json", metadata: graphLinkWorkspaceMetadata, embedded: true}.write(t, beadsDir)
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
			if got, readErr := os.ReadFile(path); readErr != nil || string(got) != graphLinkWorkspaceMetadata {
				t.Fatalf("%s open rewrote metadata.json: %q, %v", name, got, readErr)
			}
		})
	}
}

func TestHandleWorkspaceGuardErrorExitCodes(t *testing.T) {
	if err := handleWorkspaceGuardError(nil); err != nil {
		t.Fatalf("handleWorkspaceGuardError(nil) = %v", err)
	}
	formatErr := &unsupportedWorkspaceFormatError{beadsDir: "/w/.beads", graphMode: "link"}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "unsupported format", err: formatErr, want: ExitUnsupportedWorkspaceFormat},
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
