package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/gate"
	"github.com/steveyegge/beads/internal/ui"
)

// sessionGateRegistry is the default registry for session-level gates.
// Built-in gates (decision, commit-push, bead-update) are registered at init.
var sessionGateRegistry = newSessionGateRegistry()

func newSessionGateRegistry() *gate.Registry {
	reg := gate.NewRegistry()
	gate.RegisterBuiltinGates(reg)
	gate.RegisterPreToolUseGates(reg)
	gate.RegisterPreCompactGates(reg)
	gate.RegisterUserPromptSubmitGates(reg)
	gate.RegisterBridgeGates(reg)
	return reg
}

// getSessionID returns the current Claude Code session ID from the environment.
func getSessionID() string {
	return os.Getenv("CLAUDE_SESSION_ID")
}

// getWorkDir returns the working directory for gate marker storage.
func getWorkDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// gateMarkCmd marks a session gate as satisfied.
var gateMarkCmd = &cobra.Command{
	Use:   "mark <gate-id>",
	Short: "Mark a session gate as satisfied",
	Long: `Mark a session gate as satisfied for the current Claude Code session.

The session ID is read from the CLAUDE_SESSION_ID environment variable.
Gate markers are stored in .runtime/gates/<session-id>/<gate-id>.

Examples:
  bd gate mark decision         # Mark decision gate satisfied
  bd gate mark commit-push      # Mark commit-push gate satisfied`,
	Args: cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		gateID := args[0]
		sessionID := getSessionID()
		if sessionID == "" {
			fmt.Fprintln(os.Stderr, "Error: CLAUDE_SESSION_ID not set")
			os.Exit(1)
		}

		workDir := getWorkDir()
		if err := gate.MarkGate(workDir, sessionID, gateID); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"gate_id":    gateID,
				"session_id": sessionID,
				"marked":     true,
			})
			return
		}

		fmt.Printf("%s Gate marked: %s\n", ui.RenderPass("✓"), gateID)
	},
}

// gateClearCmd clears session gate markers.
var gateClearCmd = &cobra.Command{
	Use:   "clear [gate-id]",
	Short: "Clear session gate markers",
	Long: `Clear session gate markers for the current Claude Code session.

Without arguments, clears all gates. With --hook, clears gates for a
specific hook type. With a gate-id argument, clears a specific gate.

Examples:
  bd gate clear                    # Clear all gate markers
  bd gate clear decision           # Clear specific gate marker
  bd gate clear --hook Stop        # Clear all Stop hook gate markers`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		sessionID := getSessionID()
		if sessionID == "" {
			fmt.Fprintln(os.Stderr, "Error: CLAUDE_SESSION_ID not set")
			os.Exit(1)
		}

		workDir := getWorkDir()
		hookFlag, _ := cmd.Flags().GetString("hook")

		switch {
		case len(args) == 1:
			// Clear specific gate
			gate.ClearGate(workDir, sessionID, args[0])
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"gate_id":    args[0],
					"session_id": sessionID,
					"cleared":    true,
				})
				return
			}
			fmt.Printf("%s Gate cleared: %s\n", ui.RenderPass("✓"), args[0])

		case hookFlag != "":
			// Clear all gates for a hook type
			hookType, err := gate.ParseHookType(hookFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			gate.ClearGatesForHook(workDir, sessionID, hookType, sessionGateRegistry)
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"hook":       string(hookType),
					"session_id": sessionID,
					"cleared":    true,
				})
				return
			}
			fmt.Printf("%s Cleared all %s gate markers\n", ui.RenderPass("✓"), hookType)

		default:
			// Clear all gates
			gate.ClearAllGates(workDir, sessionID)
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"session_id": sessionID,
					"cleared":    "all",
				})
				return
			}
			fmt.Printf("%s Cleared all gate markers\n", ui.RenderPass("✓"))
		}
	},
}

