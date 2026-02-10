package main

import (
	"testing"
)

// TestAgentAttachCmd_FlagDefinitions verifies that the attach command
// has all expected flags defined with correct names.
func TestAgentAttachCmd_FlagDefinitions(t *testing.T) {
	flags := agentAttachCmd.Flags()

	expectedFlags := []string{"coop-port", "local-port", "url"}

	for _, name := range expectedFlags {
		f := flags.Lookup(name)
		if f == nil {
			t.Errorf("expected flag %q not found on attach command", name)
		}
	}
}

// TestAgentAttachCmd_RequiresExactlyOneArg verifies the argument validation.
func TestAgentAttachCmd_RequiresExactlyOneArg(t *testing.T) {
	if agentAttachCmd.Args == nil {
		t.Fatal("attach should have Args validation")
	}
	if err := agentAttachCmd.Args(agentAttachCmd, []string{}); err == nil {
		t.Error("expected error with 0 args")
	}
	if err := agentAttachCmd.Args(agentAttachCmd, []string{"agent-1"}); err != nil {
		t.Errorf("expected no error with 1 arg, got: %v", err)
	}
	if err := agentAttachCmd.Args(agentAttachCmd, []string{"a", "b"}); err == nil {
		t.Error("expected error with 2 args")
	}
}

// TestAgentAttachCmd_IsSubcommand verifies attach is registered under agentCmd.
func TestAgentAttachCmd_IsSubcommand(t *testing.T) {
	found := false
	for _, cmd := range agentCmd.Commands() {
		if cmd.Name() == "attach" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'attach' subcommand under agentCmd")
	}
}

// TestAgentAttachCmd_DefaultCoopPort verifies the default coop-port flag value.
func TestAgentAttachCmd_DefaultCoopPort(t *testing.T) {
	f := agentAttachCmd.Flags().Lookup("coop-port")
	if f == nil {
		t.Fatal("coop-port flag not found")
	}
	if f.DefValue != "3000" {
		t.Errorf("coop-port default = %q, want '3000'", f.DefValue)
	}
}

// TestAgentAttachCmd_DefaultLocalPort verifies local-port defaults to 0 (auto).
func TestAgentAttachCmd_DefaultLocalPort(t *testing.T) {
	f := agentAttachCmd.Flags().Lookup("local-port")
	if f == nil {
		t.Fatal("local-port flag not found")
	}
	if f.DefValue != "0" {
		t.Errorf("local-port default = %q, want '0' (auto)", f.DefValue)
	}
}

// TestFindFreePort verifies findFreePort returns a valid port.
func TestFindFreePort(t *testing.T) {
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("findFreePort error: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Errorf("port = %d, want 1-65535", port)
	}
}

// TestIsReachable_LocalhostNotListening verifies isReachable returns false
// for a port that's not listening.
func TestIsReachable_LocalhostNotListening(t *testing.T) {
	// Use a port that's almost certainly not listening
	if isReachable("127.0.0.1", 59999) {
		t.Error("expected port 59999 to not be reachable")
	}
}
