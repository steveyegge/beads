package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/wobble"
)

var wobbleCmd = &cobra.Command{
	Use:   "wobble",
	Short: "Detect skill drift before it breaks workflows",
	Long: `Wobble analyzes Claude skills for stability and drift risk.

Based on Anthropic's "Hot Mess of AI" paper on AI incoherence.
Skills with ambiguous defaults or missing "DO NOT IMPROVISE" warnings
are more likely to exhibit execution drift.

Examples:
  bd wobble scan beads              # Analyze single skill
  bd wobble scan --all              # Rank all skills by risk
  bd wobble scan --all --top 10     # Show top 10 riskiest
  bd wobble inspect .               # Inspect current project
  bd wobble inspect /path/to/proj   # Inspect specific project`,
	GroupID: GroupMaintenance,
}

var wobbleScanCmd = &cobra.Command{
	Use:   "scan [skill]",
	Short: "Scan skills for wobble risk",
	Long: `Scan skills and analyze them for drift risk.

Without --all, scans a single named skill.
With --all, scans all skills and ranks by combined risk.
With --from-sessions, uses REAL data from Claude session transcripts.

Risk factors checked:
  - No "EXECUTE NOW" section
  - No bash code block
  - Numbered steps (ambiguity)
  - Options without "(default)" marker
  - Content too long (>4000 chars)
  - Missing "DO NOT IMPROVISE" warning
  - Multiple actions without clear default

Examples:
  bd wobble scan beads                    # Analyze with simulation
  bd wobble scan --from-sessions          # Use REAL session data
  bd wobble scan beads --from-sessions    # Real data for specific skill`,
	Args: cobra.MaximumNArgs(1),
	Run:  runWobbleScan,
}

