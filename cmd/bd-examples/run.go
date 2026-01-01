package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	runExecute     bool
	runTimeout     time.Duration
	runInteractive bool
	runArgs        string
)

var runCmd = &cobra.Command{
	Use:   "run <script>",
	Short: "Run an example script with safety controls",
	Long: `Run an example script with dry-run mode by default.

By default, scripts run in dry-run mode which intercepts state-modifying
commands (bd update, bd close, etc.) and shows what would happen without
making changes.

Examples:
  bd-examples run bash-agent/agent.sh          # Dry-run (default)
  bd-examples run bash-agent/agent.sh --execute # Actually run it
  bd-examples run compaction/auto-compact.sh   # Uses native --dry-run
  bd-examples run workflow.sh --interactive    # Allow prompts`,
	Args: cobra.ExactArgs(1),
	RunE: runScript,
}

func init() {
	runCmd.Flags().BoolVar(&runExecute, "execute", false, "Actually execute (disables dry-run)")
	runCmd.Flags().DurationVar(&runTimeout, "timeout", 5*time.Minute, "Timeout for script execution")
	runCmd.Flags().BoolVar(&runInteractive, "interactive", false, "Allow interactive prompts")
	runCmd.Flags().StringVar(&runArgs, "args", "", "Arguments to pass to the script")
}

func runScript(cmd *cobra.Command, args []string) error {
	scriptPath := args[0]

	// Find the script in registry
	script := GetScript(scriptPath)
	if script == nil {
		return fmt.Errorf("unknown script: %s (use 'bd-examples list' to see available scripts)", scriptPath)
	}

	// Check prerequisites first
	if verbose {
		fmt.Println(mutedStyle.Render("Checking prerequisites..."))
	}
	for _, prereq := range script.Prerequisites {
		result := checkPrereq(prereq)
		if result.Status == "fail" {
			return fmt.Errorf("prerequisite not met: %s - %s", prereq, result.Message)
		}
	}

	// Check interactive requirement
	if script.Interactive && !runInteractive {
		return fmt.Errorf("script %s requires interactive input\nUse --interactive to allow prompts", scriptPath)
	}

	// Check if script is blocked
	if script.DryRunMode == DryRunBlock && !runExecute {
		return fmt.Errorf("script %s is blocked in dry-run mode (performs dangerous operations)\nUse --execute to run for real", scriptPath)
	}

	// Find examples directory
	exDir, err := findExamplesDir()
	if err != nil {
		return err
	}

	fullPath := filepath.Join(exDir, scriptPath)
	if _, err := os.Stat(fullPath); err != nil {
		return fmt.Errorf("script not found: %s", fullPath)
	}

	// Determine execution mode
	dryRun := !runExecute
	if dryRun {
		fmt.Printf("%s %s\n", warnStyle.Render("[DRY-RUN]"), accentStyle.Render(scriptPath))
	} else {
		fmt.Printf("%s %s\n", passStyle.Render("[EXECUTE]"), accentStyle.Render(scriptPath))
	}

	// Build command based on dry-run mode
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	var bashCmd *exec.Cmd

	switch script.DryRunMode {
	case DryRunNative:
		// Script has --dry-run flag, add it if in dry-run mode
		scriptArgs := script.DefaultArgs
		if runArgs != "" {
			scriptArgs = append(scriptArgs, strings.Fields(runArgs)...)
		}
		if dryRun {
			// DefaultArgs should already include --dry-run for native scripts
			bashCmd = exec.CommandContext(ctx, "bash", append([]string{fullPath}, scriptArgs...)...)
		} else {
			// Remove --dry-run from args if executing for real
			var realArgs []string
			for _, a := range scriptArgs {
				if a != "--dry-run" {
					realArgs = append(realArgs, a)
				}
			}
			bashCmd = exec.CommandContext(ctx, "bash", append([]string{fullPath}, realArgs...)...)
		}

	case DryRunIntercept:
		// Wrap with interceptor
		if dryRun {
			// Create a wrapper script that sources interceptor then runs the target
			wrapperScript := DryRunPrefix() + "\n" + fmt.Sprintf("source %q", fullPath)
			bashCmd = exec.CommandContext(ctx, "bash", "-c", wrapperScript)
		} else {
			bashCmd = exec.CommandContext(ctx, "bash", fullPath)
		}

	case DryRunSafe:
		// No wrapping needed
		bashCmd = exec.CommandContext(ctx, "bash", fullPath)

	case DryRunBlock:
		// Already checked above, but handle anyway
		bashCmd = exec.CommandContext(ctx, "bash", fullPath)
	}

	// Set up output streaming
	bashCmd.Dir = exDir // Run from examples directory

	if runInteractive {
		bashCmd.Stdin = os.Stdin
		bashCmd.Stdout = os.Stdout
		bashCmd.Stderr = os.Stderr
		return bashCmd.Run()
	}

	// Non-interactive: stream with timestamps
	stdout, err := bashCmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := bashCmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := bashCmd.Start(); err != nil {
		return err
	}

	// Stream output with timestamps
	done := make(chan bool, 2)
	go streamOutput(stdout, "[STDOUT]", done)
	go streamOutput(stderr, "[STDERR]", done)

	// Wait for both streams to finish
	<-done
	<-done

	err = bashCmd.Wait()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("script timed out after %v", runTimeout)
		}
		return fmt.Errorf("script failed: %v", err)
	}

	fmt.Printf("\n%s\n", passStyle.Render("Script completed successfully"))
	return nil
}

func streamOutput(r io.Reader, prefix string, done chan<- bool) {
	defer func() { done <- true }()

	scanner := bufio.NewScanner(r)
	prefixStyle := mutedStyle
	if prefix == "[STDERR]" {
		prefixStyle = warnStyle
	}

	for scanner.Scan() {
		line := scanner.Text()
		timestamp := time.Now().Format("15:04:05")
		fmt.Printf("%s %s %s\n",
			mutedStyle.Render("["+timestamp+"]"),
			prefixStyle.Render(prefix),
			line,
		)
	}
}
