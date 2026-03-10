package main

import (
	"path/filepath"

	"github.com/steveyegge/beads/internal/utils"
)

func redirectTargetForWorktree(localBeadsDir, targetBeadsDir string) string {
	absTarget := utils.CanonicalizeIfRelative(targetBeadsDir)
	worktreeRoot := filepath.Dir(localBeadsDir)
	relPath, err := filepath.Rel(worktreeRoot, absTarget)
	if err != nil {
		return absTarget
	}
	return relPath
}
