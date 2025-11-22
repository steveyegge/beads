package fix

import (
	"fmt"
	"os/exec"
)

// MergeDriver fixes the git merge driver configuration to use correct placeholders.
// Git only supports %O (base), %A (current), %B (other) - not %L/%R.
func MergeDriver(path string) error {
	if err := validateBeadsWorkspace(path); err != nil {
		return err
	}

	// Update git config to use correct placeholders
	// #nosec G204 -- path is validated by validateBeadsWorkspace
	cmd := exec.Command("git", "config", "merge.beads.driver", "bd merge %A %O %A %B")
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to update git merge driver config: %w\nOutput: %s", err, output)
	}

	return nil
}