var wobbleInspectCmd = &cobra.Command{
	Use:   "inspect [path]",
	Short: "Inspect a project for wobble readiness",
	Long: `Inspect a project's Claude configuration and analyze skill stability.

Shows inventory of skills, rules, agents, and hooks.
Analyzes each skill for structural risk factors.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runWobbleInspect,
}

func init() {
	// Scan command flags
	wobbleScanCmd.Flags().Bool("all", false, "Scan all skills and rank by risk")
	wobbleScanCmd.Flags().Int("runs", 10, "Number of simulation runs for behavioral analysis")
	wobbleScanCmd.Flags().Int("top", 10, "Show top N riskiest skills (with --all)")
	wobbleScanCmd.Flags().String("project", "", "Project path to scan (uses project-local skills)")
	wobbleScanCmd.Flags().Bool("from-sessions", false, "Use REAL data from Claude session transcripts")
	wobbleScanCmd.Flags().Int("days", 7, "Days of session history to analyze (with --from-sessions)")

	// Inspect command flags
	wobbleInspectCmd.Flags().Bool("fix", false, "Show how to fix issues")

	// Add subcommands
	wobbleCmd.AddCommand(wobbleScanCmd)
	wobbleCmd.AddCommand(wobbleInspectCmd)
	rootCmd.AddCommand(wobbleCmd)
}

func runWobbleScan(cmd *cobra.Command, args []string) {
	scanAll, _ := cmd.Flags().GetBool("all")
	runs, _ := cmd.Flags().GetInt("runs")
	topN, _ := cmd.Flags().GetInt("top")
	projectPath, _ := cmd.Flags().GetString("project")
	fromSessions, _ := cmd.Flags().GetBool("from-sessions")
	days, _ := cmd.Flags().GetInt("days")
	actor := getPacmanAgentName()
	generatedAt := time.Now().UTC()

	applyWobbleSessionConfig()

	skillsDir := wobble.DetectSkillsDir(projectPath)

	preferSessions := config.GetBool("wobble.prefer_sessions")

	// Real session data mode
	if fromSessions || (preferSessions && wobble.HasSessionData(days)) {
		var skillFilter string
		if len(args) > 0 {
			skillFilter = args[0]
		}

		if !jsonOutput {
			fmt.Printf("Parsing Claude session transcripts (last %d days)...\n", days)
		}

		results, err := wobble.ScanFromSessions(skillsDir, skillFilter, days)
		if err != nil {
			FatalErrorRespectJSON("session scan failed: %v", err)
		}

		if len(results) == 0 {
			if !jsonOutput {
				fmt.Println()
				fmt.Println("No skill invocations found in session transcripts.")
				fmt.Println("This could mean:")
				fmt.Printf("- No sessions in the last %d days\n", days)
				fmt.Println("- Session files are stored elsewhere")
				fmt.Println("- No skills were invoked")
				fmt.Printf("\nChecked: %s\n", wobble.SessionsDir)
				fmt.Printf("        %s\n", wobble.ProjectsDir)
				fmt.Println("\nFalling back to simulated analysis...")
			}

			// Fall back to structural-only scan
			if skillFilter != "" {
				result, err := wobble.ScanSkill(skillsDir, skillFilter, 0)
				if err != nil {
					FatalErrorRespectJSON("scan failed: %v", err)
				}
				if err := persistWobbleScan(wobbleSkillsFromScanResult(result, skillsDir), generatedAt, actor); err != nil && !jsonOutput {
					fmt.Fprintf(os.Stderr, "%v\n", prettyWobbleStoreError(err))
				}
				if jsonOutput {
					outputJSON(result)
				} else {
					fmt.Println("Mode: simulated (no session data)")
					renderWobbleScanSingle(result)
				}
			} else {
				results, err := wobble.ScanAllSkills(skillsDir, 0)
				if err != nil {
					FatalErrorRespectJSON("scan failed: %v", err)
				}
				if err := persistWobbleScan(wobbleSkillsFromSummary(results, skillsDir), generatedAt, actor); err != nil && !jsonOutput {
					fmt.Fprintf(os.Stderr, "%v\n", prettyWobbleStoreError(err))
				}
				if jsonOutput {
					outputJSON(results)
				} else {
					fmt.Println("Mode: simulated (no session data)")
					renderWobbleScanAll(results, topN, skillsDir)
				}
			}
			return
		}

		if err := persistWobbleScan(wobbleSkillsFromRealResults(results, skillsDir), generatedAt, actor); err != nil && !jsonOutput {
			fmt.Fprintf(os.Stderr, "%v\n", prettyWobbleStoreError(err))
		}
		if jsonOutput {
			outputJSON(results)
			return
		}

		fmt.Println("Mode: real session data")
		renderRealSessionResults(results)
		return
	}

	if scanAll {
		results, err := wobble.ScanAllSkills(skillsDir, runs)
		if err != nil {
			FatalErrorRespectJSON("scan failed: %v", err)
		}
		if err := persistWobbleScan(wobbleSkillsFromSummary(results, skillsDir), generatedAt, actor); err != nil && !jsonOutput {
			fmt.Fprintf(os.Stderr, "%v\n", prettyWobbleStoreError(err))
		}

		if jsonOutput {
			outputJSON(results)
			return
		}

		fmt.Println("Mode: simulated")
		renderWobbleScanAll(results, topN, skillsDir)
		return
	}

	// Single skill scan
	if len(args) == 0 {
		FatalErrorRespectJSON("skill name required (or use --all to scan all skills)")
	}

	skillName := args[0]
	result, err := wobble.ScanSkill(skillsDir, skillName, runs)
	if err != nil {
		FatalErrorRespectJSON("scan failed: %v", err)
	}
	if err := persistWobbleScan(wobbleSkillsFromScanResult(result, skillsDir), generatedAt, actor); err != nil && !jsonOutput {
		fmt.Fprintf(os.Stderr, "%v\n", prettyWobbleStoreError(err))
	}

	if jsonOutput {
		outputJSON(result)
		return
	}

	fmt.Println("Mode: simulated")
	renderWobbleScanSingle(result)
}

func applyWobbleSessionConfig() {
	sessionsDir := config.GetString("wobble.sessions_dir")
	projectsDir := config.GetString("wobble.projects_dir")
	if env := os.Getenv("WOBBLE_SESSIONS_DIR"); env != "" {
		sessionsDir = env
	}
	if env := os.Getenv("WOBBLE_PROJECTS_DIR"); env != "" {
		projectsDir = env
	}
	wobble.ConfigureSessionDirs(sessionsDir, projectsDir)
}

func runWobbleInspect(cmd *cobra.Command, args []string) {
	projectPath := "."
	if len(args) > 0 {
		projectPath = args[0]
	}

	showFix, _ := cmd.Flags().GetBool("fix")

	result, err := wobble.InspectProject(projectPath)
	if err != nil {
		FatalErrorRespectJSON("inspect failed: %v", err)
	}

	if jsonOutput {
		outputJSON(result)
		return
	}

	renderWobbleInspect(result, showFix)
}

func renderWobbleScanSingle(result *wobble.ScanResult) {
	// Header
	fmt.Println()
	fmt.Printf("┌─ WOBBLE REPORT: %s ─────────────────────────────┐\n", result.Skill)
	fmt.Println("│                                                    │")

	// Expected command (truncated)
	expected := result.Expected
	if expected == "" {
		expected = "N/A"
	}
	if len(expected) > 45 {
		expected = expected[:42] + "..."
	}
	fmt.Printf("│ Expected: %-41s │\n", expected)
	fmt.Println("│                                                    │")

	// Behavioral metrics (if available)
	if result.Behavioral != nil {
		fmt.Printf("│ Behavioral Metrics (N=%d runs):                    │\n", result.Behavioral.Runs)
		fmt.Printf("│ ├─ Exact Match Rate: %-27s │\n", fmt.Sprintf("%.0f%%", result.Behavioral.ExactMatchRate*100))
		fmt.Printf("│ ├─ Variants Found:   %-27d │\n", result.Behavioral.VariantCount)
		fmt.Printf("│ ├─ Bias:             %-27s │\n", fmt.Sprintf("%.2f", result.Behavioral.Bias))
		fmt.Printf("│ ├─ Variance:         %-27s │\n", fmt.Sprintf("%.2f", result.Behavioral.Variance))
		fmt.Printf("│ └─ Wobble Score:     %-27s │\n", fmt.Sprintf("%.2f", result.Behavioral.WobbleScore))
		fmt.Println("│                                                    │")
	}

	// Structural risk
	fmt.Printf("│ Structural Risk: %-31s │\n", fmt.Sprintf("%.0f%%", result.Structure.RiskScore*100))

	// Risk factors
	if len(result.Structure.ActiveFactors) > 0 {
		fmt.Println("│ Risk Factors:                                      │")
		for i, factor := range result.Structure.ActiveFactors {
			if i >= 3 {
				break
			}
			fmt.Printf("│   • %-44s │\n", factor)
		}
	}

	fmt.Println("│                                                    │")
	fmt.Println("├────────────────────────────────────────────────────┤")

	// Verdict
	var verdictIcon string
	switch result.Verdict {
	case "STABLE":
		verdictIcon = ui.RenderPass("✅")
	case "WOBBLY":
		verdictIcon = ui.RenderWarn("⚠️ ")
	case "UNSTABLE":
		verdictIcon = ui.RenderFail("🔴")
	}
	fmt.Printf("│ VERDICT: %s %s                              │\n", verdictIcon, result.Verdict)
	fmt.Println("│                                                    │")

	// Recommendation (word-wrap if needed)
	rec := result.Recommendation
	if len(rec) > 46 {
		rec = rec[:43] + "..."
	}
	fmt.Printf("│ %-50s │\n", rec)
	fmt.Println("└────────────────────────────────────────────────────┘")
	fmt.Println()
}

func renderWobbleScanAll(results []wobble.SkillSummary, topN int, skillsDir string) {
	if len(results) == 0 {
		fmt.Println("No skills found to scan.")
		return
	}

	if topN > len(results) {
		topN = len(results)
	}

	fmt.Println()
	fmt.Printf("┌─ WOBBLE SCAN: Top %d Riskiest Skills ─────────────────┐\n", topN)
	fmt.Println("│                                                        │")
	fmt.Println("│ Skill                    Wobble   Struct   Combined    │")
	fmt.Println("│ ────────────────────────────────────────────────────── │")

	for i, r := range results {
		if i >= topN {
			break
		}
		name := r.Name
		if len(name) > 24 {
			name = name[:21] + "..."
		}
		wobbleStr := "N/A "
		if r.WobbleScore != nil {
			wobbleStr = fmt.Sprintf("%.2f", *r.WobbleScore)
		}
		fmt.Printf("│ %-24s %-8s %-8s %-11s │\n",
			name, wobbleStr, fmt.Sprintf("%.2f", r.StructuralRisk), fmt.Sprintf("%.2f", r.CombinedRisk))
	}

	fmt.Println("│                                                        │")
	fmt.Printf("│ Total skills scanned: %-33d │\n", len(results))
	fmt.Printf("│ Skills directory: %-37s │\n", truncatePath(skillsDir, 37))
	fmt.Println("└────────────────────────────────────────────────────────┘")
	fmt.Println()
}

func renderWobbleInspect(result *wobble.ProjectInspection, showFix bool) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  🔍 WOBBLE PROJECT INSPECTION                                ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	fmt.Printf("📁 Project: %s\n", result.ProjectName)
	fmt.Printf("   Path: %s\n", result.ProjectPath)
	fmt.Println()

	if !result.HasClaude {
		fmt.Println("❌ No .claude/ folder found")
		fmt.Println("   This project has no Claude configuration.")
		fmt.Println()
		return
	}

	fmt.Println("✅ .claude/ folder found")
	fmt.Println()

	// Inventory
	fmt.Println("┌─ INVENTORY ─────────────────────────────────────────────┐")
	fmt.Printf("│ Skills: %3d                                            │\n", len(result.Inventory.Skills))
	fmt.Printf("│ Rules:  %3d                                            │\n", len(result.Inventory.Rules))
	fmt.Printf("│ Agents: %3d                                            │\n", len(result.Inventory.Agents))
	fmt.Printf("│ Hooks:  %3d                                            │\n", len(result.Inventory.Hooks))
	fmt.Println("└─────────────────────────────────────────────────────────┘")
	fmt.Println()

	// Skill analysis
	if len(result.SkillRisks) > 0 {
		fmt.Println("┌─ SKILL WOBBLE ANALYSIS ─────────────────────────────────┐")
		fmt.Println("│                                                         │")
		fmt.Println("│ Skill                    Struct   Verdict               │")
		fmt.Println("│ ─────────────────────────────────────────────────────── │")

		// Show up to 20 skills
		showCount := len(result.SkillRisks)
		if showCount > 20 {
			showCount = 20
		}

		for i := 0; i < showCount; i++ {
			entry := result.SkillRisks[i]
			name := entry.Name
			if len(name) > 24 {
				name = name[:21] + "..."
			}

			var verdictStr string
			switch entry.Verdict {
			case "STABLE":
				verdictStr = ui.RenderPass("✅ STABLE")
			case "WOBBLY":
				verdictStr = ui.RenderWarn("⚠️  WOBBLY")
			case "UNSTABLE":
				verdictStr = ui.RenderFail("🔴 UNSTABLE")
			}

			fmt.Printf("│ %-24s %5.0f%%    %-18s │\n",
				name, entry.StructuralRisk*100, verdictStr)
		}

		fmt.Println("│                                                         │")
		fmt.Println("└─────────────────────────────────────────────────────────┘")
		fmt.Println()

		// Summary
		fmt.Println("┌─ SUMMARY ───────────────────────────────────────────────┐")
		fmt.Printf("│ ✅ Stable:   %3d skills                                  │\n", result.Summary.StableCount)
		fmt.Printf("│ ⚠️  Wobbly:   %3d skills (need attention)                │\n", result.Summary.WobblyCount)
		fmt.Printf("│ 🔴 Unstable: %3d skills (need rewrite)                  │\n", result.Summary.UnstableCount)
		fmt.Println("└─────────────────────────────────────────────────────────┘")
		fmt.Println()

		// Show fix instructions if requested
		if showFix && (result.Summary.WobblyCount > 0 || result.Summary.UnstableCount > 0) {
			renderFixInstructions(result)
		}
	}
}

func renderFixInstructions(result *wobble.ProjectInspection) {
	fmt.Println("┌─ HOW TO FIX ────────────────────────────────────────────┐")
	fmt.Println("│                                                         │")
	fmt.Println("│ For each wobbly/unstable skill, add:                    │")
	fmt.Println("│                                                         │")
	fmt.Println("│ 1. ## ⚠️ EXECUTE NOW                                    │")
	fmt.Println("│    **When this skill loads, run this immediately:**     │")
	fmt.Println("│    ```bash                                              │")
	fmt.Println("│    your-exact-command --with-flags                      │")
	fmt.Println("│    ```                                                  │")
	fmt.Println("│                                                         │")
	fmt.Println("│ 2. **Do NOT improvise.** Run the command above first.   │")
	fmt.Println("│                                                         │")
	fmt.Println("│ 3. Mark one action as `(default)` if multiple exist     │")
	fmt.Println("│                                                         │")
	fmt.Println("└─────────────────────────────────────────────────────────┘")
	fmt.Println()

	// List high priority fixes
	var highRisk []string
	for _, entry := range result.SkillRisks {
		if entry.Verdict == "UNSTABLE" {
			highRisk = append(highRisk, entry.Name)
			if len(highRisk) >= 5 {
				break
			}
		}
	}

	if len(highRisk) > 0 {
		fmt.Println("🔴 Priority fixes (unstable):")
		for _, name := range highRisk {
			fmt.Printf("   • %s\n", name)
		}
		fmt.Println()
	}
}

