package main

import (
	"testing"
)

// TestAgentPodRegisterCmd_FlagDefinitions verifies that the pod-register command
// has all expected flags defined with correct names.
func TestAgentPodRegisterCmd_FlagDefinitions(t *testing.T) {
	flags := agentPodRegisterCmd.Flags()

	expectedFlags := []struct {
		name     string
		required bool
	}{
		{"pod-name", true},
		{"pod-ip", false},
		{"pod-node", false},
		{"status", false},
		{"screen", false},
	}

	for _, ef := range expectedFlags {
		f := flags.Lookup(ef.name)
		if f == nil {
			t.Errorf("expected flag %q not found on pod-register command", ef.name)
			continue
		}
	}
}

// TestAgentPodRegisterCmd_RequiresExactlyOneArg verifies the argument validation.
func TestAgentPodRegisterCmd_RequiresExactlyOneArg(t *testing.T) {
	if agentPodRegisterCmd.Args == nil {
		t.Fatal("pod-register should have Args validation")
	}
	// cobra.ExactArgs(1) should reject 0 args
	if err := agentPodRegisterCmd.Args(agentPodRegisterCmd, []string{}); err == nil {
		t.Error("expected error with 0 args")
	}
	// Should accept exactly 1 arg
	if err := agentPodRegisterCmd.Args(agentPodRegisterCmd, []string{"agent-1"}); err != nil {
		t.Errorf("expected no error with 1 arg, got: %v", err)
	}
	// Should reject 2 args
	if err := agentPodRegisterCmd.Args(agentPodRegisterCmd, []string{"a", "b"}); err == nil {
		t.Error("expected error with 2 args")
	}
}

// TestAgentPodDeregisterCmd_RequiresExactlyOneArg verifies the argument validation.
func TestAgentPodDeregisterCmd_RequiresExactlyOneArg(t *testing.T) {
	if agentPodDeregisterCmd.Args == nil {
		t.Fatal("pod-deregister should have Args validation")
	}
	if err := agentPodDeregisterCmd.Args(agentPodDeregisterCmd, []string{}); err == nil {
		t.Error("expected error with 0 args")
	}
	if err := agentPodDeregisterCmd.Args(agentPodDeregisterCmd, []string{"agent-1"}); err != nil {
		t.Errorf("expected no error with 1 arg, got: %v", err)
	}
}

// TestAgentPodStatusCmd_FlagDefinitions verifies that pod-status has expected flags.
func TestAgentPodStatusCmd_FlagDefinitions(t *testing.T) {
	f := agentPodStatusCmd.Flags().Lookup("status")
	if f == nil {
		t.Error("expected --status flag on pod-status command")
	}
}

// TestAgentPodStatusCmd_RequiresExactlyOneArg verifies the argument validation.
func TestAgentPodStatusCmd_RequiresExactlyOneArg(t *testing.T) {
	if agentPodStatusCmd.Args == nil {
		t.Fatal("pod-status should have Args validation")
	}
	if err := agentPodStatusCmd.Args(agentPodStatusCmd, []string{}); err == nil {
		t.Error("expected error with 0 args")
	}
	if err := agentPodStatusCmd.Args(agentPodStatusCmd, []string{"agent-1"}); err != nil {
		t.Errorf("expected no error with 1 arg, got: %v", err)
	}
}

// TestAgentPodListCmd_FlagDefinitions verifies that pod-list has expected flags.
func TestAgentPodListCmd_FlagDefinitions(t *testing.T) {
	f := agentPodListCmd.Flags().Lookup("rig")
	if f == nil {
		t.Error("expected --rig flag on pod-list command")
	}
}

// TestAgentPodListCmd_AcceptsNoArgs verifies pod-list works without args.
func TestAgentPodListCmd_AcceptsNoArgs(t *testing.T) {
	// pod-list has no Args validation (accepts 0 args by default)
	if agentPodListCmd.Args != nil {
		if err := agentPodListCmd.Args(agentPodListCmd, []string{}); err != nil {
			t.Errorf("pod-list should accept 0 args, got: %v", err)
		}
	}
}

// TestAgentPodCommands_AreSubcommands verifies all pod commands are registered
// as subcommands of agentCmd.
func TestAgentPodCommands_AreSubcommands(t *testing.T) {
	subCmds := agentCmd.Commands()
	expectedCmds := map[string]bool{
		"pod-register":   false,
		"pod-deregister": false,
		"pod-status":     false,
		"pod-list":       false,
	}

	for _, cmd := range subCmds {
		if _, ok := expectedCmds[cmd.Name()]; ok {
			expectedCmds[cmd.Name()] = true
		}
	}

	for name, found := range expectedCmds {
		if !found {
			t.Errorf("expected subcommand %q not found under agentCmd", name)
		}
	}
}

// TestAgentPodRegisterCmd_RequiresDaemon verifies the command fails gracefully
// when no daemon client is available.
func TestAgentPodRegisterCmd_RequiresDaemon(t *testing.T) {
	ensureTestMode(t)
	ensureCleanGlobalState(t)

	// Ensure daemonClient is nil (simulates no daemon running)
	oldClient := daemonClient
	daemonClient = nil
	defer func() { daemonClient = oldClient }()

	err := runAgentPodRegister(agentPodRegisterCmd, []string{"test-agent"})
	if err == nil {
		t.Error("expected error when daemon is not available")
	}
	if err.Error() != "agent pod-register requires the daemon (set BD_DAEMON_HOST)" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestAgentPodDeregisterCmd_RequiresDaemon verifies the command fails gracefully
// when no daemon client is available.
func TestAgentPodDeregisterCmd_RequiresDaemon(t *testing.T) {
	ensureTestMode(t)
	ensureCleanGlobalState(t)

	oldClient := daemonClient
	daemonClient = nil
	defer func() { daemonClient = oldClient }()

	err := runAgentPodDeregister(agentPodDeregisterCmd, []string{"test-agent"})
	if err == nil {
		t.Error("expected error when daemon is not available")
	}
	if err.Error() != "agent pod-deregister requires the daemon (set BD_DAEMON_HOST)" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestAgentPodStatusCmd_RequiresDaemon verifies the command fails gracefully
// when no daemon client is available.
func TestAgentPodStatusCmd_RequiresDaemon(t *testing.T) {
	ensureTestMode(t)
	ensureCleanGlobalState(t)

	oldClient := daemonClient
	daemonClient = nil
	defer func() { daemonClient = oldClient }()

	err := runAgentPodStatus(agentPodStatusCmd, []string{"test-agent"})
	if err == nil {
		t.Error("expected error when daemon is not available")
	}
	if err.Error() != "agent pod-status requires the daemon (set BD_DAEMON_HOST)" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestAgentPodListCmd_RequiresDaemon verifies the command fails gracefully
// when no daemon client is available.
func TestAgentPodListCmd_RequiresDaemon(t *testing.T) {
	ensureTestMode(t)
	ensureCleanGlobalState(t)

	oldClient := daemonClient
	daemonClient = nil
	defer func() { daemonClient = oldClient }()

	err := runAgentPodList(agentPodListCmd, []string{})
	if err == nil {
		t.Error("expected error when daemon is not available")
	}
	if err.Error() != "agent pod-list requires the daemon (set BD_DAEMON_HOST)" {
		t.Errorf("unexpected error message: %v", err)
	}
}