// gateSessionCheckCmd checks session gates for a specific hook type.
var gateSessionCheckCmd = &cobra.Command{
	Use:   "session-check --hook <type>",
	Short: "Check session gates for a hook type",
	Long: `Evaluate all registered session gates for a Claude Code hook type.

Returns exit 0 if all strict gates are satisfied (allow).
Returns exit 1 with block JSON if any strict gate is unsatisfied (block).
Soft gates produce warnings but do not block.

The --soft flag makes all gates soft (for autonomous mode).
The --json flag outputs the full check response as JSON.

Hook types: Stop, PreToolUse, UserPromptSubmit, PreCompact

This command is designed to be called from Claude Code hook scripts:
  bd gate session-check --hook Stop --json

Examples:
  bd gate session-check --hook Stop          # Check Stop gates
  bd gate session-check --hook Stop --json   # JSON output for hooks
  bd gate session-check --hook Stop --soft   # Soft mode (warn only)`,
	Run: func(cmd *cobra.Command, _ []string) {
		hookFlag, _ := cmd.Flags().GetString("hook")
		softMode, _ := cmd.Flags().GetBool("soft")

		if hookFlag == "" {
			fmt.Fprintln(os.Stderr, "Error: --hook flag is required")
			os.Exit(1)
		}

		hookType, err := gate.ParseHookType(hookFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		sessionID := getSessionID()
		if sessionID == "" {
			// No session — allow by default
			if jsonOutput {
				outputJSON(gate.CheckResponse{Decision: "allow", Reason: "no session"})
			}
			return
		}

		workDir := getWorkDir()

		// If soft mode, temporarily override gate modes
		reg := sessionGateRegistry
		if softMode {
			reg = softCopyRegistry(reg, hookType)
		}

		resp, err := gate.EvaluateHook(workDir, sessionID, hookType, reg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error evaluating gates: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(resp)
			if resp.Decision == "block" {
				os.Exit(1)
			}
			return
		}

		// Human-readable output
		if resp.Decision == "block" {
			fmt.Printf("%s Blocked: %s\n", ui.RenderFail("✗"), resp.Reason)
			for _, r := range resp.Results {
				if !r.Satisfied && r.Mode == gate.GateModeStrict {
					hint := ""
					if r.Hint != "" {
						hint = fmt.Sprintf(" → %s", r.Hint)
					}
					fmt.Printf("  %s %s: %s%s\n", ui.RenderFail("●"), r.GateID, r.Message, hint)
				}
			}
			os.Exit(1)
		}

		// Warnings
		for _, w := range resp.Warnings {
			fmt.Printf("  %s %s\n", ui.RenderWarn("⚠"), w)
		}

		if len(resp.Warnings) == 0 {
			fmt.Printf("%s All %s gates satisfied\n", ui.RenderPass("✓"), hookType)
		} else {
			fmt.Printf("%s %s gates allow (with %d warnings)\n", ui.RenderPass("✓"), hookType, len(resp.Warnings))
		}
	},
}

// gateStatusCmd shows current session gate status.
var gateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show session gate status",
	Long: `Show the current status of all session gates for debugging.

Displays all registered gates grouped by hook type, showing which
are satisfied and which are pending.

Examples:
  bd gate status                   # Show all gate status
  bd gate status --session abc123  # Show for specific session`,
	Run: func(cmd *cobra.Command, _ []string) {
		sessionFlag, _ := cmd.Flags().GetString("session")

		sessionID := sessionFlag
		if sessionID == "" {
			sessionID = getSessionID()
		}
		if sessionID == "" {
			fmt.Fprintln(os.Stderr, "Error: no session ID (set CLAUDE_SESSION_ID or use --session)")
			os.Exit(1)
		}

		workDir := getWorkDir()

		type gateStatus struct {
			ID          string        `json:"id"`
			Hook        gate.HookType `json:"hook"`
			Mode        gate.GateMode `json:"mode"`
			Satisfied   bool          `json:"satisfied"`
			Description string        `json:"description"`
			Hint        string        `json:"hint,omitempty"`
		}

		var allStatus []gateStatus
		for _, hookType := range gate.ValidHookTypes() {
			gates := sessionGateRegistry.GatesForHook(hookType)
			for _, g := range gates {
				allStatus = append(allStatus, gateStatus{
					ID:          g.ID,
					Hook:        g.Hook,
					Mode:        g.Mode,
					Satisfied:   gate.IsGateSatisfied(workDir, sessionID, g.ID),
					Description: g.Description,
					Hint:        g.Hint,
				})
			}
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"session_id": sessionID,
				"gates":      allStatus,
			})
			return
		}

		if len(allStatus) == 0 {
			fmt.Println("No session gates registered.")
			return
		}

		fmt.Printf("Session: %s\n\n", sessionID)

		// Group by hook type
		currentHook := gate.HookType("")
		for _, s := range allStatus {
			if s.Hook != currentHook {
				currentHook = s.Hook
				fmt.Printf("%s gates:\n", ui.RenderAccent(string(s.Hook)))
			}

			sym := ui.RenderFail("○")
			label := "pending"
			if s.Satisfied {
				sym = ui.RenderPass("●")
				label = "satisfied"
			}

			modeLabel := ""
			if s.Mode == gate.GateModeSoft {
				modeLabel = " (soft)"
			}

			fmt.Printf("  %s %s: %s%s\n", sym, s.ID, label, modeLabel)
			if !s.Satisfied && s.Hint != "" {
				fmt.Printf("    → %s\n", s.Hint)
			}
		}
	},
}

