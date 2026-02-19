package main

import "testing"

func TestFlowParityCommands(t *testing.T) {
	if flowCmd == nil {
		t.Fatalf("flowCmd is nil")
	}

	expected := map[string]struct{}{
		"claim-next":         {},
		"create-discovered":  {},
		"block-with-context": {},
		"close-safe":         {},
	}

	seen := make(map[string]struct{}, len(flowCmd.Commands()))
	for _, cmd := range flowCmd.Commands() {
		seen[cmd.Name()] = struct{}{}
	}

	for name := range expected {
		if _, ok := seen[name]; !ok {
			t.Fatalf("flow command missing subcommand %q", name)
		}
	}

	if got, want := len(seen), len(expected); got < want {
		t.Fatalf("flow command exposes %d subcommands, want at least %d", got, want)
	}
}
