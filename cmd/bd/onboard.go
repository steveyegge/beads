package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
)

const (
	onboardTemplateMinimal      = "minimal"
	onboardTemplateControlFlow  = "control-flow"
	onboardTemplateSplitControl = "control-flow-split"
)

var onboardTemplate string

const copilotInstructionsContent = `# GitHub Copilot Instructions

## Issue Tracking

This project uses **bd (beads)** for issue tracking.
Run ` + "`bd prime`" + ` for workflow context, or install hooks (` + "`bd hooks install`" + `) for auto-injection.

**Quick reference:**
- ` + "`bd ready`" + ` - Find unblocked work
- ` + "`bd create \"Title\" --type task --priority 2`" + ` - Create issue
- ` + "`bd close <id>`" + ` - Complete work
- ` + "`bd sync`" + ` - Sync with git (run at session end)

For full workflow details: ` + "`bd prime`" + ``

const agentsContent = `## Issue Tracking

This project uses **bd (beads)** for issue tracking.
Run ` + "`bd prime`" + ` for workflow context, or install hooks (` + "`bd hooks install`" + `) for auto-injection.

**Quick reference:**
- ` + "`bd ready`" + ` - Find unblocked work
- ` + "`bd create \"Title\" --type task --priority 2`" + ` - Create issue
- ` + "`bd close <id>`" + ` - Complete work
- ` + "`bd sync`" + ` - Sync with git (run at session end)

For full workflow details: ` + "`bd prime`" + ``

const agentsControlFlowContent = `# AGENTS.md — Control-Flow Kernel

## Authority Order
1. user instruction
2. ` + "`bd <cmd> --help`" + ` and source-truth command behavior
3. this AGENTS.md control flow

## Session Boot
- Run ` + "`bd prime`" + `.
- Run ` + "`bd preflight gate --action claim --json`" + `.
- If gate fails, remediate before claim/write.

## Deterministic Loop
1. Pick work from ` + "`bd ready`" + `.
2. Pre-claim lint: ` + "`bd flow preclaim-lint --issue <id>`" + `.
3. Claim with WIP gate: ` + "`bd flow claim-next ...`" + `.
4. Execute and verify.
5. Close safely: ` + "`bd flow close-safe --issue <id> --reason \"<safe reason>\" --verified \"<command + result>\"`" + `.
6. If no scoped ready work: ` + "`bd recover loop ...`" + ` then ` + "`bd recover signature ...`" + `.
7. Land with ` + "`bd land ...`" + `.

## Hard Rules
- Use ` + "`bd ready`" + ` as the only claim queue.
- Keep WIP at 1 per actor.
- Record verification evidence before close.
- Use keyword-safe close reasons.
- Prefer deterministic wrappers (` + "`flow`" + `, ` + "`intake`" + `, ` + "`preflight`" + `, ` + "`recover`" + `, ` + "`land`" + `).

## Contract
See ` + "`docs/CONTROL_PLANE_CONTRACT.md`" + ` for command/result envelope semantics.`

const agentsSplitControlFlowContent = `# AGENTS.md — Split-Agent Control Flow

## Boundary
- CLI owns deterministic lifecycle and policy enforcement.
- Agent docs own judgment and sequencing tradeoffs.

## Required Deterministic Commands
- ` + "`bd preflight gate --action claim --json`" + `
- ` + "`bd flow claim-next ...`" + `
- ` + "`bd flow close-safe ... --verified ...`" + `
- ` + "`bd intake audit --epic <id> --write-proof --json`" + ` (when intake hard gate applies)
- ` + "`bd recover loop ...`" + ` and ` + "`bd recover signature ...`" + `
- ` + "`bd land ...`" + `

## Split Role Docs
- ` + "`docs/agents/planner.md`" + `
- ` + "`docs/agents/executor.md`" + `
- ` + "`docs/agents/reviewer.md`" + `

## Contract
See ` + "`docs/CONTROL_PLANE_CONTRACT.md`" + ` for deterministic envelopes and result-state semantics.`

