package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage"
)

var importCmd = &cobra.Command{
	Use:   "import [path]",
	Short: "Import issues from JSONL into Dolt",
	Long: `Import issues from JSONL (newline-delimited JSON).

Accepted line formats:
  1. Legacy JSONL records matching types.Issue
  2. Export JSONL records emitted by 'bd export' (types.IssueWithCounts)

Import semantics:
  - Default mode is upsert.
  - Strict mode fails if any imported ID already exists.
  - Upsert mode updates scalar fields and replaces labels/dependencies/comments
    for imported issue IDs (source-authoritative, idempotent).

Notes:
  - Audit events are not imported by this command (Level 1 contract only).
  - By default, wisps are included; use --include-wisps=false to skip them.`,
	GroupID: "sync",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runImport,
}

var (
	importInput                string
	importModeFlag             string
	importOrphansFlag          string
	importOrphanHandlingAlias  string
	importIncludeWisps         bool
	importSkipPrefixValidation bool
	importDryRun               bool
)

func init() {
	importCmd.Flags().StringVarP(&importInput, "input", "i", "", "Input JSONL file (default: .beads/issues.jsonl)")
	importCmd.Flags().StringVar(&importModeFlag, "mode", importModeUpsert, "Import mode: upsert|strict")
	importCmd.Flags().StringVar(&importOrphansFlag, "orphans", string(storage.OrphanAllow), "Orphan handling: strict|allow|skip|resurrect")
	importCmd.Flags().StringVar(&importOrphanHandlingAlias, "orphan-handling", "", "Deprecated alias for --orphans")
	importCmd.Flags().BoolVar(&importIncludeWisps, "include-wisps", true, "Include ephemeral wisps during import")
	importCmd.Flags().BoolVar(&importSkipPrefixValidation, "skip-prefix-validation", false, "Allow importing IDs with a different prefix")
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Parse and validate import input without writing")
	rootCmd.AddCommand(importCmd)
}

func runImport(cmd *cobra.Command, args []string) error {
	if store == nil {
		return fmt.Errorf("database not initialized (run 'bd init' first)")
	}

	inputPath := strings.TrimSpace(importInput)
	if len(args) > 0 {
		if inputPath != "" {
			return fmt.Errorf("specify input with either -i/--input or a positional path, not both")
		}
		inputPath = args[0]
	}
	if inputPath == "" {
		inputPath = filepath.Join(".beads", "issues.jsonl")
	}

	orphans := importOrphansFlag
	if cmd.Flags().Changed("orphan-handling") && strings.TrimSpace(importOrphanHandlingAlias) != "" {
		orphans = importOrphanHandlingAlias
	}

	opts := ImportOptions{
		DryRun:               importDryRun,
		Strict:               strings.EqualFold(importModeFlag, importModeStrict),
		Mode:                 importModeFlag,
		IncludeWisps:         importIncludeWisps,
		OrphanHandling:       orphans,
		SkipPrefixValidation: importSkipPrefixValidation,
	}

	result, err := importFromLocalJSONLWithOptions(rootCtx, store, inputPath, opts)
	if err != nil {
		return err
	}

	total := result.Created + result.Updated
	if !importDryRun && total > 0 {
		commandDidWrite.Store(true)
	}

	if jsonOutput {
		payload := map[string]interface{}{
			"input":                inputPath,
			"mode":                 strings.ToLower(strings.TrimSpace(importModeFlag)),
			"created":              result.Created,
			"updated":              result.Updated,
			"skipped":              result.Skipped,
			"skipped_dependencies": result.SkippedDependencies,
			"dry_run":              importDryRun,
		}
		return json.NewEncoder(os.Stdout).Encode(payload)
	}

	if importDryRun {
		fmt.Printf("Dry run: would import %d issue(s) from %s\n", total, inputPath)
	} else {
		fmt.Printf("Imported %d issue(s) from %s", total, inputPath)
		if result.Skipped > 0 {
			fmt.Printf(" (%d skipped)", result.Skipped)
		}
		fmt.Println()
	}
	if len(result.SkippedDependencies) > 0 {
		fmt.Fprintf(os.Stderr, "Skipped %d dependency edge(s) due to orphan policy\n", len(result.SkippedDependencies))
	}

	return nil
}
