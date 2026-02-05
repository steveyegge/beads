package gate

import (
	"os"
	"path/filepath"
	"strings"
)

// RegisterPreToolUseGates registers the built-in PreToolUse gates.
func RegisterPreToolUseGates(reg *Registry) {
	_ = reg.Register(DestructiveOpGate())
	_ = reg.Register(SandboxBoundaryGate())
}

// DestructiveOpGate returns the "destructive-op" gate definition.
// Blocks destructive commands unless explicitly approved.
func DestructiveOpGate() *Gate {
	return &Gate{
		ID:          "destructive-op",
		Hook:        HookPreToolUse,
		Description: "destructive command detected",
		Mode:        GateModeStrict,
		AutoCheck:   checkNotDestructive,
		Hint:        "this command appears destructive — confirm with the user or run bd gate mark destructive-op",
	}
}

// SandboxBoundaryGate returns the "sandbox-boundary" gate definition.
// Warns when commands operate outside the workspace boundary.
func SandboxBoundaryGate() *Gate {
	return &Gate{
		ID:          "sandbox-boundary",
		Hook:        HookPreToolUse,
		Description: "command operates outside workspace boundary",
		Mode:        GateModeSoft,
		AutoCheck:   checkWithinSandbox,
		Hint:        "this command operates outside your workspace boundary",
	}
}

// destructivePatterns lists command patterns considered destructive.
var destructivePatterns = []string{
	"rm -rf",
	"rm -r ",
	"git push --force",
	"git push -f",
	"git reset --hard",
	"git clean -f",
	"git branch -D",
	"DROP TABLE",
	"drop table",
	"TRUNCATE",
	"truncate ",
	"docker rm ",
	"docker rmi ",
}

// checkNotDestructive returns true if the command in ToolInput is NOT destructive.
// Returns true (gate satisfied) for safe commands, false for destructive ones.
func checkNotDestructive(ctx GateContext) bool {
	cmd := ctx.ToolInput
	if cmd == "" {
		return true // no command to check
	}

	for _, pattern := range destructivePatterns {
		if strings.Contains(cmd, pattern) {
			return false
		}
	}
	return true
}

// checkWithinSandbox returns true if the command operates within the workspace.
// Uses GT_ROOT or WorkDir to determine the sandbox boundary.
func checkWithinSandbox(ctx GateContext) bool {
	cmd := ctx.ToolInput
	if cmd == "" {
		return true
	}

	// Determine workspace root
	root := os.Getenv("GT_ROOT")
	if root == "" {
		root = ctx.WorkDir
	}
	if root == "" {
		return true // can't determine boundary, fail open
	}

	// Normalize root path
	root, err := filepath.Abs(root)
	if err != nil {
		return true
	}

	// Extract paths from the command and check each one
	paths := extractPaths(cmd)
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(abs, root) {
			return false
		}
	}

	return true
}

// extractPaths extracts file path-like arguments from a command string.
// It looks for arguments that start with / or ~/ or ../ which indicate
// absolute or relative paths.
func extractPaths(cmd string) []string {
	var paths []string
	parts := strings.Fields(cmd)
	for _, p := range parts {
		// Skip flags
		if strings.HasPrefix(p, "-") {
			continue
		}
		// Check for absolute paths, home-relative, or parent-relative
		if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "../") {
			paths = append(paths, p)
		}
	}
	return paths
}
