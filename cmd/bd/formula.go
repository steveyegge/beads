package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/ui"
)

// formulaCmd is the parent command for formula operations.
var formulaCmd = &cobra.Command{
	Use:   "formula",
	Short: "Manage workflow formulas",
	Long: `Manage workflow formulas - the source layer for molecule templates.

Formulas are YAML/JSON files that define workflows with composition rules.
They are "cooked" into proto beads which can then be poured or wisped.

The Rig → Cook → Run lifecycle:
  - Rig: Compose formulas (extends, compose)
  - Cook: Transform to proto (bd cook expands macros, applies aspects)
  - Run: Agents execute poured mols or wisps

Search paths (in order):
  1. .beads/formulas/ (project)
  2. ~/.beads/formulas/ (user)
  3. $GT_ROOT/.beads/formulas/ (orchestrator, if GT_ROOT set)

Commands:
  list   List available formulas from all search paths
  show   Show formula details, steps, and composition rules`,
}

// formulaListCmd lists all available formulas.
var formulaListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available formulas",
	Long: `List all formulas from search paths.

Search paths (in order of priority):
  1. .beads/formulas/ (project - highest priority)
  2. ~/.beads/formulas/ (user)
  3. $GT_ROOT/.beads/formulas/ (orchestrator, if GT_ROOT set)

Formulas in earlier paths shadow those with the same name in later paths.

Use --all to discover formulas across all rigs in a Gas Town workspace.
This reads routes from $GT_ROOT/.beads/routes.jsonl and scans each rig's
.beads/formulas/ directory, grouping results by source.

Examples:
  bd formula list
  bd formula list --json
  bd formula list --type workflow
  bd formula list --type aspect
  bd formula list --all`,
	Run: runFormulaList,
}

// formulaShowCmd shows details of a specific formula.
var formulaShowCmd = &cobra.Command{
	Use:   "show <formula-name>",
	Short: "Show formula details",
	Long: `Show detailed information about a formula.

Displays:
  - Formula metadata (name, type, description)
  - Variables with defaults and constraints
  - Steps with dependencies
  - Composition rules (extends, aspects, expansions)
  - Bond points for external composition

Examples:
  bd formula show shiny
  bd formula show rule-of-five
  bd formula show security-audit --json`,
	Args: cobra.ExactArgs(1),
	Run:  runFormulaShow,
}

// FormulaListEntry represents a formula in the list output.
type FormulaListEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Steps       int    `json:"steps"`
	Vars        int    `json:"vars"`
}

func runFormulaList(cmd *cobra.Command, args []string) {
	typeFilter, _ := cmd.Flags().GetString("type")

	if listAllRigs {
		runFormulaListAll(typeFilter)
		return
	}

	// Get all search paths
	searchPaths := getFormulaSearchPaths()

	// Track seen formulas (first occurrence wins)
	seen := make(map[string]bool)
	var entries []FormulaListEntry

	// Scan each search path
	for _, dir := range searchPaths {
		formulas, err := scanFormulaDir(dir)
		if err != nil {
			continue // Skip inaccessible directories
		}

		for _, f := range formulas {
			if seen[f.Formula] {
				continue // Skip shadowed formulas
			}
			seen[f.Formula] = true

			// Apply type filter
			if typeFilter != "" && string(f.Type) != typeFilter {
				continue
			}

			entries = append(entries, FormulaListEntry{
				Name:        f.Formula,
				Type:        string(f.Type),
				Description: truncateDescription(f.Description, 60),
				Source:      f.Source,
				Steps:       countSteps(f.Steps),
				Vars:        len(f.Vars),
			})
		}
	}

	// Sort by name
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	if jsonOutput {
		outputJSON(entries)
		return
	}

	if len(entries) == 0 {
		fmt.Println("No formulas found.")
		fmt.Println("\nSearch paths:")
		for _, p := range searchPaths {
			fmt.Printf("  %s\n", p)
		}
		return
	}

	fmt.Printf("📜 Formulas (%d found)\n\n", len(entries))

	// Group by type
	byType := make(map[string][]FormulaListEntry)
	for _, e := range entries {
		byType[e.Type] = append(byType[e.Type], e)
	}

	// Print in type order: workflow, expansion, aspect
	typeOrder := []string{"workflow", "expansion", "aspect"}
	for _, t := range typeOrder {
		typeEntries := byType[t]
		if len(typeEntries) == 0 {
			continue
		}

		typeIcon := getTypeIcon(t)
		fmt.Printf("%s %s:\n", typeIcon, strings.Title(t))

		for _, e := range typeEntries {
			varInfo := ""
			if e.Vars > 0 {
				varInfo = fmt.Sprintf(" (%d vars)", e.Vars)
			}
			fmt.Printf("  %-25s %s%s\n", e.Name, e.Description, varInfo)
		}
		fmt.Println()
	}
}

