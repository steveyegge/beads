package gate

import (
	"os"
	"os/exec"
	"strings"
)

// RegisterBuiltinGates registers the built-in session gates on the given registry.
func RegisterBuiltinGates(reg *Registry) {
	_ = reg.Register(DecisionGate())
	_ = reg.Register(CommitPushGate())
	_ = reg.Register(BeadUpdateGate())
}

// DecisionGate returns the "decision" gate definition.
// Satisfied when a decision point was offered during the session.
func DecisionGate() *Gate {
	return &Gate{
		ID:          "decision",
		Hook:        HookStop,
		Description: "decision point offered before session end",
		Mode:        GateModeStrict,
		Hint:        "offer a decision point before ending the session",
	}
}

// CommitPushGate returns the "commit-push" gate definition.
// Satisfied when changes are committed and pushed, or when there are
// no uncommitted changes (auto-check).
func CommitPushGate() *Gate {
	return &Gate{
		ID:          "commit-push",
		Hook:        HookStop,
		Description: "changes committed and pushed",
		Mode:        GateModeSoft,
		AutoCheck:   checkGitClean,
		Hint:        "you have uncommitted changes — run git add/commit/push",
	}
}

// BeadUpdateGate returns the "bead-update" gate definition.
// Satisfied when the hooked bead was updated, or when no bead is hooked
// (auto-check).
func BeadUpdateGate() *Gate {
	return &Gate{
		ID:          "bead-update",
		Hook:        HookStop,
		Description: "hooked bead status updated",
		Mode:        GateModeSoft,
		AutoCheck:   checkNoBeadHooked,
		Hint:        "your hooked bead has not been updated — run bd update or bd close",
	}
}

// checkGitClean returns true if git working tree is clean (no uncommitted changes).
func checkGitClean(ctx GateContext) bool {
	cmd := exec.Command("git", "status", "--porcelain")
	if ctx.WorkDir != "" {
		cmd.Dir = ctx.WorkDir
	}
	output, err := cmd.Output()
	if err != nil {
		// If git status fails (not a git repo, etc.), consider it satisfied
		// to avoid blocking non-git sessions.
		return true
	}
	// Clean if output is empty (no modified/untracked files)
	return strings.TrimSpace(string(output)) == ""
}

// checkNoBeadHooked returns true if no bead is currently hooked.
// If GT_HOOK_BEAD is set, a bead is hooked and this gate needs explicit marking.
func checkNoBeadHooked(ctx GateContext) bool {
	hookBead := ctx.HookBead
	if hookBead == "" {
		hookBead = os.Getenv("GT_HOOK_BEAD")
	}
	return hookBead == ""
}
