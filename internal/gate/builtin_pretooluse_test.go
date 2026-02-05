package gate

import (
	"testing"
)

func TestDestructiveOpGate(t *testing.T) {
	g := DestructiveOpGate()
	if g.ID != "destructive-op" {
		t.Errorf("expected ID 'destructive-op', got %q", g.ID)
	}
	if g.Hook != HookPreToolUse {
		t.Errorf("expected HookPreToolUse, got %q", g.Hook)
	}
	if g.Mode != GateModeStrict {
		t.Errorf("expected strict, got %q", g.Mode)
	}
}

func TestSandboxBoundaryGate(t *testing.T) {
	g := SandboxBoundaryGate()
	if g.ID != "sandbox-boundary" {
		t.Errorf("expected ID 'sandbox-boundary', got %q", g.ID)
	}
	if g.Hook != HookPreToolUse {
		t.Errorf("expected HookPreToolUse, got %q", g.Hook)
	}
	if g.Mode != GateModeSoft {
		t.Errorf("expected soft, got %q", g.Mode)
	}
}

func TestRegisterPreToolUseGates(t *testing.T) {
	reg := NewRegistry()
	RegisterPreToolUseGates(reg)

	if reg.Count() != 2 {
		t.Errorf("expected 2 gates, got %d", reg.Count())
	}

	toolGates := reg.GatesForHook(HookPreToolUse)
	if len(toolGates) != 2 {
		t.Errorf("expected 2 PreToolUse gates, got %d", len(toolGates))
	}
}

func TestCheckNotDestructive(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantSafe bool
	}{
		{"empty", "", true},
		{"safe ls", "ls -la", true},
		{"safe git status", "git status", true},
		{"safe git push", "git push origin main", true},
		{"rm -rf", "rm -rf /tmp/foo", false},
		{"rm -r", "rm -r some/dir", false},
		{"git push --force", "git push --force origin main", false},
		{"git push -f", "git push -f origin main", false},
		{"git reset --hard", "git reset --hard HEAD~1", false},
		{"git clean -f", "git clean -f", false},
		{"git branch -D", "git branch -D feature", false},
		{"DROP TABLE", "DROP TABLE users", false},
		{"drop table", "drop table users", false},
		{"TRUNCATE", "TRUNCATE users", false},
		{"docker rm", "docker rm container-id", false},
		{"docker rmi", "docker rmi image-id", false},
		{"safe go test", "go test ./...", true},
		{"safe mkdir", "mkdir -p /tmp/test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := GateContext{ToolInput: tt.cmd}
			got := checkNotDestructive(ctx)
			if got != tt.wantSafe {
				t.Errorf("checkNotDestructive(%q) = %v, want %v", tt.cmd, got, tt.wantSafe)
			}
		})
	}
}

func TestCheckWithinSandbox(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		workDir  string
		wantSafe bool
	}{
		{"empty command", "", "/home/user/project", true},
		{"relative path", "cat foo.txt", "/home/user/project", true},
		{"absolute within workspace", "cat /home/user/project/file.txt", "/home/user/project", true},
		{"absolute outside workspace", "cat /etc/passwd", "/home/user/project", false},
		{"home relative", "cat ~/other/file", "/home/user/project", false},
		{"parent relative", "cat ../other/file", "/home/user/project", false},
		{"no workdir", "cat /etc/passwd", "", true}, // fail open
		{"flag with path", "ls -la /home/user/project/dir", "/home/user/project", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GT_ROOT", "")
			ctx := GateContext{ToolInput: tt.cmd, WorkDir: tt.workDir}
			got := checkWithinSandbox(ctx)
			if got != tt.wantSafe {
				t.Errorf("checkWithinSandbox(%q, workDir=%q) = %v, want %v", tt.cmd, tt.workDir, got, tt.wantSafe)
			}
		})
	}
}

func TestExtractPaths(t *testing.T) {
	tests := []struct {
		cmd      string
		expected []string
	}{
		{"ls -la", nil},
		{"cat /etc/passwd", []string{"/etc/passwd"}},
		{"cp /src /dst", []string{"/src", "/dst"}},
		{"cat ~/file.txt", []string{"~/file.txt"}},
		{"cat ../parent/file", []string{"../parent/file"}},
		{"git status", nil},
		{"-flag /not/a/path", []string{"/not/a/path"}}, // only flag is skipped, path arg is extracted
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := extractPaths(tt.cmd)
			if len(got) != len(tt.expected) {
				t.Errorf("extractPaths(%q) = %v, want %v", tt.cmd, got, tt.expected)
				return
			}
			for i, p := range got {
				if p != tt.expected[i] {
					t.Errorf("extractPaths(%q)[%d] = %q, want %q", tt.cmd, i, p, tt.expected[i])
				}
			}
		})
	}
}

func TestPreToolUseIntegration(t *testing.T) {
	workDir := t.TempDir()
	sessionID := "pretooluse-integration"

	reg := NewRegistry()
	RegisterPreToolUseGates(reg)

	// Safe command — should allow
	// Note: auto-checks use GateContext, but CheckGatesForHook creates a basic one.
	// We need to test via EvaluateHook with context passed through.
	resp, err := EvaluateHook(workDir, sessionID, HookPreToolUse, reg)
	if err != nil {
		t.Fatal(err)
	}
	// With empty ToolInput, both auto-checks pass
	if resp.Decision != "allow" {
		t.Errorf("expected allow for empty input, got %q: %s", resp.Decision, resp.Reason)
	}
}
