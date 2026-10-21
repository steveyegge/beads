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

func stubMuxEnvProvider(t *testing.T, env agentsEnv) {
	t.Helper()
	orig := muxEnvProvider
	muxEnvProvider = func() agentsEnv {
		return env
	}
	t.Cleanup(func() { muxEnvProvider = orig })
}

// stubMuxHome points muxUserHomeDir at a new, empty home directory and
// returns it.
func stubMuxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	orig := muxUserHomeDir
	muxUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { muxUserHomeDir = orig })
	return home
}

// muxUserLevelSection is the managed section a global install writes to
// ~/.mux/AGENTS.md.
func muxUserLevelSection() string {
	return agents.RenderSectionWithOpts(agents.ProfileFull, agents.UserLevelRenderOpts())
}

func readMuxFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func writeMuxFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestInstallMuxCreatesNewFile(t *testing.T) {
	env, stdout, _ := newFactoryTestEnv(t)
	if err := installMux(env, false, false); err != nil {
		t.Fatalf("installMux returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Mux integration installed") {
		t.Error("expected Mux install success message")
	}
	if !strings.Contains(stdout.String(), muxAgentInstructionsURL) {
		t.Error("expected Mux docs URL in install output")
	}
}

func TestInstallMuxReportsSkippedSymlinkRootInstructions(t *testing.T) {
	env, stdout, stderr := newFactoryTestEnv(t)
	target := filepath.Join(filepath.Dir(env.agentsPath), "SHARED_AGENTS.md")
	if err := os.WriteFile(target, []byte("# Shared instructions\n"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, env.agentsPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := installMux(env, false, false); err != nil {
		t.Fatalf("installMux returned error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Mux hooks installed; managed section skipped") {
		t.Fatalf("expected partial Mux success message, got:\n%s", out)
	}
	if strings.Contains(out, "Mux integration installed") {
		t.Fatalf("should not report full Mux integration success when root instructions are skipped:\n%s", out)
	}
	if !strings.Contains(stderr.String(), "AGENTS.md is a symlink") {
		t.Fatalf("expected symlink warning on stderr, got:\n%s", stderr.String())
	}
	if !FileExists(muxProjectHookPathsFirst(env.agentsPath)) {
		t.Fatalf("expected Mux init hook to be installed")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if strings.Contains(string(data), "BEGIN BEADS INTEGRATION") {
		t.Fatalf("symlink target should remain untouched:\n%s", data)
	}
}

func muxProjectHookPathsFirst(baseAgentsPath string) string {
	initPath, _, _ := muxProjectHookPaths(baseAgentsPath)
	return initPath
}

func TestCheckMuxMissingFile(t *testing.T) {
	env, stdout, _ := newFactoryTestEnv(t)
	err := checkMux(env, false, false)
	if err == nil {
		t.Fatal("expected error for missing AGENTS.md")
	}
	if !strings.Contains(stdout.String(), "bd setup mux") {
		t.Error("expected setup guidance for mux")
	}
}

func TestMuxProjectAgentsPath(t *testing.T) {
	if got, want := muxProjectAgentsPath("AGENTS.md"), filepath.Join(".mux", "AGENTS.md"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	agentsPath := filepath.Join(t.TempDir(), "AGENTS.md")
	if got, want := muxProjectAgentsPath(agentsPath), filepath.Join(filepath.Dir(agentsPath), ".mux", "AGENTS.md"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMuxProjectHookPaths(t *testing.T) {
	initPath, toolPostPath, toolEnvPath := muxProjectHookPaths("AGENTS.md")
	if want := filepath.Join(".mux", "init"); initPath != want {
		t.Fatalf("init path = %q, want %q", initPath, want)
	}
	if want := filepath.Join(".mux", "tool_post"); toolPostPath != want {
		t.Fatalf("tool_post path = %q, want %q", toolPostPath, want)
	}
	if want := filepath.Join(".mux", "tool_env"); toolEnvPath != want {
		t.Fatalf("tool_env path = %q, want %q", toolEnvPath, want)
	}
}

func TestInstallMuxProjectInstallsBothLayers(t *testing.T) {
	stubDetectRenderOpts(t)
	stubMuxHome(t)
	env, _, _ := newFactoryTestEnv(t)
	if err := installMux(env, true, false); err != nil {
		t.Fatalf("installMux(project=true) returned error: %v", err)
	}
	// Both layers belong to the project, so both get its rendering.
	want := createNewAgentsFileWithOpts(agents.ProfileFull, agents.DefaultRenderOpts())
	for _, path := range []string{env.agentsPath, muxProjectAgentsPath(env.agentsPath)} {
		if got := readMuxFile(t, path); got != want {
			t.Errorf("%s should get the project rendering:\ngot:\n%s\nwant:\n%s", path, got, want)
		}
	}
}

func TestCheckMuxProjectRequiresBothLayers(t *testing.T) {
	env, _, _ := newFactoryTestEnv(t)
	if err := installMux(env, false, false); err != nil {
		t.Fatalf("installMux(project=false) returned error: %v", err)
	}
	if err := checkMux(env, true, false); err == nil {
		t.Fatal("expected project check to fail when .mux/AGENTS.md is missing")
	}
}

func TestRemoveMuxProjectRemovesBothLayers(t *testing.T) {
	env, _, _ := newFactoryTestEnv(t)
	if err := installMux(env, true, false); err != nil {
		t.Fatalf("installMux(project=true) returned error: %v", err)
	}
	if err := removeMux(env, true, false); err != nil {
		t.Fatalf("removeMux(project=true) returned error: %v", err)
	}

	for _, path := range []string{env.agentsPath, muxProjectAgentsPath(env.agentsPath)} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s to remain readable after remove: %v", path, err)
		}
		content := string(data)
		if strings.Contains(content, agentsBeginMarker) || strings.Contains(content, agentsEndMarker) {
			t.Fatalf("expected beads markers removed from %s", path)
		}
	}

	if err := checkMux(env, true, false); err == nil {
		t.Fatal("expected project check to fail after remove")
	}
}

func TestMuxGlobalAgentsPath(t *testing.T) {
	t.Cleanup(func() {
		muxUserHomeDir = os.UserHomeDir
	})
	home := t.TempDir()
	muxUserHomeDir = func() (string, error) {
		return home, nil
	}

	got, err := muxGlobalAgentsPath()
	if err != nil {
		t.Fatalf("muxGlobalAgentsPath returned error: %v", err)
	}
	if want := filepath.Join(home, ".mux", "AGENTS.md"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Mux loads ~/.mux/AGENTS.md in every workspace, so a global install writes
// it with just the managed section, worded for repositories that use beads,
// and leaves the root AGENTS.md with the project's rendering.
func TestInstallMuxGlobalInstallsGlobalLayer(t *testing.T) {
	stubDetectRenderOpts(t)
	env, _, _ := newFactoryTestEnv(t)
	home := stubMuxHome(t)

	if err := installMux(env, false, true); err != nil {
		t.Fatalf("installMux(global=true) returned error: %v", err)
	}

	globalPath := filepath.Join(home, ".mux", "AGENTS.md")
	if got, want := readMuxFile(t, globalPath), muxUserLevelSection(); got != want {
		t.Errorf("global AGENTS.md should hold only the user-level section:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if got, want := readMuxFile(t, env.agentsPath), createNewAgentsFileWithOpts(agents.ProfileFull, agents.DefaultRenderOpts()); got != want {
		t.Errorf("root AGENTS.md should keep the project rendering:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if err := checkMux(env, false, true); err != nil {
		t.Fatalf("checkMux(global=true) returned error: %v", err)
	}
}

// ~/.mux/AGENTS.md is shared by every project, so a global install renders
// and checks it the same way whatever beads config the current directory has:
// otherwise --check --global from one project calls the section another
// project wrote stale, and following its hint flips it back.
func TestMuxGlobalSectionIgnoresProjectConfig(t *testing.T) {
	configs := []struct {
		name string
		opts agents.RenderOpts
	}{
		{"remote", agents.RenderOpts{HasRemote: true}},
		{"no remote", agents.RenderOpts{HasRemote: false}},
		{"no push", agents.RenderOpts{HasRemote: true, NoPush: true}},
	}
	wantHash := agents.CurrentHashWithOpts(agents.ProfileFull, agents.UserLevelRenderOpts())
	orig := detectRenderOptsImpl
	t.Cleanup(func() { detectRenderOptsImpl = orig })
	for _, install := range configs {
		for _, check := range configs {
			t.Run(install.name+" install, "+check.name+" check", func(t *testing.T) {
				home := stubMuxHome(t)
				globalPath := filepath.Join(home, ".mux", "AGENTS.md")
				installEnv, _, _ := newFactoryTestEnv(t)
				installOpts := install.opts
				detectRenderOptsImpl = func() agents.RenderOpts { return installOpts }
				if err := installMux(installEnv, false, true); err != nil {
					t.Fatalf("installMux: %v", err)
				}
				if got := readMuxFile(t, globalPath); !strings.Contains(got, "hash:"+wantHash) {
					t.Errorf("expected hash %s regardless of project config, got:\n%s", wantHash, got)
				}

				// --check --global from another project, set up under its
				// own config.
				checkEnv, stdout, _ := newFactoryTestEnv(t)
				checkOpts := check.opts
				detectRenderOptsImpl = func() agents.RenderOpts { return checkOpts }
				if err := installMux(checkEnv, false, false); err != nil {
					t.Fatalf("installMux (other project): %v", err)
				}
				stdout.Reset()
				if err := checkMux(checkEnv, false, true); err != nil {
					t.Fatalf("checkMux: %v\n%s", err, stdout.String())
				}
				if !strings.Contains(stdout.String(), globalPath+" (current)") {
					t.Errorf("expected the global layer reported current, got:\n%s", stdout.String())
				}
			})
		}
	}
}

// A ~/.mux/AGENTS.md section an older bd rendered for a project is stale:
// --check names the global install, which rewords the section in place and
// keeps everything around it, including the project scaffolding an older
// global install put in a new file.
func TestInstallMuxGlobalRewordsProjectSection(t *testing.T) {
	stubDetectRenderOpts(t)
	for _, tt := range []struct {
		name    string
		section string
		seed    string
	}{
		{
			name:    "beside user text",
			section: agents.RenderSection(agents.ProfileFull),
			seed:    "# Mine\n\n" + agents.RenderSection(agents.ProfileFull),
		},
		{
			name:    "in an older global install's new file",
			section: agents.RenderSectionWithOpts(agents.ProfileFull, agents.RenderOpts{}),
			seed:    createNewAgentsFileWithOpts(agents.ProfileFull, agents.RenderOpts{}),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env, stdout, _ := newFactoryTestEnv(t)
			home := stubMuxHome(t)
			globalPath := filepath.Join(home, ".mux", "AGENTS.md")
			writeMuxFile(t, globalPath, tt.seed)
			if err := installMux(env, false, false); err != nil {
				t.Fatalf("installMux: %v", err)
			}

			stdout.Reset()
			if err := checkMux(env, false, true); !errors.Is(err, errBeadsSectionStale) {
				t.Fatalf("expected errBeadsSectionStale, got %v\n%s", err, stdout.String())
			}
			if !strings.Contains(stdout.String(), "Run: bd setup mux --global (to update)") {
				t.Errorf("expected the global setup hint, got:\n%s", stdout.String())
			}

			if err := installMux(env, false, true); err != nil {
				t.Fatalf("installMux(global=true): %v", err)
			}
			want := strings.Replace(tt.seed, tt.section, muxUserLevelSection(), 1)
			if got := readMuxFile(t, globalPath); got != want {
				t.Errorf("global AGENTS.md after the global install:\ngot:\n%s\nwant:\n%s", got, want)
			}
			stdout.Reset()
			if err := checkMux(env, false, true); err != nil {
				t.Fatalf("checkMux: %v\n%s", err, stdout.String())
			}
		})
	}
}

// Run from the home directory, the workspace layer's .mux/AGENTS.md is
// ~/.mux/AGENTS.md, and run from ~/.mux, so is the root AGENTS.md. That file
// is managed as the global layer whichever layer names it, so one run does
// not write it one way and then the other, and --check does not call it
// stale.
func TestMuxLayerAtGlobalPathIsGlobalLayer(t *testing.T) {
	stubDetectRenderOpts(t)
	inHome := func(home string) string { return home }
	inMuxHome := func(home string) string { return filepath.Join(home, ".mux") }
	for _, tt := range []struct {
		name            string
		dir             func(home string) string // the directory bd runs in
		link            bool                     // run through a symlink to dir
		project, global bool
	}{
		{name: "workspace layer from home", dir: inHome, project: true, global: true},
		{name: "workspace layer alone from home", dir: inHome, project: true},
		{name: "workspace layer from a symlink to home", dir: inHome, link: true, project: true, global: true},
		{name: "root layer from ~/.mux", dir: inMuxHome, global: true},
		{name: "root layer alone from ~/.mux", dir: inMuxHome},
	} {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := stubMuxHome(t)
			dir := tt.dir(homeDir)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
			if tt.link {
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(dir, link); err != nil {
					t.Skipf("symlink: %v", err)
				}
				dir = link
			}
			stdout := &bytes.Buffer{}
			env := agentsEnv{agentsPath: filepath.Join(dir, "AGENTS.md"), stdout: stdout, stderr: &bytes.Buffer{}}
			globalPath := filepath.Join(homeDir, ".mux", "AGENTS.md")

			if err := installMux(env, tt.project, tt.global); err != nil {
				t.Fatalf("installMux: %v\n%s", err, stdout.String())
			}
			if got, want := readMuxFile(t, globalPath), muxUserLevelSection(); got != want {
				t.Errorf("global AGENTS.md should hold only the user-level section:\ngot:\n%s\nwant:\n%s", got, want)
			}
			stdout.Reset()
			if err := checkMux(env, tt.project, tt.global); err != nil {
				t.Fatalf("checkMux: %v\n%s", err, stdout.String())
			}
			stdout.Reset()
			if err := installMux(env, tt.project, tt.global); err != nil {
				t.Fatalf("installMux (rerun): %v\n%s", err, stdout.String())
			}
			if strings.Contains(stdout.String(), "Updated existing beads section") {
				t.Errorf("a rerun rewrote a section:\n%s", stdout.String())
			}
		})
	}

	// Only a root file named AGENTS.md is the global one: an agents file of
	// another name in ~/.mux is the root layer's own.
	t.Run("root layer with another agents file from ~/.mux", func(t *testing.T) {
		homeDir := stubMuxHome(t)
		dir := filepath.Join(homeDir, ".mux")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		env := agentsEnv{agentsPath: filepath.Join(dir, "TEAM.md"), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := installMux(env, false, false); err != nil {
			t.Fatalf("installMux: %v", err)
		}
		if got, want := readMuxFile(t, env.agentsPath), createNewAgentsFileWithOpts(agents.ProfileFull, agents.DefaultRenderOpts()); got != want {
			t.Errorf("TEAM.md should get the project rendering:\ngot:\n%s\nwant:\n%s", got, want)
		}
		if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
			t.Errorf("expected no global AGENTS.md, stat err = %v", err)
		}
	})
}

func TestRemoveMuxGlobalRemovesGlobalLayerSection(t *testing.T) {
	for _, tt := range []struct {
		name string
		seed string
		want string
	}{
		// A new global file holds only the section, so removing it
		// leaves the file empty.
		{name: "new file", want: ""},
		{name: "user text", seed: "# Mine\n", want: "# Mine\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env, _, _ := newFactoryTestEnv(t)
			home := stubMuxHome(t)
			globalPath := filepath.Join(home, ".mux", "AGENTS.md")
			if tt.seed != "" {
				writeMuxFile(t, globalPath, tt.seed)
			}

			if err := installMux(env, false, true); err != nil {
				t.Fatalf("installMux(global=true) returned error: %v", err)
			}
			if err := removeMux(env, false, true); err != nil {
				t.Fatalf("removeMux(global=true) returned error: %v", err)
			}

			if got := readMuxFile(t, globalPath); got != tt.want {
				t.Fatalf("global AGENTS.md after remove = %q, want %q", got, tt.want)
			}
		})
	}
}
