package setup

import (
	"os"
	"strings"
	"testing"
)

func stubMuxEnvProvider(t *testing.T, env agentsEnv) {
	t.Helper()
	orig := muxEnvProvider
	muxEnvProvider = func() agentsEnv {
		return env
	}
	t.Cleanup(func() { muxEnvProvider = orig })
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
	if got := muxProjectAgentsPath("AGENTS.md"); got != ".mux/AGENTS.md" {
		t.Fatalf("got %q, want .mux/AGENTS.md", got)
	}
	if got := muxProjectAgentsPath("/tmp/work/AGENTS.md"); got != "/tmp/work/.mux/AGENTS.md" {
		t.Fatalf("got %q, want /tmp/work/.mux/AGENTS.md", got)
	}
}

func TestMuxProjectHookPaths(t *testing.T) {
	initPath, toolPostPath, toolEnvPath := muxProjectHookPaths("AGENTS.md")
	if initPath != ".mux/init" {
		t.Fatalf("init path = %q, want .mux/init", initPath)
	}
	if toolPostPath != ".mux/tool_post" {
		t.Fatalf("tool_post path = %q, want .mux/tool_post", toolPostPath)
	}
	if toolEnvPath != ".mux/tool_env" {
		t.Fatalf("tool_env path = %q, want .mux/tool_env", toolEnvPath)
	}
}

func TestInstallMuxProjectInstallsBothLayers(t *testing.T) {
	env, _, _ := newFactoryTestEnv(t)
	if err := installMux(env, true, false); err != nil {
		t.Fatalf("installMux(project=true) returned error: %v", err)
	}
	if !FileExists(env.agentsPath) {
		t.Fatalf("expected root AGENTS.md at %s", env.agentsPath)
	}
	projectPath := muxProjectAgentsPath(env.agentsPath)
	if !FileExists(projectPath) {
		t.Fatalf("expected project AGENTS.md at %s", projectPath)
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
	muxUserHomeDir = func() (string, error) {
		return "/tmp/test-home", nil
	}

	got, err := muxGlobalAgentsPath()
	if err != nil {
		t.Fatalf("muxGlobalAgentsPath returned error: %v", err)
	}
	if got != "/tmp/test-home/.mux/AGENTS.md" {
		t.Fatalf("got %q, want /tmp/test-home/.mux/AGENTS.md", got)
	}
}

func TestInstallMuxGlobalInstallsGlobalLayer(t *testing.T) {
	env, _, _ := newFactoryTestEnv(t)
	home := t.TempDir()
	t.Cleanup(func() {
		muxUserHomeDir = os.UserHomeDir
	})
	muxUserHomeDir = func() (string, error) {
		return home, nil
	}

	if err := installMux(env, false, true); err != nil {
		t.Fatalf("installMux(global=true) returned error: %v", err)
	}

	globalPath, err := muxGlobalAgentsPath()
	if err != nil {
		t.Fatalf("muxGlobalAgentsPath returned error: %v", err)
	}
	if !FileExists(globalPath) {
		t.Fatalf("expected global AGENTS.md at %s", globalPath)
	}
	if err := checkMux(env, false, true); err != nil {
		t.Fatalf("checkMux(global=true) returned error: %v", err)
	}
}

func TestRemoveMuxGlobalRemovesGlobalLayerSection(t *testing.T) {
	env, _, _ := newFactoryTestEnv(t)
	home := t.TempDir()
	t.Cleanup(func() {
		muxUserHomeDir = os.UserHomeDir
	})
	muxUserHomeDir = func() (string, error) {
		return home, nil
	}

	if err := installMux(env, false, true); err != nil {
		t.Fatalf("installMux(global=true) returned error: %v", err)
	}
	if err := removeMux(env, false, true); err != nil {
		t.Fatalf("removeMux(global=true) returned error: %v", err)
	}

	globalPath, err := muxGlobalAgentsPath()
	if err != nil {
		t.Fatalf("muxGlobalAgentsPath returned error: %v", err)
	}
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("expected %s to remain readable after remove: %v", globalPath, err)
	}
	if strings.Contains(string(data), agentsBeginMarker) {
		t.Fatalf("expected beads markers removed from %s", globalPath)
	}
}
