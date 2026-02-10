package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
)

func TestOutputContextFunction(t *testing.T) {
	tests := []struct {
		name          string
		mcpMode       bool
		stealthMode   bool
		ephemeralMode bool
		localOnlyMode bool
		expectText    []string
		rejectText    []string
	}{
		{
			name:          "CLI Normal (non-ephemeral)",
			mcpMode:       false,
			stealthMode:   false,
			ephemeralMode: false,
			localOnlyMode: false,
			expectText:    []string{"Beads Workflow Context", "bd sync", "git push"},
			rejectText:    []string{"bd sync --flush-only", "--from-main"},
		},
		{
			name:          "CLI Normal (ephemeral)",
			mcpMode:       false,
			stealthMode:   false,
			ephemeralMode: true,
			localOnlyMode: false,
			expectText:    []string{"Beads Workflow Context", "bd sync --from-main", "ephemeral branch"},
			rejectText:    []string{"bd sync --flush-only", "git push"},
		},
		{
			name:          "CLI Stealth",
			mcpMode:       false,
			stealthMode:   true,
			ephemeralMode: false, // stealth mode overrides ephemeral detection
			localOnlyMode: false,
			expectText:    []string{"Beads Workflow Context", "bd sync --flush-only"},
			rejectText:    []string{"git push", "git pull", "git commit", "git status", "git add"},
		},
		{
			name:          "CLI Local-only (no git remote)",
			mcpMode:       false,
			stealthMode:   false,
			ephemeralMode: false,
			localOnlyMode: true,
			expectText:    []string{"Beads Workflow Context", "bd sync --flush-only", "No git remote configured"},
			rejectText:    []string{"git push", "git pull", "--from-main"},
		},
		{
			name:          "CLI Local-only overrides ephemeral",
			mcpMode:       false,
			stealthMode:   false,
			ephemeralMode: true, // ephemeral is true but local-only takes precedence
			localOnlyMode: true,
			expectText:    []string{"Beads Workflow Context", "bd sync --flush-only", "No git remote configured"},
			rejectText:    []string{"git push", "--from-main", "ephemeral branch"},
		},
		{
			name:          "CLI Stealth overrides local-only",
			mcpMode:       false,
			stealthMode:   true,
			ephemeralMode: false,
			localOnlyMode: true, // local-only is true but stealth takes precedence
			expectText:    []string{"Beads Workflow Context", "bd sync --flush-only"},
			rejectText:    []string{"git push", "git pull", "git commit", "git status", "git add", "No git remote configured"},
		},
		{
			name:          "MCP Normal (non-ephemeral)",
			mcpMode:       true,
			stealthMode:   false,
			ephemeralMode: false,
			localOnlyMode: false,
			expectText:    []string{"Beads Issue Tracker Active", "bd sync", "git push"},
			rejectText:    []string{"bd sync --flush-only", "--from-main"},
		},
		{
			name:          "MCP Normal (ephemeral)",
			mcpMode:       true,
			stealthMode:   false,
			ephemeralMode: true,
			localOnlyMode: false,
			expectText:    []string{"Beads Issue Tracker Active", "bd sync --from-main", "ephemeral branch"},
			rejectText:    []string{"bd sync --flush-only", "git push"},
		},
		{
			name:          "MCP Stealth",
			mcpMode:       true,
			stealthMode:   true,
			ephemeralMode: false, // stealth mode overrides ephemeral detection
			localOnlyMode: false,
			expectText:    []string{"Beads Issue Tracker Active", "bd sync --flush-only"},
			rejectText:    []string{"git push", "git pull", "git commit", "git status", "git add"},
		},
		{
			name:          "MCP Local-only (no git remote)",
			mcpMode:       true,
			stealthMode:   false,
			ephemeralMode: false,
			localOnlyMode: true,
			expectText:    []string{"Beads Issue Tracker Active", "bd sync --flush-only"},
			rejectText:    []string{"git push", "git pull", "--from-main"},
		},
		{
			name:          "MCP Local-only overrides ephemeral",
			mcpMode:       true,
			stealthMode:   false,
			ephemeralMode: true, // ephemeral is true but local-only takes precedence
			localOnlyMode: true,
			expectText:    []string{"Beads Issue Tracker Active", "bd sync --flush-only"},
			rejectText:    []string{"git push", "--from-main", "ephemeral branch"},
		},
		{
			name:          "MCP Stealth overrides local-only",
			mcpMode:       true,
			stealthMode:   true,
			ephemeralMode: false,
			localOnlyMode: true, // local-only is true but stealth takes precedence
			expectText:    []string{"Beads Issue Tracker Active", "bd sync --flush-only"},
			rejectText:    []string{"git push", "git pull", "git commit", "git status", "git add"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer stubIsEphemeralBranch(tt.ephemeralMode)()
			defer stubPrimeDaemonStatus(nil)()                // Default: no daemon in tests
			defer stubPrimeHasGitRemote(!tt.localOnlyMode)() // localOnly = !primeHasGitRemote

			var buf bytes.Buffer
			err := outputPrimeContext(&buf, tt.mcpMode, tt.stealthMode)
			if err != nil {
				t.Fatalf("outputPrimeContext failed: %v", err)
			}

			output := buf.String()

			for _, expected := range tt.expectText {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected text not found: %s", expected)
				}
			}

			for _, rejected := range tt.rejectText {
				if strings.Contains(output, rejected) {
					t.Errorf("Unexpected text found: %s", rejected)
				}
			}
		})
	}
}

