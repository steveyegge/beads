package routing

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// TokenDiscoverer provides methods to discover GitHub authentication tokens.
type TokenDiscoverer interface {
	// DiscoverToken attempts to find a GitHub token from various sources.
	// Returns the token if found, or empty string if no token is available.
	// Never returns an error - graceful degradation is expected.
	DiscoverToken() string
}

// EnvGetter is a function type for getting environment variables.
// This allows mocking os.Getenv in tests.
type EnvGetter func(key string) string

// CommandRunner is a function type for running shell commands.
// This allows mocking exec.Command in tests.
type CommandRunner func(name string, args ...string) ([]byte, error)

// CommandWithStdinRunner is a function type for running shell commands with stdin.
// This allows mocking exec.Command in tests for commands requiring stdin.
type CommandWithStdinRunner func(name string, args []string, stdin string) ([]byte, error)

// RealTokenDiscoverer implements TokenDiscoverer using the environment and CLI tools.
type RealTokenDiscoverer struct {
	getEnv              EnvGetter
	runCommand          CommandRunner
	runCommandWithStdin CommandWithStdinRunner
}

// NewTokenDiscoverer creates a new TokenDiscoverer with real implementations.
func NewTokenDiscoverer() *RealTokenDiscoverer {
	return &RealTokenDiscoverer{
		getEnv:              os.Getenv,
		runCommand:          defaultCommandRunner,
		runCommandWithStdin: defaultCommandWithStdinRunner,
	}
}

// NewTokenDiscovererWithMocks creates a TokenDiscoverer with custom implementations.
// This is used for testing.
func NewTokenDiscovererWithMocks(getEnv EnvGetter, runCommand CommandRunner) *RealTokenDiscoverer {
	// Create a stdin runner that always fails - tests should not rely on git credential
	// unless they explicitly set up for it
	mockStdinRunner := func(name string, args []string, stdin string) ([]byte, error) {
		// Delegate to runCommand for testability
		return runCommand(name, args...)
	}
	return &RealTokenDiscoverer{
		getEnv:              getEnv,
		runCommand:          runCommand,
		runCommandWithStdin: mockStdinRunner,
	}
}

// defaultCommandRunner executes a command and returns its output.
func defaultCommandRunner(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.Output()
}

// defaultCommandWithStdinRunner executes a command with stdin and returns its output.
func defaultCommandWithStdinRunner(name string, args []string, stdin string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = bytes.NewBufferString(stdin)
	return cmd.Output()
}

// DiscoverToken attempts to find a GitHub token from multiple sources.
// Discovery order (first found wins):
//  1. GITHUB_TOKEN environment variable
//  2. GH_TOKEN environment variable
//  3. gh auth token CLI command (if gh is installed)
//  4. git credential fill for github.com
//
// Returns empty string if no token is found. This is not an error condition -
// the caller should fall back to heuristic detection.
func (d *RealTokenDiscoverer) DiscoverToken() string {
	// 1. Check GITHUB_TOKEN env var (most explicit)
	if token := d.getEnv("GITHUB_TOKEN"); token != "" {
		return token
	}

	// 2. Check GH_TOKEN env var (GitHub CLI convention)
	if token := d.getEnv("GH_TOKEN"); token != "" {
		return token
	}

	// 3. Try gh auth token (GitHub CLI)
	if token := d.tryGHCLI(); token != "" {
		return token
	}

	// 4. Try git credential fill
	if token := d.tryGitCredential(); token != "" {
		return token
	}

	// No token found - this is fine, caller should fall back to heuristic
	return ""
}

// tryGHCLI attempts to get a token from the GitHub CLI.
func (d *RealTokenDiscoverer) tryGHCLI() string {
	output, err := d.runCommand("gh", "auth", "token")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// tryGitCredential attempts to get a token from git credential store.
func (d *RealTokenDiscoverer) tryGitCredential() string {
	// Git credential fill expects input on stdin in a specific format
	input := "protocol=https\nhost=github.com\n\n"

	// Use the configurable stdin runner for testability
	output, err := d.runCommandWithStdin("git", []string{"credential", "fill"}, input)
	if err != nil {
		return ""
	}

	// Parse the output to find the password (token)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "password=") {
			return strings.TrimPrefix(line, "password=")
		}
	}

	return ""
}
