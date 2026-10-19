//go:build cgo

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// graphModeAdmissionFixture is a workspace whose metadata declares a
// graph_mode this build does not open.
type graphModeAdmissionFixture struct {
	name     string
	filename string
	mode     string
	metadata string
	// embedded adds what a real graph workspace holds beside its metadata:
	// the embedded Dolt root of its graph database and the format sentinel
	// graph-preview builds write. Without the guard, an ordinary bd opens
	// that database as an Issue store and migrates it.
	embedded bool
}

// write lays the fixture down under beadsDir and returns the metadata path.
func (f graphModeAdmissionFixture) write(t *testing.T, beadsDir string) string {
	t.Helper()
	path := filepath.Join(beadsDir, f.filename)
	writeFile(t, path, []byte(f.metadata))
	if f.embedded {
		if err := os.MkdirAll(filepath.Join(beadsDir, "embeddeddolt", "beads_graph_0123456789abcdef0123456789abcdef", ".dolt"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(beadsDir, "graph-preview-format"), []byte("link-preview-v9\n"))
	}
	if err := os.Chmod(beadsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func graphModeAdmissionFixtures() []graphModeAdmissionFixture {
	var fixtures []graphModeAdmissionFixture
	fixtures = append(fixtures, graphModeAdmissionFixture{
		name: "metadata.json/link/embedded", filename: "metadata.json", mode: "link",
		metadata: graphLinkWorkspaceMetadata, embedded: true,
	})
	for _, filename := range []string{"metadata.json", "config.json"} {
		for _, mode := range []string{"link", "future-format"} {
			fixtures = append(fixtures, graphModeAdmissionFixture{
				name: filename + "/" + mode + "/server", filename: filename, mode: mode,
				metadata: `{"backend":"dolt","dolt_mode":"server","graph_mode":"` + mode + `"}`,
			})
		}
	}
	return fixtures
}

// runGraphModeAdmissionBD runs bd in repoDir with a private HOME and no
// ambient BEADS_* selection, and returns its combined output and exit code.
func runGraphModeAdmissionBD(t *testing.T, bd, repoDir string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bd, args...)
	cmd.Dir = repoDir
	cmd.Env = append(bdEnv(t.TempDir()),
		"BEADS_DOLT_SERVER_MODE=0",
		"BEADS_DOLT_SHARED_SERVER=0",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("bd %s did not finish promptly: %v\n%s", strings.Join(args, " "), ctx.Err(), output.String())
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return output.String(), 0
	case errors.As(err, &exitErr):
		return output.String(), exitErr.ExitCode()
	default:
		t.Fatalf("bd %s: %v", strings.Join(args, " "), err)
		return "", -1
	}
}

// A refusal in a helper is insufficient: ordinary commands must reach it before
// metadata migration, version tracking, or an attempt to provision/open storage.
func TestGraphModeCLIRefusesLegacyStoreWithoutWorkspaceEffects(t *testing.T) {
	bd := buildBDUnderTest(t)
	commands := [][]string{
		{"show", "beads/plan"},
		{"list", "--json"},
		{"create", "graph fence probe"},
		{"context", "--json"},
		{"where"},
		{"dolt", "start"},
		{"doctor", "--json"},
		{"bootstrap", "--dry-run", "--yes"},
		{"init", "--force", "--quiet", "--non-interactive", "--skip-hooks", "--skip-agents"},
	}
	for _, fixture := range graphModeAdmissionFixtures() {
		for _, args := range commands {
			t.Run(fixture.name+"/"+args[0], func(t *testing.T) {
				repoDir := t.TempDir()
				initGitRepoAt(t, repoDir)
				beadsDir := filepath.Join(repoDir, ".beads")
				metadataPath := fixture.write(t, beadsDir)
				before := legacyUpgradeTreeDigest(t, beadsDir)

				output, code := runGraphModeAdmissionBD(t, bd, repoDir, args...)
				if args[0] == "bootstrap" && fixture.filename == "config.json" {
					// Workspace discovery does not recognize a .beads holding
					// only config.json, and bootstrap, unlike the commands
					// above, does not look further: it reports no workspace.
					// It must still leave the directory alone.
					if code == 0 || !strings.Contains(output, activeWorkspaceNotFoundMessage()) {
						t.Fatalf("bd %s exit = %d, want the no-workspace failure\n%s", strings.Join(args, " "), code, output)
					}
				} else {
					if code != ExitUnsupportedWorkspaceFormat {
						t.Fatalf("bd %s exit = %d, want %d (ExitUnsupportedWorkspaceFormat)\n%s",
							strings.Join(args, " "), code, ExitUnsupportedWorkspaceFormat, output)
					}
					// --json renders the refusal as a JSON error, which escapes
					// the quotes around the mode, so match its parts.
					for _, want := range []string{"graph_mode", fixture.mode, "this bd build does not support"} {
						if !strings.Contains(output, want) {
							t.Fatalf("bd %s refusal does not mention %q:\n%s", strings.Join(args, " "), want, output)
						}
					}
				}
				if after := legacyUpgradeTreeDigest(t, beadsDir); after != before {
					t.Fatalf("refused bd %s changed the workspace\n%s", strings.Join(args, " "), output)
				}
				if got, err := os.ReadFile(metadataPath); err != nil || string(got) != fixture.metadata {
					t.Fatalf("refused bd %s rewrote %s: %q, %v", strings.Join(args, " "), fixture.filename, got, err)
				}
			})
		}
	}
}

// Commands that never open a store keep working in a workspace this build
// refuses, and leave it untouched.
func TestGraphModeWorkspaceAdmitsStorelessCommands(t *testing.T) {
	bd := buildBDUnderTest(t)
	fixture := graphModeAdmissionFixtures()[0]
	for _, args := range [][]string{
		{"version"},
		{"--version"},
		{"help"},
		{"completion", "bash"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			repoDir := t.TempDir()
			initGitRepoAt(t, repoDir)
			beadsDir := filepath.Join(repoDir, ".beads")
			fixture.write(t, beadsDir)
			before := legacyUpgradeTreeDigest(t, beadsDir)

			output, code := runGraphModeAdmissionBD(t, bd, repoDir, args...)
			if code != 0 {
				t.Fatalf("bd %s exit = %d, want 0\n%s", strings.Join(args, " "), code, output)
			}
			if strings.Contains(output, "graph_mode") {
				t.Fatalf("store-less bd %s reported the workspace format:\n%s", strings.Join(args, " "), output)
			}
			if after := legacyUpgradeTreeDigest(t, beadsDir); after != before {
				t.Fatalf("bd %s changed the workspace", strings.Join(args, " "))
			}
		})
	}
}