// RigFormulaGroup holds formulas for a single rig.
type RigFormulaGroup struct {
	RigName  string             // e.g., "town", "local", "fhc"
	Path     string             // Path to the formulas directory
	Formulas []FormulaListEntry // Formulas in this rig
}

// runFormulaListAll lists formulas across all rigs using routes.jsonl.
func runFormulaListAll(typeFilter string) {
	gtRoot := os.Getenv("GT_ROOT")
	if gtRoot == "" {
		fmt.Fprintf(os.Stderr, "Error: --all requires GT_ROOT to be set\n")
		fmt.Fprintf(os.Stderr, "This flag discovers formulas across all rigs in a Gas Town workspace.\n")
		os.Exit(1)
	}

	// Load routes from town-level beads
	townBeadsDir := filepath.Join(gtRoot, ".beads")
	routes, err := routing.LoadRoutes(townBeadsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading routes: %v\n", err)
		os.Exit(1)
	}

	if len(routes) == 0 {
		fmt.Fprintf(os.Stderr, "No routes found in %s/routes.jsonl\n", townBeadsDir)
		os.Exit(1)
	}

	var groups []RigFormulaGroup

	// First, add town-level formulas (from $GT_ROOT/.beads/formulas)
	townFormulasDir := filepath.Join(townBeadsDir, "formulas")
	if formulas, err := scanFormulaDir(townFormulasDir); err == nil && len(formulas) > 0 {
		var entries []FormulaListEntry
		for _, f := range formulas {
			if typeFilter != "" && string(f.Type) != typeFilter {
				continue
			}
			entries = append(entries, FormulaListEntry{
				Name:        f.Formula,
				Type:        string(f.Type),
				Description: truncateDescription(f.Description, 60),
				Source:      f.Source,
				Steps:       countSteps(f.Steps),
				Vars:        len(f.Vars),
			})
		}
		if len(entries) > 0 {
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name < entries[j].Name
			})
			groups = append(groups, RigFormulaGroup{
				RigName:  "town",
				Path:     townFormulasDir,
				Formulas: entries,
			})
		}
	}

	// Then scan each rig from routes
	for _, route := range routes {
		if route.Path == "." {
			continue // Skip town-level (already handled above)
		}

		rigName := routing.ExtractProjectFromPath(route.Path)
		if rigName == "" {
			rigName = route.Prefix
		}

		// Construct path to rig's formulas directory
		rigFormulasDir := filepath.Join(gtRoot, route.Path, ".beads", "formulas")

		formulas, err := scanFormulaDir(rigFormulasDir)
		if err != nil || len(formulas) == 0 {
			continue // Skip rigs without formulas
		}

		var entries []FormulaListEntry
		for _, f := range formulas {
			if typeFilter != "" && string(f.Type) != typeFilter {
				continue
			}
			entries = append(entries, FormulaListEntry{
				Name:        f.Formula,
				Type:        string(f.Type),
				Description: truncateDescription(f.Description, 60),
				Source:      f.Source,
				Steps:       countSteps(f.Steps),
				Vars:        len(f.Vars),
			})
		}

		if len(entries) > 0 {
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name < entries[j].Name
			})
			groups = append(groups, RigFormulaGroup{
				RigName:  rigName,
				Path:     rigFormulasDir,
				Formulas: entries,
			})
		}
	}

	// Also check user-level formulas
	if home, err := os.UserHomeDir(); err == nil {
		userFormulasDir := filepath.Join(home, ".beads", "formulas")
		if formulas, err := scanFormulaDir(userFormulasDir); err == nil && len(formulas) > 0 {
			var entries []FormulaListEntry
			for _, f := range formulas {
				if typeFilter != "" && string(f.Type) != typeFilter {
					continue
				}
				entries = append(entries, FormulaListEntry{
					Name:        f.Formula,
					Type:        string(f.Type),
					Description: truncateDescription(f.Description, 60),
					Source:      f.Source,
					Steps:       countSteps(f.Steps),
					Vars:        len(f.Vars),
				})
			}
			if len(entries) > 0 {
				sort.Slice(entries, func(i, j int) bool {
					return entries[i].Name < entries[j].Name
				})
				groups = append(groups, RigFormulaGroup{
					RigName:  "user",
					Path:     userFormulasDir,
					Formulas: entries,
				})
			}
		}
	}

	// Calculate total
	total := 0
	for _, g := range groups {
		total += len(g.Formulas)
	}

	if jsonOutput {
		outputJSON(groups)
		return
	}

	if total == 0 {
		fmt.Println("No formulas found across any rigs.")
		return
	}

	fmt.Printf("📜 Formulas (%d found)\n\n", total)

	for _, g := range groups {
		fmt.Printf("[%s] (%d)\n", g.RigName, len(g.Formulas))

		// Collect formula names for compact display
		names := make([]string, len(g.Formulas))
		for i, f := range g.Formulas {
			names[i] = f.Name
		}

		// Print in compact format with wrapping
		printWrappedNames(names, "  ", 80)
		fmt.Println()
	}
}

