package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
)

// ---------------------------------------------------------------------------
// outputEmitResult tests
// ---------------------------------------------------------------------------

func TestOutputEmitResult_InjectOnly(t *testing.T) {
	// Capture stdout.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	result := &rpc.BusEmitResult{
		Inject: []string{"line1", "line2"},
	}
	if err := outputEmitResult(result); err != nil {
		os.Stdout = oldStdout
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if got != "line1\nline2\n" {
		t.Errorf("stdout = %q, want %q", got, "line1\nline2\n")
	}
}

func TestOutputEmitResult_WarningsOnly(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	result := &rpc.BusEmitResult{
		Warnings: []string{"warn1", "warn2"},
	}
	if err := outputEmitResult(result); err != nil {
		os.Stdout = oldStdout
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	want := "<system-reminder>warn1</system-reminder>\n<system-reminder>warn2</system-reminder>\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestOutputEmitResult_InjectAndWarnings(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	result := &rpc.BusEmitResult{
		Inject:   []string{"injected"},
		Warnings: []string{"warning"},
	}
	if err := outputEmitResult(result); err != nil {
		os.Stdout = oldStdout
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	want := "injected\n<system-reminder>warning</system-reminder>\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestOutputEmitResult_Empty(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	result := &rpc.BusEmitResult{}
	if err := outputEmitResult(result); err != nil {
		os.Stdout = oldStdout
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
}

func TestOutputEmitResult_BlockWritesStderr(t *testing.T) {
	// os.Exit(2) cannot be caught in-process. Run the block path in a subprocess.
	if os.Getenv("TEST_BLOCK_OUTPUT") == "1" {
		outputEmitResult(&rpc.BusEmitResult{Block: true, Reason: "gate failed"})
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputEmitResult_BlockWritesStderr$")
	cmd.Env = append(os.Environ(), "TEST_BLOCK_OUTPUT=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Expect a non-zero exit.
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2", exitErr.ExitCode())
	}

	// Verify stderr contains the expected JSON.
	var parsed map[string]string
	// stderr may contain other output; find the JSON line.
	for _, line := range strings.Split(stderr.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") {
			if json.Unmarshal([]byte(line), &parsed) == nil {
				break
			}
		}
	}
	if parsed["decision"] != "block" {
		t.Errorf("decision = %q, want %q", parsed["decision"], "block")
	}
	if parsed["reason"] != "gate failed" {
		t.Errorf("reason = %q, want %q", parsed["reason"], "gate failed")
	}
}

// ---------------------------------------------------------------------------
// runBusEmit local fallback tests
// ---------------------------------------------------------------------------

// newBusEmitCmd creates a minimal cobra command wired to runBusEmit, with the
// --hook flag registered. This avoids depending on the full command tree.
func newBusEmitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "emit",
		RunE: runBusEmit,
	}
	cmd.Flags().String("hook", "", "Hook event type")
	return cmd
}

func TestRunBusEmit_MissingHookFlag(t *testing.T) {
	// Save and restore the global daemonClient.
	oldClient := daemonClient
	daemonClient = nil
	defer func() { daemonClient = oldClient }()

	cmd := newBusEmitCmd()
	// Do not set --hook; the flag defaults to "".
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --hook flag, got nil")
	}
	if !strings.Contains(err.Error(), "--hook flag is required") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "--hook flag is required")
	}
}

func TestRunBusEmit_LocalFallbackPassthrough(t *testing.T) {
	// Save and restore globals.
	oldClient := daemonClient
	daemonClient = nil
	defer func() { daemonClient = oldClient }()

	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	// Provide valid JSON on stdin.
	stdinJSON := `{"session_id":"test-session","cwd":"/tmp"}`
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdinW.WriteString(stdinJSON); err != nil {
		t.Fatal(err)
	}
	stdinW.Close()
	os.Stdin = stdinR

	// Capture stdout.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutW

	cmd := newBusEmitCmd()
	cmd.SetArgs([]string{"--hook", "SessionStart"})
	if err := cmd.Execute(); err != nil {
		os.Stdout = oldStdout
		t.Fatalf("unexpected error: %v", err)
	}

	stdoutW.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(stdoutR); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	// Local bus with no handlers → passthrough → no inject, no warnings, no block.
	if got != "" {
		t.Errorf("stdout = %q, want empty (passthrough)", got)
	}
}

func TestRunBusEmit_LocalFallbackEmptyStdin(t *testing.T) {
	// Save and restore globals.
	oldClient := daemonClient
	daemonClient = nil
	defer func() { daemonClient = oldClient }()

	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	// Empty stdin.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdinW.Close()
	os.Stdin = stdinR

	// Capture stdout.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutW

	cmd := newBusEmitCmd()
	cmd.SetArgs([]string{"--hook", "PreToolUse"})
	if err := cmd.Execute(); err != nil {
		os.Stdout = oldStdout
		t.Fatalf("unexpected error: %v", err)
	}

	stdoutW.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(stdoutR); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if got != "" {
		t.Errorf("stdout = %q, want empty (empty stdin passthrough)", got)
	}
}
