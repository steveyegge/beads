package testutil

import (
	"fmt"
	"os/exec"
	"strings"
)

// ForceRepoLocalHooksPath configures a git test repository to use .git/hooks
// regardless of any global core.hooksPath configuration.
func ForceRepoLocalHooksPath(repoDir string) error {
	cmd := exec.Command("git", "config", "core.hooksPath", ".git/hooks")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return fmt.Errorf("set core.hooksPath in %s: %w (output: %s)", repoDir, err, trimmed)
		}
		return fmt.Errorf("set core.hooksPath in %s: %w", repoDir, err)
	}
	return nil
}
