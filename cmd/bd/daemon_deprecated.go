package main

// daemon_deprecated.go provides stub variables for daemon-related code
// that hasn't been fully removed yet from command files.
//
// The daemon subsystem has been removed. These variables exist only to
// allow gradual cleanup of the 66+ files that still reference them.
// All daemon branches are dead code (daemonClient is always nil,
// daemonStatus is always zero-valued, noDaemon is always false).
//
// TODO: Remove this file after all `if daemonClient != nil` branches
// are cleaned up from command files.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
)

// DaemonStatus is a deprecated stub. The daemon has been removed.
type DaemonStatus struct {
	Mode               string `json:"mode"`
	Connected          bool   `json:"connected"`
	Degraded           bool   `json:"degraded"`
	SocketPath         string `json:"socket_path,omitempty"`
	AutoStartEnabled   bool   `json:"auto_start_enabled"`
	AutoStartAttempted bool   `json:"auto_start_attempted"`
	AutoStartSucceeded bool   `json:"auto_start_succeeded"`
	FallbackReason     string `json:"fallback_reason,omitempty"`
	Detail             string `json:"detail,omitempty"`
	Health             string `json:"health,omitempty"`
}

// Deprecated fallback reason constants. The daemon has been removed.
const (
	FallbackNone              = "none"
	FallbackFlagNoDaemon      = "flag_no_daemon"
	FallbackConnectFailed     = "connect_failed"
	FallbackHealthFailed      = "health_failed"
	FallbackWorktreeSafety    = "worktree_safety"
	FallbackSingleProcessOnly = "single_process_only"
	FallbackAutoStartDisabled = "auto_start_disabled"
	FallbackAutoStartFailed   = "auto_start_failed"
	FallbackDaemonUnsupported = "daemon_unsupported"
	FallbackWispOperation     = "wisp_operation"
)

var (
	// daemonClient is always nil. The daemon has been removed.
	daemonClient *rpc.Client

	// daemonStatus is always zero-valued. The daemon has been removed.
	daemonStatus DaemonStatus

	// noDaemon is always false. The --no-daemon flag has been removed.
	noDaemon bool
)

// Deprecated accessor stubs for context.go compatibility.
func getDaemonClient() *rpc.Client   { return nil }
func setDaemonClient(_ *rpc.Client)  {}
func getDaemonStatus() DaemonStatus  { return DaemonStatus{} }
func setDaemonStatus(_ DaemonStatus) {}
func isNoDaemon() bool               { return false }
func setNoDaemon(_ bool)             {}

// Deprecated daemon lifecycle stubs.
func isDaemonRunning(_ string) (bool, int) { return false, 0 }
func stopDaemonQuiet(_ string)             {}
func getPIDFilePath() (string, error)      { return "", nil }
func getSocketPath() string                { return "" }
func emitVerboseWarning()                  {}
func shouldAutoStartDaemon() bool          { return false }
func singleProcessOnlyBackend() bool       { return false }
func shouldDisableDaemonForWorktree() bool { return false }
func warnWorktreeDaemon(_ string)          {}
func newSilentLogger() *slog.Logger        { return slog.New(slog.DiscardHandler) }
func syncBranchPull(_ context.Context, _ storage.Storage, _ *slog.Logger) (bool, error) {
	return false, nil
}

func boolToFlag(condition bool, flag string) string {
	if condition {
		return flag
	}
	return ""
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func formatUptime(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%.1f seconds", seconds)
	}
	if seconds < 3600 {
		minutes := int(seconds / 60)
		secs := int(seconds) % 60
		return fmt.Sprintf("%dm %ds", minutes, secs)
	}
	if seconds < 86400 {
		hours := int(seconds / 3600)
		minutes := int(seconds/60) % 60
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	days := int(seconds / 86400)
	hours := int(seconds/3600) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}