// printWrappedNames prints formula names with word wrapping.
func printWrappedNames(names []string, indent string, maxWidth int) {
	if len(names) == 0 {
		return
	}

	line := indent
	for i, name := range names {
		suffix := ", "
		if i == len(names)-1 {
			suffix = ""
		}

		addition := name + suffix
		if len(line)+len(addition) > maxWidth && line != indent {
			fmt.Println(line)
			line = indent + addition
		} else {
			line += addition
		}
	}
	if line != indent {
		fmt.Println(line)
	}
}

func runFormulaShow(cmd *cobra.Command, args []string) {
	name := args[0]

	// Create parser with default search paths
	parser := formula.NewParser()

	// Try to load the formula
	f, err := parser.LoadByName(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nSearch paths:\n")
		for _, p := range getFormulaSearchPaths() {
			fmt.Fprintf(os.Stderr, "  %s\n", p)
		}
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(f)
		return
	}

	// Print header
	typeIcon := getTypeIcon(string(f.Type))
	fmt.Printf("\n%s %s\n", typeIcon, f.Formula)
	fmt.Printf("   Type: %s\n", f.Type)
	if f.Description != "" {
		fmt.Printf("   Description: %s\n", f.Description)
	}
	fmt.Printf("   Source: %s\n", f.Source)

	// Print extends
	if len(f.Extends) > 0 {
		fmt.Printf("\n%s Extends:\n", ui.RenderAccent("📎"))
		for _, ext := range f.Extends {
			fmt.Printf("   - %s\n", ext)
		}
	}

	// Print variables
	if len(f.Vars) > 0 {
		fmt.Printf("\n%s Variables:\n", ui.RenderWarn("📝"))
		// Sort for consistent output
		varNames := make([]string, 0, len(f.Vars))
		for name := range f.Vars {
			varNames = append(varNames, name)
		}
		sort.Strings(varNames)

		for _, name := range varNames {
			v := f.Vars[name]
			attrs := []string{}
			if v.Required {
				attrs = append(attrs, ui.RenderFail("required"))
			}
			if v.Default != "" {
				attrs = append(attrs, fmt.Sprintf("default=%q", v.Default))
			}
			if len(v.Enum) > 0 {
				attrs = append(attrs, fmt.Sprintf("enum=[%s]", strings.Join(v.Enum, ",")))
			}
			if v.Pattern != "" {
				attrs = append(attrs, fmt.Sprintf("pattern=%q", v.Pattern))
			}
			attrStr := ""
			if len(attrs) > 0 {
				attrStr = fmt.Sprintf(" [%s]", strings.Join(attrs, ", "))
			}
			desc := ""
			if v.Description != "" {
				desc = fmt.Sprintf(": %s", v.Description)
			}
			fmt.Printf("   {{%s}}%s%s\n", name, desc, attrStr)
		}
	}

	// Print steps
	if len(f.Steps) > 0 {
		fmt.Printf("\n%s Steps (%d):\n", ui.RenderPass("🌲"), countSteps(f.Steps))
		printFormulaStepsTree(f.Steps, "   ")
	}

	// Print template (for expansion formulas)
	if len(f.Template) > 0 {
		fmt.Printf("\n%s Template (%d steps):\n", ui.RenderAccent("📐"), len(f.Template))
		printFormulaStepsTree(f.Template, "   ")
	}

	// Print advice rules
	if len(f.Advice) > 0 {
		fmt.Printf("\n%s Advice:\n", ui.RenderWarn("💡"))
		for _, a := range f.Advice {
			parts := []string{}
			if a.Before != nil {
				parts = append(parts, fmt.Sprintf("before: %s", a.Before.ID))
			}
			if a.After != nil {
				parts = append(parts, fmt.Sprintf("after: %s", a.After.ID))
			}
			if a.Around != nil {
				parts = append(parts, "around")
			}
			fmt.Printf("   %s → %s\n", a.Target, strings.Join(parts, ", "))
		}
	}

	// Print compose rules
	if f.Compose != nil {
		hasCompose := len(f.Compose.BondPoints) > 0 || len(f.Compose.Expand) > 0 ||
			len(f.Compose.Map) > 0 || len(f.Compose.Aspects) > 0

		if hasCompose {
			fmt.Printf("\n%s Composition:\n", ui.RenderAccent("🔗"))

			if len(f.Compose.BondPoints) > 0 {
				fmt.Printf("   Bond Points:\n")
				for _, bp := range f.Compose.BondPoints {
					loc := ""
					if bp.AfterStep != "" {
						loc = fmt.Sprintf("after %s", bp.AfterStep)
					} else if bp.BeforeStep != "" {
						loc = fmt.Sprintf("before %s", bp.BeforeStep)
					}
					fmt.Printf("     - %s (%s)\n", bp.ID, loc)
				}
			}

			if len(f.Compose.Expand) > 0 {
				fmt.Printf("   Expansions:\n")
				for _, e := range f.Compose.Expand {
					fmt.Printf("     - %s → %s\n", e.Target, e.With)
				}
			}

			if len(f.Compose.Map) > 0 {
				fmt.Printf("   Maps:\n")
				for _, m := range f.Compose.Map {
					fmt.Printf("     - %s → %s\n", m.Select, m.With)
				}
			}

			if len(f.Compose.Aspects) > 0 {
				fmt.Printf("   Aspects: %s\n", strings.Join(f.Compose.Aspects, ", "))
			}
		}
	}

	// Print pointcuts (for aspects)
	if len(f.Pointcuts) > 0 {
		fmt.Printf("\n%s Pointcuts:\n", ui.RenderWarn("🎯"))
		for _, p := range f.Pointcuts {
			parts := []string{}
			if p.Glob != "" {
				parts = append(parts, fmt.Sprintf("glob=%q", p.Glob))
			}
			if p.Type != "" {
				parts = append(parts, fmt.Sprintf("type=%q", p.Type))
			}
			if p.Label != "" {
				parts = append(parts, fmt.Sprintf("label=%q", p.Label))
			}
			fmt.Printf("   - %s\n", strings.Join(parts, ", "))
		}
	}

	fmt.Println()
}