// gateSessionListCmd lists registered session gates.
var gateSessionListCmd = &cobra.Command{
	Use:   "session-list [--hook <type>]",
	Short: "List registered session gates",
	Long: `List all registered session gates, optionally filtered by hook type.

This lists the gate definitions (not their satisfaction status).
Use 'bd gate status' to see which gates are currently satisfied.

Examples:
  bd gate session-list               # List all session gates
  bd gate session-list --hook Stop   # List only Stop gates`,
	Run: func(cmd *cobra.Command, _ []string) {
		hookFlag, _ := cmd.Flags().GetString("hook")

		var gates []*gate.Gate
		if hookFlag != "" {
			hookType, err := gate.ParseHookType(hookFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			gates = sessionGateRegistry.GatesForHook(hookType)
		} else {
			gates = sessionGateRegistry.AllGates()
		}

		if jsonOutput {
			type gateInfo struct {
				ID          string        `json:"id"`
				Hook        gate.HookType `json:"hook"`
				Mode        gate.GateMode `json:"mode"`
				Description string        `json:"description"`
				Hint        string        `json:"hint,omitempty"`
				HasAutoCheck bool         `json:"has_auto_check"`
			}
			var infos []gateInfo
			for _, g := range gates {
				infos = append(infos, gateInfo{
					ID:          g.ID,
					Hook:        g.Hook,
					Mode:        g.Mode,
					Description: g.Description,
					Hint:        g.Hint,
					HasAutoCheck: g.AutoCheck != nil,
				})
			}
			outputJSON(infos)
			return
		}

		if len(gates) == 0 {
			if hookFlag != "" {
				fmt.Printf("No session gates registered for hook %s.\n", hookFlag)
			} else {
				fmt.Println("No session gates registered.")
			}
			return
		}

		fmt.Printf("Session gates (%d):\n\n", len(gates))
		for _, g := range gates {
			modeLabel := "strict"
			if g.Mode == gate.GateModeSoft {
				modeLabel = "soft"
			}
			fmt.Printf("  %s  hook=%s  mode=%s\n", ui.RenderID(g.ID), g.Hook, modeLabel)
			if g.Description != "" {
				fmt.Printf("    %s\n", g.Description)
			}
		}
	},
}

// gateRegisterCmd registers a custom session gate.
var gateRegisterCmd = &cobra.Command{
	Use:   "register <gate-id> --hook <type> --description '...' --mode strict",
	Short: "Register a custom session gate",
	Long: `Register a custom session gate beyond the built-in ones.

Custom gates participate in session-check alongside built-in gates.
They must be marked manually with 'bd gate mark'.

Examples:
  bd gate register my-check --hook Stop --description 'Custom check' --mode strict
  bd gate register review --hook Stop --description 'Code reviewed' --mode soft`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		gateID := args[0]
		hookFlag, _ := cmd.Flags().GetString("hook")
		description, _ := cmd.Flags().GetString("description")
		modeFlag, _ := cmd.Flags().GetString("mode")

		if hookFlag == "" {
			fmt.Fprintln(os.Stderr, "Error: --hook flag is required")
			os.Exit(1)
		}

		hookType, err := gate.ParseHookType(hookFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		mode := gate.GateModeStrict
		if modeFlag == "soft" {
			mode = gate.GateModeSoft
		}

		g := &gate.Gate{
			ID:          gateID,
			Hook:        hookType,
			Description: description,
			Mode:        mode,
		}

		if err := sessionGateRegistry.Register(g); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"gate_id":     gateID,
				"hook":        string(hookType),
				"mode":        string(mode),
				"registered":  true,
			})
			return
		}

		fmt.Printf("%s Registered gate: %s (hook=%s, mode=%s)\n",
			ui.RenderPass("✓"), gateID, hookType, mode)
	},
}

