package gate

import (
	"os"
	"os/exec"
	"strings"
)

// RegisterBridgeGates registers the molecule-to-session bridge gate.
func RegisterBridgeGates(reg *Registry) {
	_ = reg.Register(MolGatePendingGate())
}

// MolGatePendingGate returns the "mol-gate-pending" gate definition.
// Warns when the active molecule has unresolved human/decision gates
// that the agent should address before ending the session.
func MolGatePendingGate() *Gate {
	return &Gate{
		ID:          "mol-gate-pending",
		Hook:        HookStop,
		Description: "active molecule has pending human/decision gates",
		Mode:        GateModeSoft,
		AutoCheck:   checkMolGatesPending,
		Hint:        "your molecule has pending gates requiring human action — run bd gate list to see them",
	}
}

// checkMolGatesPending checks if the hooked bead's molecule has open
// human/decision gate issues. Returns true (satisfied) if no pending
// gates exist, or if there's no hooked bead.
func checkMolGatesPending(ctx GateContext) bool {
	hookBead := ctx.HookBead
	if hookBead == "" {
		hookBead = os.Getenv("GT_HOOK_BEAD")
	}
	if hookBead == "" {
		return true // no hooked bead, nothing to check
	}

	// Query for open gate issues under the hooked bead using bd CLI.
	// This avoids importing storage directly, keeping the gate package
	// free of database dependencies.
	cmd := exec.Command("bd", "list",
		"--type", "gate",
		"--parent", hookBead,
		"--status", "open",
		"--json",
	)
	if ctx.WorkDir != "" {
		cmd.Dir = ctx.WorkDir
	}

	output, err := cmd.Output()
	if err != nil {
		// If bd list fails, assume no pending gates (fail open)
		return true
	}

	// Check if any results are human/decision gates
	// The output is a JSON array of issues; we look for human-actionable types
	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" || outputStr == "[]" || outputStr == "null" {
		return true // no open gates
	}

	// Check for human-actionable gate types in the output
	// Types that require human action: human, mail, decision
	// Types that are external waits (not bridged): gh:run, gh:pr, timer, bead
	for _, actionable := range []string{`"human"`, `"mail"`, `"decision"`} {
		if strings.Contains(outputStr, actionable) {
			return false // has actionable pending gates
		}
	}

	return true // only non-human gates (external waits)
}