// getFormulaSearchPaths returns the formula search paths in priority order.
// Implements three-tier resolution:
//  1. Project (rig-level): .beads/formulas/ in current directory
//  2. Town (user-level): $GT_ROOT/.beads/formulas/ or auto-detected town root
//  3. User fallback: ~/.beads/formulas/ (only if not in a Gas Town workspace)
func getFormulaSearchPaths() []string {
	var paths []string

	// Tier 1: Project-level formulas
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, ".beads", "formulas"))
	}

	// Tier 2: Town-level formulas (GT_ROOT or auto-detected)
	townRoot := os.Getenv("GT_ROOT")
	if townRoot == "" {
		// Auto-detect town root by walking up from CWD
		cwd, err := os.Getwd()
		if err == nil {
			townRoot = routing.FindTownRoot(cwd)
		}
	}

	if townRoot != "" {
		paths = append(paths, filepath.Join(townRoot, ".beads", "formulas"))
	} else {
		// Tier 3: User-level formulas (only if not in a Gas Town workspace)
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, ".beads", "formulas"))
		}
	}

	return paths
}

// scanFormulaDir scans a directory for formula files (both TOML and JSON).
func scanFormulaDir(dir string) ([]*formula.Formula, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	parser := formula.NewParser(dir)
	var formulas []*formula.Formula

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// Support both .formula.toml and .formula.json
		name := entry.Name()
		if !strings.HasSuffix(name, formula.FormulaExtTOML) && !strings.HasSuffix(name, formula.FormulaExtJSON) {
			continue
		}

		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(path)
		if err != nil {
			continue // Skip invalid formulas
		}
		formulas = append(formulas, f)
	}

	return formulas, nil
}

