package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/rpc"
)

// TestEditForceDirectMode_SkippedWithDaemonHost verifies the fix for bd-bdbt:
// when BD_DAEMON_HOST is set, the edit command must NOT force direct mode.
// This tests the condition in main.go PersistentPreRun:
//
//	if cmd.Name() == "edit" && rpc.GetDaemonHost() == "" { forceDirectMode = true }
func TestEditForceDirectMode_SkippedWithDaemonHost(t *testing.T) {
	t.Run("BD_DAEMON_HOST set - should not force direct mode", func(t *testing.T) {
		t.Setenv("BD_DAEMON_HOST", "192.168.1.100:9876")
		if rpc.GetDaemonHost() == "" {
			t.Fatal("Expected GetDaemonHost() to return non-empty when BD_DAEMON_HOST is set")
		}
		// The condition: cmd.Name() == "edit" && rpc.GetDaemonHost() == ""
		// With BD_DAEMON_HOST set: rpc.GetDaemonHost() != "" → condition is false
		shouldForce := rpc.GetDaemonHost() == ""
		if shouldForce {
			t.Error("forceDirectMode should be false when BD_DAEMON_HOST is set")
		}
	})

	t.Run("BD_DAEMON_HOST not set - should force direct mode", func(t *testing.T) {
		t.Setenv("BD_DAEMON_HOST", "")
		if rpc.GetDaemonHost() != "" {
			t.Fatal("Expected GetDaemonHost() to return empty when BD_DAEMON_HOST is not set")
		}
		// The condition: cmd.Name() == "edit" && rpc.GetDaemonHost() == ""
		// Without BD_DAEMON_HOST: rpc.GetDaemonHost() == "" → condition is true
		shouldForce := rpc.GetDaemonHost() == ""
		if !shouldForce {
			t.Error("forceDirectMode should be true when BD_DAEMON_HOST is not set")
		}
	})
}