func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	// Show ... at the start
	return "..." + path[len(path)-(maxLen-3):]
}

// renderRealSessionResults displays results from real session data analysis.
func renderRealSessionResults(results []wobble.RealScanResult) {
	fmt.Println()
	fmt.Println("┌─ WOBBLE SCAN: REAL SESSION DATA ───────────────────────┐")
	fmt.Println("│                                                        │")
	fmt.Printf("│ 📊 Analyzed %d skills with REAL session data            │\n", len(results))
	fmt.Println("│                                                        │")
	fmt.Println("└────────────────────────────────────────────────────────┘")
	fmt.Println()

	for _, r := range results {
		fmt.Printf("┌─ WOBBLE REPORT: %s (REAL DATA) ─────────────────┐\n", r.Skill)
		fmt.Println("│                                                    │")

		// Expected command
		expected := r.Expected
		if len(expected) > 42 {
			expected = expected[:39] + "..."
		}
		fmt.Printf("│ Expected: %-41s │\n", expected)
		fmt.Printf("│ Invocations: %-38d │\n", r.Invocations)
		fmt.Println("│                                                    │")

		// Behavioral metrics from REAL data
		if r.Behavioral != nil {
			fmt.Printf("│ Behavioral Metrics (N=%d REAL runs):              │\n", r.Behavioral.Runs)
			fmt.Printf("│ ├─ Exact Match Rate: %-27s │\n", fmt.Sprintf("%.0f%%", r.Behavioral.ExactMatchRate*100))
			fmt.Printf("│ ├─ Variants Found:   %-27d │\n", r.Behavioral.VariantCount)
			fmt.Printf("│ ├─ Bias:             %-27s │\n", fmt.Sprintf("%.2f", r.Behavioral.Bias))
			fmt.Printf("│ ├─ Variance:         %-27s │\n", fmt.Sprintf("%.2f", r.Behavioral.Variance))
			fmt.Printf("│ └─ Wobble Score:     %-27s │\n", fmt.Sprintf("%.2f", r.Behavioral.WobbleScore))
			fmt.Println("│                                                    │")

			// Show actual variants observed
			if len(r.Behavioral.Variants) > 1 {
				fmt.Println("│ Variants observed:                                 │")
				for i, v := range r.Behavioral.Variants {
					if i >= 3 {
						fmt.Printf("│   ... and %d more                                 │\n", len(r.Behavioral.Variants)-3)
						break
					}
					vTrunc := v
					if len(vTrunc) > 44 {
						vTrunc = vTrunc[:41] + "..."
					}
					fmt.Printf("│   • %-44s │\n", vTrunc)
				}
				fmt.Println("│                                                    │")
			}
		}

		// Structural risk
		fmt.Printf("│ Structural Risk: %-31s │\n", fmt.Sprintf("%.0f%%", r.Structure.RiskScore*100))

		// Risk factors
		if len(r.Structure.ActiveFactors) > 0 {
			fmt.Println("│ Risk Factors:                                      │")
			for i, factor := range r.Structure.ActiveFactors {
				if i >= 3 {
					break
				}
				fmt.Printf("│   • %-44s │\n", factor)
			}
		}

		fmt.Println("│                                                    │")
		fmt.Println("├────────────────────────────────────────────────────┤")

		// Verdict
		var verdictIcon string
		switch r.Verdict {
		case "STABLE":
			verdictIcon = ui.RenderPass("✅")
		case "WOBBLY":
			verdictIcon = ui.RenderWarn("⚠️ ")
		case "UNSTABLE":
			verdictIcon = ui.RenderFail("🔴")
		}
		fmt.Printf("│ VERDICT: %s %s                              │\n", verdictIcon, r.Verdict)
		fmt.Println("│                                                    │")

		rec := r.Recommendation
		if len(rec) > 46 {
			rec = rec[:43] + "..."
		}
		fmt.Printf("│ %-50s │\n", rec)
		fmt.Println("└────────────────────────────────────────────────────┘")
		fmt.Println()
	}
}