// countSteps recursively counts steps including children.
func countSteps(steps []*formula.Step) int {
	count := len(steps)
	for _, s := range steps {
		count += countSteps(s.Children)
	}
	return count
}

// truncateDescription truncates a description to maxLen characters.
//
//nolint:unparam // maxLen is always 60 for now but may vary in future
func truncateDescription(desc string, maxLen int) string {
	// Take first line only
	if idx := strings.Index(desc, "\n"); idx >= 0 {
		desc = desc[:idx]
	}
	if len(desc) > maxLen {
		return desc[:maxLen-3] + "..."
	}
	return desc
}

// getTypeIcon returns an icon for the formula type.
func getTypeIcon(t string) string {
	switch t {
	case "workflow":
		return "📋"
	case "expansion":
		return "📐"
	case "aspect":
		return "🎯"
	default:
		return "📜"
	}
}

// printFormulaStepsTree prints steps in a tree format.
func printFormulaStepsTree(steps []*formula.Step, indent string) {
	for i, step := range steps {
		connector := "├──"
		if i == len(steps)-1 {
			connector = "└──"
		}

		// Collect dependency info
		var depParts []string
		if len(step.DependsOn) > 0 {
			depParts = append(depParts, fmt.Sprintf("depends: %s", strings.Join(step.DependsOn, ", ")))
		}
		if len(step.Needs) > 0 {
			depParts = append(depParts, fmt.Sprintf("needs: %s", strings.Join(step.Needs, ", ")))
		}
		if step.WaitsFor != "" {
			depParts = append(depParts, fmt.Sprintf("waits_for: %s", step.WaitsFor))
		}

		depStr := ""
		if len(depParts) > 0 {
			depStr = fmt.Sprintf(" [%s]", strings.Join(depParts, ", "))
		}

		typeStr := ""
		if step.Type != "" && step.Type != "task" {
			typeStr = fmt.Sprintf(" (%s)", step.Type)
		}

		fmt.Printf("%s%s %s: %s%s%s\n", indent, connector, step.ID, step.Title, typeStr, depStr)

		if len(step.Children) > 0 {
			childIndent := indent
			if i == len(steps)-1 {
				childIndent += "    "
			} else {
				childIndent += "│   "
			}
			printFormulaStepsTree(step.Children, childIndent)
		}
	}
}

// formulaConvertCmd converts JSON formulas to TOML.
var formulaConvertCmd = &cobra.Command{
	Use:   "convert <formula-name|path> [--all]",
	Short: "Convert formula from JSON to TOML",
	Long: `Convert formula files from JSON to TOML format.

TOML format provides better ergonomics:
  - Multi-line strings without \n escaping
  - Human-readable diffs
  - Comments allowed

The convert command reads a .formula.json file and outputs .formula.toml.
The original JSON file is preserved (use --delete to remove it).

Examples:
  bd formula convert shiny              # Convert shiny.formula.json to .toml
  bd formula convert ./my.formula.json  # Convert specific file
  bd formula convert --all              # Convert all JSON formulas
  bd formula convert shiny --delete     # Convert and remove JSON file
  bd formula convert shiny --stdout     # Print TOML to stdout`,
	Run: runFormulaConvert,
}

var (
	convertAll    bool
	convertDelete bool
	convertStdout bool
	listAllRigs   bool
)