// softCopyRegistry creates a copy of the registry with all gates for the given
// hook type set to soft mode. Used for autonomous/soft mode operation.
func softCopyRegistry(reg *gate.Registry, hookType gate.HookType) *gate.Registry {
	softReg := gate.NewRegistry()
	for _, g := range reg.AllGates() {
		softGate := *g
		if softGate.Hook == hookType {
			softGate.Mode = gate.GateModeSoft
		}
		_ = softReg.Register(&softGate)
	}
	return softReg
}

func init() {
	// gate clear flags
	gateClearCmd.Flags().String("hook", "", "Clear gates for a specific hook type")

	// gate session-check flags
	gateSessionCheckCmd.Flags().String("hook", "", "Hook type to check (required)")
	gateSessionCheckCmd.Flags().Bool("soft", false, "Treat all gates as soft (warn only)")

	// gate status flags
	gateStatusCmd.Flags().String("session", "", "Session ID (default: CLAUDE_SESSION_ID)")

	// gate session-list flags
	gateSessionListCmd.Flags().String("hook", "", "Filter by hook type")

	// gate register flags
	gateRegisterCmd.Flags().String("hook", "", "Hook type (required)")
	gateRegisterCmd.Flags().String("description", "", "Gate description")
	gateRegisterCmd.Flags().String("mode", "strict", "Gate mode: strict or soft")

	// Add subcommands to gateCmd
	gateCmd.AddCommand(gateMarkCmd)
	gateCmd.AddCommand(gateClearCmd)
	gateCmd.AddCommand(gateSessionCheckCmd)
	gateCmd.AddCommand(gateStatusCmd)
	gateCmd.AddCommand(gateSessionListCmd)
	gateCmd.AddCommand(gateRegisterCmd)
}

// readStdinHookInput reads and parses JSON from stdin for hook input.
// Returns parsed fields or nil if no stdin available.
func readStdinHookInput() map[string]interface{} {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		// stdin is a terminal, not piped
		return nil
	}

	var input map[string]interface{}
	dec := json.NewDecoder(os.Stdin)
	if err := dec.Decode(&input); err != nil {
		return nil
	}
	return input
}

// formatGateResults formats gate results as a human-readable string.
func formatGateResults(results []gate.GateResult) string {
	var parts []string
	for _, r := range results {
		status := "○"
		if r.Satisfied {
			status = "●"
		}
		parts = append(parts, fmt.Sprintf("%s %s", status, r.GateID))
	}
	return strings.Join(parts, ", ")
}