// stubIsEphemeralBranch temporarily replaces isEphemeralBranch
// with a stub returning returnValue.
//
// Returns a function to restore the original isEphemeralBranch.
// Usage:
//
//	defer stubIsEphemeralBranch(true)()
func stubIsEphemeralBranch(isEphem bool) func() {
	original := isEphemeralBranch
	isEphemeralBranch = func() bool {
		return isEphem
	}
	return func() {
		isEphemeralBranch = original
	}
}

// stubPrimeDaemonStatus temporarily replaces primeDaemonStatus
// with a stub returning the given status.
func stubPrimeDaemonStatus(status *rpc.StatusResponse) func() {
	original := primeDaemonStatus
	primeDaemonStatus = func() *rpc.StatusResponse {
		return status
	}
	return func() {
		primeDaemonStatus = original
	}
}

// stubPrimeHasGitRemote temporarily replaces primeHasGitRemote
// with a stub returning returnValue.
//
// Returns a function to restore the original primeHasGitRemote.
// Usage:
//
//	defer stubPrimeHasGitRemote(true)()
func stubPrimeHasGitRemote(hasRemote bool) func() {
	original := primeHasGitRemote
	primeHasGitRemote = func() bool {
		return hasRemote
	}
	return func() {
		primeHasGitRemote = original
	}
}