func runFormulaConvert(cmd *cobra.Command, args []string) {
	if convertAll {
		convertAllFormulas()
		return
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: formula name or path required\n")
		fmt.Fprintf(os.Stderr, "Usage: bd formula convert <name|path> [--all]\n")
		os.Exit(1)
	}

	name := args[0]

	// Determine the JSON file path
	var jsonPath string
	if strings.HasSuffix(name, formula.FormulaExtJSON) {
		// Direct path provided
		jsonPath = name
	} else if strings.HasSuffix(name, formula.FormulaExtTOML) {
		fmt.Fprintf(os.Stderr, "Error: %s is already a TOML file\n", name)
		os.Exit(1)
	} else {
		// Search for the formula in search paths
		jsonPath = findFormulaJSON(name)
		if jsonPath == "" {
			fmt.Fprintf(os.Stderr, "Error: JSON formula %q not found\n", name)
			fmt.Fprintf(os.Stderr, "\nSearch paths:\n")
			for _, p := range getFormulaSearchPaths() {
				fmt.Fprintf(os.Stderr, "  %s\n", p)
			}
			os.Exit(1)
		}
	}

	// Parse the JSON file
	parser := formula.NewParser()
	f, err := parser.ParseFile(jsonPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", jsonPath, err)
		os.Exit(1)
	}

	// Convert to TOML
	tomlData, err := formulaToTOML(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error converting to TOML: %v\n", err)
		os.Exit(1)
	}

	if convertStdout {
		fmt.Print(string(tomlData))
		return
	}

	// Determine output path
	tomlPath := strings.TrimSuffix(jsonPath, formula.FormulaExtJSON) + formula.FormulaExtTOML

	// Write the TOML file
	if err := os.WriteFile(tomlPath, tomlData, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", tomlPath, err)
		os.Exit(1)
	}

	fmt.Printf("✓ Converted: %s\n", tomlPath)

	if convertDelete {
		if err := os.Remove(jsonPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not delete %s: %v\n", jsonPath, err)
		} else {
			fmt.Printf("✓ Deleted: %s\n", jsonPath)
		}
	}
}

func convertAllFormulas() {
	converted := 0
	errors := 0

	for _, dir := range getFormulaSearchPaths() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		parser := formula.NewParser(dir)

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if !strings.HasSuffix(entry.Name(), formula.FormulaExtJSON) {
				continue
			}

			jsonPath := filepath.Join(dir, entry.Name())
			tomlPath := strings.TrimSuffix(jsonPath, formula.FormulaExtJSON) + formula.FormulaExtTOML

			// Skip if TOML already exists
			if _, err := os.Stat(tomlPath); err == nil {
				fmt.Printf("⏭ Skipped (TOML exists): %s\n", entry.Name())
				continue
			}

			f, err := parser.ParseFile(jsonPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗ Error parsing %s: %v\n", jsonPath, err)
				errors++
				continue
			}

			tomlData, err := formulaToTOML(f)
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗ Error converting %s: %v\n", jsonPath, err)
				errors++
				continue
			}

			if err := os.WriteFile(tomlPath, tomlData, 0600); err != nil {
				fmt.Fprintf(os.Stderr, "✗ Error writing %s: %v\n", tomlPath, err)
				errors++
				continue
			}

			fmt.Printf("✓ Converted: %s\n", tomlPath)
			converted++

			if convertDelete {
				if err := os.Remove(jsonPath); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not delete %s: %v\n", jsonPath, err)
				}
			}
		}
	}

	fmt.Printf("\nConverted %d formulas", converted)
	if errors > 0 {
		fmt.Printf(" (%d errors)", errors)
	}
	fmt.Println()
}

