package setup

import (
	"strings"
	"testing"
)

func stubOpenCodeEnvProvider(t *testing.T, env agentsEnv) {
	t.Helper()
	orig := opencodeEnvProvider
	opencodeEnvProvider = func() agentsEnv {
		return env
	}
	t.Cleanup(func() { opencodeEnvProvider = orig })
}

func TestInstallOpenCodeCreatesNewFile(t *testing.T) {
	env, stdout, _ := newFactoryTestEnv(t)
	if err := installOpenCode(env); err != nil {
		t.Fatalf("installOpenCode returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "OpenCode integration installed") {
		t.Error("expected OpenCode install success message")
	}
}

func TestCheckOpenCodeMissingFile(t *testing.T) {
	env, stdout, _ := newFactoryTestEnv(t)
	err := checkOpenCode(env)
	if err == nil {
		t.Fatal("expected error for missing AGENTS.md")
	}
	if !strings.Contains(stdout.String(), "bd setup opencode") {
		t.Error("expected setup guidance for opencode")
	}
}