func TestOutputContextStalenessWarning(t *testing.T) {
	tests := []struct {
		name       string
		mcpMode    bool
		status     *rpc.StatusResponse
		expectText []string
		rejectText []string
	}{
		{
			name:       "nil status shows daemon not running warning (CLI)",
			mcpMode:    false,
			status:     nil,
			expectText: []string{"Sync freshness unknown"},
		},
		{
			name:       "nil status shows daemon not running warning (MCP)",
			mcpMode:    true,
			status:     nil,
			expectText: []string{"Sync freshness unknown"},
		},
		{
			name:    "stale daemon shows warning",
			mcpMode: false,
			status: &rpc.StatusResponse{
				AutoCommit:       true,
				AutoPush:         true,
				LastActivityTime: time.Now().Add(-3 * time.Hour).Format(time.RFC3339),
			},
			expectText: []string{"Sync may be stale"},
		},
		{
			name:    "fresh daemon no warning",
			mcpMode: false,
			status: &rpc.StatusResponse{
				AutoCommit:       true,
				AutoPush:         true,
				LastActivityTime: time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			},
			rejectText: []string{"Sync may be stale", "Sync freshness unknown"},
		},
		{
			name:    "daemon not auto-syncing no warning",
			mcpMode: false,
			status: &rpc.StatusResponse{
				AutoCommit:       false,
				AutoPush:         false,
				LastActivityTime: time.Now().Add(-3 * time.Hour).Format(time.RFC3339),
			},
			rejectText: []string{"Sync may be stale"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer stubIsEphemeralBranch(false)()
			defer stubPrimeDaemonStatus(tt.status)()
			defer stubPrimeHasGitRemote(true)()

			var buf bytes.Buffer
			err := outputPrimeContext(&buf, tt.mcpMode, false)
			if err != nil {
				t.Fatalf("outputPrimeContext failed: %v", err)
			}

			output := buf.String()

			for _, expected := range tt.expectText {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected text not found: %q\nOutput:\n%s", expected, output)
				}
			}

			for _, rejected := range tt.rejectText {
				if strings.Contains(output, rejected) {
					t.Errorf("Unexpected text found: %q", rejected)
				}
			}
		})
	}
}

func TestOutputContextStalenessSkippedInStealth(t *testing.T) {
	defer stubIsEphemeralBranch(false)()
	defer stubPrimeDaemonStatus(nil)() // nil would normally trigger warning
	defer stubPrimeHasGitRemote(true)()

	var buf bytes.Buffer
	err := outputPrimeContext(&buf, false, true) // stealth=true
	if err != nil {
		t.Fatalf("outputPrimeContext failed: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "Sync freshness unknown") {
		t.Error("Staleness warning should not appear in stealth mode")
	}
}

func TestOutputContextStalenessSkippedInLocalOnly(t *testing.T) {
	defer stubIsEphemeralBranch(false)()
	defer stubPrimeDaemonStatus(nil)() // nil would normally trigger warning
	defer stubPrimeHasGitRemote(false)() // local-only

	var buf bytes.Buffer
	err := outputPrimeContext(&buf, false, false)
	if err != nil {
		t.Fatalf("outputPrimeContext failed: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "Sync freshness unknown") {
		t.Error("Staleness warning should not appear in local-only mode")
	}
}

func TestCheckSyncStaleness(t *testing.T) {
	tests := []struct {
		name   string
		status *rpc.StatusResponse
		want   string
	}{
		{
			name:   "nil status",
			status: nil,
			want:   "Sync freshness unknown",
		},
		{
			name: "not auto-syncing",
			status: &rpc.StatusResponse{
				AutoCommit: false,
				AutoPush:   false,
			},
			want: "",
		},
		{
			name: "empty activity time",
			status: &rpc.StatusResponse{
				AutoCommit: true,
				AutoPush:   true,
			},
			want: "",
		},
		{
			name: "recent activity",
			status: &rpc.StatusResponse{
				AutoCommit:       true,
				AutoPush:         true,
				LastActivityTime: time.Now().Add(-10 * time.Minute).Format(time.RFC3339),
			},
			want: "",
		},
		{
			name: "stale activity",
			status: &rpc.StatusResponse{
				AutoCommit:       true,
				AutoPush:         true,
				LastActivityTime: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
			},
			want: "Sync may be stale",
		},
		{
			name: "invalid time format",
			status: &rpc.StatusResponse{
				AutoCommit:       true,
				AutoPush:         true,
				LastActivityTime: "not-a-time",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkSyncStaleness(tt.status)
			if tt.want == "" {
				if got != "" {
					t.Errorf("checkSyncStaleness() = %q, want empty", got)
				}
			} else {
				if !strings.Contains(got, tt.want) {
					t.Errorf("checkSyncStaleness() = %q, want to contain %q", got, tt.want)
				}
			}
		})
	}
}