// findFormulaJSON searches for a JSON formula file by name.
func findFormulaJSON(name string) string {
	for _, dir := range getFormulaSearchPaths() {
		path := filepath.Join(dir, name+formula.FormulaExtJSON)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// formulaToTOML converts a Formula to TOML bytes.
// Uses a custom structure optimized for TOML readability.
func formulaToTOML(f *formula.Formula) ([]byte, error) {
	// We need to re-read the original JSON to get the raw structure
	// because the Formula struct loses some ordering/formatting
	if f.Source == "" {
		return nil, fmt.Errorf("formula has no source path")
	}

	// Read the original JSON
	jsonData, err := os.ReadFile(f.Source)
	if err != nil {
		return nil, fmt.Errorf("reading source: %w", err)
	}

	// Parse into a map to preserve structure
	var raw map[string]interface{}
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	// Fix float64 to int for known integer fields
	fixIntegerFields(raw)

	// Encode to TOML
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	encoder.Indent = ""
	if err := encoder.Encode(raw); err != nil {
		return nil, fmt.Errorf("encoding TOML: %w", err)
	}

	// Post-process to convert escaped \n in strings to multi-line strings
	result := convertToMultiLineStrings(buf.String())

	return []byte(result), nil
}

// convertToMultiLineStrings post-processes TOML to use multi-line strings
// where strings contain newlines. This improves readability for descriptions.
func convertToMultiLineStrings(input string) string {
	// Regular expression to match key = "value with \n"
	// We look for description fields specifically as those benefit most
	lines := strings.Split(input, "\n")
	var result []string

	for _, line := range lines {
		// Check if this line has a string with escaped newlines
		if strings.Contains(line, "\\n") {
			// Find the key = "..." pattern
			eqIdx := strings.Index(line, " = \"")
			if eqIdx > 0 && strings.HasSuffix(line, "\"") {
				key := strings.TrimSpace(line[:eqIdx])
				// Only convert description fields
				if key == "description" {
					// Extract the value (without quotes)
					value := line[eqIdx+4 : len(line)-1]
					// Unescape the newlines
					value = strings.ReplaceAll(value, "\\n", "\n")
					// Use multi-line string syntax
					result = append(result, fmt.Sprintf("%s = \"\"\"\n%s\"\"\"", key, value))
					continue
				}
			}
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// fixIntegerFields recursively fixes float64 values that should be integers.
// JSON unmarshals all numbers as float64, but TOML needs proper int types.
func fixIntegerFields(m map[string]interface{}) {
	// Known integer fields
	intFields := map[string]bool{
		"version":  true,
		"priority": true,
		"count":    true,
		"max":      true,
	}

	for k, v := range m {
		switch val := v.(type) {
		case float64:
			// Convert whole numbers to int64 if they're known int fields
			if intFields[k] && val == float64(int64(val)) {
				m[k] = int64(val)
			}
		case map[string]interface{}:
			fixIntegerFields(val)
		case []interface{}:
			for _, item := range val {
				if subMap, ok := item.(map[string]interface{}); ok {
					fixIntegerFields(subMap)
				}
			}
		}
	}
}

// formulaImportCmd imports formulas from filesystem into beads database.
var formulaImportCmd = &cobra.Command{
	Use:   "import [formula-name|path]",
	Short: "Import formulas from filesystem into beads database",
	Long: `Import workflow formulas from filesystem (.formula.toml/.formula.json)
into the beads database as TypeFormula issues.

This is the migration path from filesystem-based formulas to the
"Everything Is Beads" model where formulas are stored as first-class beads.

Once imported, formulas can be:
  - Listed with 'bd formula list --db'
  - Resolved by cook/pour without filesystem access
  - Synced across rigs via standard beads JSONL federation

Examples:
  bd formula import shiny              # Import a specific formula by name
  bd formula import ./my.formula.toml  # Import from explicit path
  bd formula import --all              # Import all formulas from search paths
  bd formula import --all --force      # Re-import all (overwrite existing)`,
	Run: runFormulaImport,
}

var (
	importAll   bool
	importForce bool
)

func runFormulaImport(cmd *cobra.Command, args []string) {
	CheckReadonly("formula import")

	if !importAll && len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: formula name/path required (or use --all)\n")
		os.Exit(1)
	}

	if importAll {
		importAllFormulas()
		return
	}

	// Import single formula
	name := args[0]
	f, err := loadFormulaByNameOrPath(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading formula %q: %v\n", name, err)
		os.Exit(1)
	}

	result, err := saveFormulaToDB(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error importing %q: %v\n", f.Formula, err)
		os.Exit(1)
	}

	action := "Created"
	if !result.Created {
		action = "Updated"
	}
	fmt.Printf("%s %s formula %q as %s\n", ui.RenderPass("✓"), action, result.Name, result.ID)
}

func importAllFormulas() {
	searchPaths := getFormulaSearchPaths()
	seen := make(map[string]bool)
	imported := 0
	skipped := 0
	errors := 0

	for _, dir := range searchPaths {
		formulas, err := scanFormulaDir(dir)
		if err != nil {
			continue
		}

		for _, f := range formulas {
			if seen[f.Formula] {
				continue
			}
			seen[f.Formula] = true

			result, err := saveFormulaToDB(f)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s %s: %v\n", ui.RenderFail("✗"), f.Formula, err)
				errors++
				continue
			}

			if result.Created {
				fmt.Printf("  %s Imported %s → %s\n", ui.RenderPass("✓"), f.Formula, result.ID)
				imported++
			} else {
				fmt.Printf("  %s Updated %s → %s\n", ui.RenderPass("✓"), f.Formula, result.ID)
				imported++
			}
		}
	}

	fmt.Printf("\nImported: %d, Skipped: %d, Errors: %d\n", imported, skipped, errors)
}

// loadFormulaByNameOrPath loads a formula from an explicit path or by searching.
func loadFormulaByNameOrPath(nameOrPath string) (*formula.Formula, error) {
	// If it looks like a path (has extension or separator), try direct parse
	if strings.HasSuffix(nameOrPath, formula.FormulaExtTOML) ||
		strings.HasSuffix(nameOrPath, formula.FormulaExtJSON) ||
		strings.Contains(nameOrPath, string(os.PathSeparator)) {
		parser := formula.NewParser()
		return parser.ParseFile(nameOrPath)
	}

	// Search by name in search paths
	searchPaths := getFormulaSearchPaths()
	parser := formula.NewParser(searchPaths...)
	return parser.LoadByName(nameOrPath)
}

// saveFormulaToDB saves a formula to the database via daemon or direct storage.
func saveFormulaToDB(f *formula.Formula) (*rpc.FormulaSaveResult, error) {
	formulaJSON, err := json.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("serializing formula: %w", err)
	}

	saveArgs := &rpc.FormulaSaveArgs{
		Formula: formulaJSON,
		Force:   importForce,
	}

	if daemonClient != nil {
		return daemonClient.FormulaSave(saveArgs)
	}

	// Direct mode: use storage directly
	if store == nil {
		return nil, fmt.Errorf("no database connection (set BD_DAEMON_HOST or run in a beads directory)")
	}

	ctx := rootCtx

	// Get prefix
	idPrefix := ""
	if p, err := store.GetConfig(ctx, "issue_prefix"); err == nil && p != "" {
		idPrefix = p + "-"
	}

	issue, labels, err := formula.FormulaToIssue(f, idPrefix)
	if err != nil {
		return nil, fmt.Errorf("converting formula to issue: %w", err)
	}

	if issue.Status == "" {
		issue.Status = "open"
	}

	// Check if exists
	existing, _ := store.GetIssue(ctx, issue.ID)
	created := existing == nil

	if existing != nil {
		if !importForce {
			return nil, fmt.Errorf("formula %q already exists as %s (use --force to overwrite)", f.Formula, issue.ID)
		}
		updates := map[string]interface{}{
			"title":       issue.Title,
			"description": issue.Description,
			"metadata":    issue.Metadata,
		}
		if err := store.UpdateIssue(ctx, existing.ID, updates, actor); err != nil {
			return nil, fmt.Errorf("updating formula: %w", err)
		}
	} else {
		if err := store.CreateIssue(ctx, issue, actor); err != nil {
			return nil, fmt.Errorf("creating formula: %w", err)
		}
	}

	for _, label := range labels {
		_ = store.AddLabel(ctx, issue.ID, label, actor)
	}

	return &rpc.FormulaSaveResult{
		ID:      issue.ID,
		Name:    f.Formula,
		Created: created,
	}, nil
}

func init() {
	formulaListCmd.Flags().String("type", "", "Filter by type (workflow, expansion, aspect)")
	formulaListCmd.Flags().BoolVar(&listAllRigs, "all", false, "Discover formulas across all rigs (requires GT_ROOT)")
	formulaConvertCmd.Flags().BoolVar(&convertAll, "all", false, "Convert all JSON formulas")
	formulaConvertCmd.Flags().BoolVar(&convertDelete, "delete", false, "Delete JSON file after conversion")
	formulaConvertCmd.Flags().BoolVar(&convertStdout, "stdout", false, "Print TOML to stdout instead of file")
	formulaImportCmd.Flags().BoolVar(&importAll, "all", false, "Import all formulas from search paths")
	formulaImportCmd.Flags().BoolVar(&importForce, "force", false, "Overwrite existing formulas in database")

	formulaCmd.AddCommand(formulaListCmd)
	formulaCmd.AddCommand(formulaShowCmd)
	formulaCmd.AddCommand(formulaConvertCmd)
	formulaCmd.AddCommand(formulaImportCmd)
	rootCmd.AddCommand(formulaCmd)
}