func resolveOnboardTemplate(name string) (string, string, error) {
	template := strings.TrimSpace(strings.ToLower(name))
	switch template {
	case "", onboardTemplateMinimal:
		return onboardTemplateMinimal, agentsContent, nil
	case onboardTemplateControlFlow:
		return onboardTemplateControlFlow, agentsControlFlowContent, nil
	case onboardTemplateSplitControl:
		return onboardTemplateSplitControl, agentsSplitControlFlowContent, nil
	default:
		return "", "", fmt.Errorf("unsupported --template %q (supported: %s, %s, %s)", name, onboardTemplateMinimal, onboardTemplateControlFlow, onboardTemplateSplitControl)
	}
}

func renderOnboardInstructions(w io.Writer, templateName string) error {
	writef := func(format string, args ...interface{}) error {
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	writeln := func(text string) error {
		_, err := fmt.Fprintln(w, text)
		return err
	}
	writeBlank := func() error {
		_, err := fmt.Fprintln(w)
		return err
	}

	resolvedTemplate, resolvedAgentsContent, err := resolveOnboardTemplate(templateName)
	if err != nil {
		return err
	}

	if err := writef("\n%s\n\n", ui.RenderBold("bd Onboarding")); err != nil {
		return err
	}
	if err := writef("Selected template: %s\n", ui.RenderAccent(resolvedTemplate)); err != nil {
		return err
	}
	if err := writeln("Add this snippet to AGENTS.md (or create it):"); err != nil {
		return err
	}
	if err := writeBlank(); err != nil {
		return err
	}

	if err := writef("%s\n", ui.RenderAccent("--- BEGIN AGENTS.MD CONTENT ---")); err != nil {
		return err
	}
	if err := writeln(resolvedAgentsContent); err != nil {
		return err
	}
	if err := writef("%s\n\n", ui.RenderAccent("--- END AGENTS.MD CONTENT ---")); err != nil {
		return err
	}

	if err := writef("%s\n", ui.RenderBold("For GitHub Copilot users:")); err != nil {
		return err
	}
	if err := writeln("Add the same content to .github/copilot-instructions.md"); err != nil {
		return err
	}
	if err := writeBlank(); err != nil {
		return err
	}

	if err := writef("%s\n", ui.RenderBold("How it works:")); err != nil {
		return err
	}
	if err := writef("   • %s provides dynamic workflow context (~80 lines)\n", ui.RenderAccent("bd prime")); err != nil {
		return err
	}
	if err := writef("   • %s auto-injects bd prime at session start\n", ui.RenderAccent("bd hooks install")); err != nil {
		return err
	}
	if err := writeln("   • Pick minimal template for lean setups, control-flow templates for deterministic execution policy"); err != nil {
		return err
	}
	if err := writeBlank(); err != nil {
		return err
	}

	if err := writef("%s\n\n", ui.RenderPass("Use `bd onboard --template <name>` to switch template tiers at any time.")); err != nil {
		return err
	}

	return nil
}

var onboardCmd = &cobra.Command{
	Use:     "onboard",
	GroupID: "setup",
	Short:   "Display AGENTS.md onboarding snippets",
	Long: `Display AGENTS.md snippets for bd integration.

Use --template to select the policy tier:
  • minimal             -> lean pointer to bd prime
  • control-flow        -> single-file deterministic control-flow kernel
  • control-flow-split  -> split-agent control-flow boundaries

Hooks auto-inject bd prime at session start.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := renderOnboardInstructions(cmd.OutOrStdout(), onboardTemplate); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
		}
	},
}

func init() {
	onboardCmd.Flags().StringVar(&onboardTemplate, "template", onboardTemplateMinimal, "Template tier: minimal | control-flow | control-flow-split")
	rootCmd.AddCommand(onboardCmd)
}
