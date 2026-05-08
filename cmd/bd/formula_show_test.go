//go:build cgo

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/git"
)

func captureStdoutForFormulaShow(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("pipe close: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("pipe drain: %v", err)
	}
	return buf.String()
}

// printFormulaStepsTree is the function the --full flag plumbs through to.
// Test it directly so the formatting contract is locked in without
// depending on filesystem-resident formulas.

func TestPrintFormulaStepsTreeDefaultOmitsDescriptions(t *testing.T) {
	steps := []*formula.Step{
		{ID: "first", Title: "First step title", Description: "First-step body."},
		{ID: "second", Title: "Second step title", Description: "Second-step body."},
	}
	out := captureStdoutForFormulaShow(t, func() {
		printFormulaStepsTree(steps, "   ", false)
	})
	if !strings.Contains(out, "First step title") {
		t.Fatalf("default should still emit titles, got:\n%s", out)
	}
	if strings.Contains(out, "step body") {
		t.Fatalf("default should NOT emit descriptions (use --full), got:\n%s", out)
	}
}

func TestPrintFormulaStepsTreeFullEmitsDescriptions(t *testing.T) {
	steps := []*formula.Step{
		{ID: "first", Title: "First step title", Description: "First-line body.\nSecond-line body."},
		{ID: "second", Title: "Second step title", Description: "Final-step body."},
	}
	out := captureStdoutForFormulaShow(t, func() {
		printFormulaStepsTree(steps, "   ", true)
	})
	for _, want := range []string{
		"First step title",
		"First-line body.",
		"Second-line body.",
		"Second step title",
		"Final-step body.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("--full output should contain %q, got:\n%s", want, out)
		}
	}
}

func TestPrintFormulaStepsTreeFullSkipsEmptyDescriptions(t *testing.T) {
	steps := []*formula.Step{
		{ID: "first", Title: "First step", Description: ""},
		{ID: "second", Title: "Second step", Description: "Has body."},
	}
	out := captureStdoutForFormulaShow(t, func() {
		printFormulaStepsTree(steps, "   ", true)
	})
	if !strings.Contains(out, "Has body.") {
		t.Fatalf("--full should emit non-empty descriptions, got:\n%s", out)
	}
	// First step's empty description should not produce a blank-line block;
	// guard against accidental "\n\n" gap between first-step title and
	// second-step entry.
	if strings.Contains(out, "First step\n   │   \n") {
		t.Fatalf("--full should not emit a placeholder block for empty descriptions, got:\n%s", out)
	}
}

// End-to-end test: drive `bd formula show <name> --full` against a real
// formula on disk and verify the flag plumbs through the cobra command
// to the print routine.
func TestFormulaShowCmdFullFlagEmitsDescriptions(t *testing.T) {
	t.Setenv("BEADS_DIR", "")
	t.Setenv("GT_ROOT", "")
	beads.ResetCaches()
	git.ResetCaches()
	t.Cleanup(func() {
		beads.ResetCaches()
		git.ResetCaches()
	})

	root := t.TempDir()
	formulaDir := filepath.Join(root, ".beads", "formulas")
	if err := os.MkdirAll(formulaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	formulaBody := `formula = "test-recipe"
type = "workflow"
description = "Smoke test for --full"

[[steps]]
id = "do"
title = "Do the thing"
description = "Carefully do exactly the thing the bead says."
`
	if err := os.WriteFile(filepath.Join(formulaDir, "test-recipe.formula.toml"), []byte(formulaBody), 0o644); err != nil {
		t.Fatalf("write formula: %v", err)
	}
	t.Chdir(root)
	beads.ResetCaches()
	git.ResetCaches()

	// Default: no description.
	defaultOut := captureStdoutForFormulaShow(t, func() {
		formulaShowCmd.Run(formulaShowCmd, []string{"test-recipe"})
	})
	if !strings.Contains(defaultOut, "Do the thing") {
		t.Fatalf("default should emit step title, got:\n%s", defaultOut)
	}
	if strings.Contains(defaultOut, "Carefully do exactly") {
		t.Fatalf("default should NOT emit step descriptions, got:\n%s", defaultOut)
	}

	// --full: description appears.
	if err := formulaShowCmd.Flags().Set("full", "true"); err != nil {
		t.Fatalf("set --full: %v", err)
	}
	t.Cleanup(func() { _ = formulaShowCmd.Flags().Set("full", "false") })

	fullOut := captureStdoutForFormulaShow(t, func() {
		formulaShowCmd.Run(formulaShowCmd, []string{"test-recipe"})
	})
	if !strings.Contains(fullOut, "Carefully do exactly the thing the bead says.") {
		t.Fatalf("--full should emit step descriptions, got:\n%s", fullOut)
	}
}
