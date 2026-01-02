package compact

import (
	"os/exec"
	"strings"
)

var gitExec = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// GetCurrentCommitHash returns the current git HEAD commit hash.
// Returns empty string if not in a git repository or if git command fails.
func GetCurrentCommitHash() string {
	output, err := gitExec("git", "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
