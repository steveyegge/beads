package gate

import (
	"os"
	"path/filepath"
)

// RegisterPreCompactGates registers the built-in PreCompact gates.
func RegisterPreCompactGates(reg *Registry) {
	_ = reg.Register(StateCheckpointGate())
	_ = reg.Register(DirtyWorkGate())
}

// StateCheckpointGate returns the "state-checkpoint" gate definition.
// Warns when there are pending inject queue items before compaction.
func StateCheckpointGate() *Gate {
	return &Gate{
		ID:          "state-checkpoint",
		Hook:        HookPreCompact,
		Description: "pending injections exist before compaction",
		Mode:        GateModeSoft,
		AutoCheck:   checkInjectQueueEmpty,
		Hint:        "pending injections exist — run gt inject drain before compaction",
	}
}

// DirtyWorkGate returns the "dirty-work" gate definition.
// Warns when there are uncommitted changes before compaction.
func DirtyWorkGate() *Gate {
	return &Gate{
		ID:          "dirty-work",
		Hook:        HookPreCompact,
		Description: "uncommitted changes may be forgotten after compaction",
		Mode:        GateModeSoft,
		AutoCheck:   checkGitClean, // reuse from builtin.go
		Hint:        "you have uncommitted changes that may be forgotten after compaction — consider committing first",
	}
}

// checkInjectQueueEmpty returns true if the inject queue is empty.
// Queue location: .runtime/inject-queue/<session>.jsonl
func checkInjectQueueEmpty(ctx GateContext) bool {
	if ctx.WorkDir == "" || ctx.SessionID == "" {
		return true // can't check, fail open
	}

	queueFile := filepath.Join(ctx.WorkDir, ".runtime", "inject-queue", ctx.SessionID+".jsonl")
	info, err := os.Stat(queueFile)
	if err != nil {
		return true // file doesn't exist = empty queue
	}
	return info.Size() == 0
}
