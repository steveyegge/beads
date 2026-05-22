//go:build cgo

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnComplete_RuntimeFanout_EndToEnd is the end-to-end regression test for
// GH#3782 / beads-c7g — on_complete is a runtime no-op. It exercises the
// canonical dynamic-fanout pattern:
//
//  1. a "survey" step declares on_complete { for_each, bond, vars }
//  2. it's poured into the database
//  3. at runtime the agent populates step.metadata.output.<path> with a list
//     it discovered (e.g. arxiv seeds found by a literature search)
//  4. closing the step fires the executor, which iterates the list and bonds
//     one molecule of <bond> per item with placeholder-substituted vars
//
// This is the pattern that today's lit-search recipe expresses imperatively
// in ~30 lines of prose ("for each seed in resolve-seeds's output.seeds,
// run bd mol bond ..."). on_complete is meant to make it declarative.
//
// Before the fix, on_complete parses + validates + survives cook but is
// dropped at pour time and never read at close time. Closing the survey
// step succeeds with zero side effects. After the fix, closing spawns N
// child molecules with vars derived from the survey's output.
func TestOnComplete_RuntimeFanout_EndToEnd(t *testing.T) {
	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "c7g")

	formulaDir := filepath.Join(beadsDir, "formulas")
	if err := os.MkdirAll(formulaDir, 0o755); err != nil {
		t.Fatalf("mkdir formulas dir: %v", err)
	}

	// Worker formula — one step per spawned molecule. The worker_name var is
	// what on_complete substitutes from {item.name}.
	const workerTOML = `formula = "worker-arm"
version = 1
type = "workflow"

[vars.worker_name]
required = true

[[steps]]
id = "do-work"
title = "Worker {{worker_name}}"
type = "task"
`
	if err := os.WriteFile(filepath.Join(formulaDir, "worker-arm.formula.toml"), []byte(workerTOML), 0o644); err != nil {
		t.Fatalf("write worker formula: %v", err)
	}

	// Survey formula — surveyor declares on_complete fanout; join step gates
	// on the dynamic children via waits_for=all-children (the canonical
	// fanout-join recipe).
	const surveyTOML = `formula = "survey"
version = 1
type = "workflow"

[[steps]]
id = "survey-workers"
title = "Survey workers"
type = "task"

[steps.on_complete]
for_each = "output.items"
bond = "worker-arm"
parallel = true

[steps.on_complete.vars]
worker_name = "{item.name}"

[[steps]]
id = "join"
title = "Join after fanout"
type = "task"
needs = ["survey-workers"]
waits_for = "all-children"
`
	if err := os.WriteFile(filepath.Join(formulaDir, "survey.formula.toml"), []byte(surveyTOML), 0o644); err != nil {
		t.Fatalf("write survey formula: %v", err)
	}

	out, err := bdRunWithFlockRetry(t, bd, dir, "mol", "pour", "survey")
	if err != nil {
		t.Fatalf("bd mol pour failed: %v\n%s", err, out)
	}

	// Find the survey step's bd id from the workspace (the step with
	// title "Survey workers").
	listOut, err := bdRunWithFlockRetry(t, bd, dir, "list", "--all", "--json")
	if err != nil {
		t.Fatalf("bd list failed: %v\n%s", err, listOut)
	}
	surveyID := findIDByTitle(string(listOut), "Survey workers")
	if surveyID == "" {
		t.Fatalf("could not find survey step id in workspace:\n%s", listOut)
	}

	// Populate the runtime output that on_complete fans over. Three items
	// with distinct names so we can assert each got a substituted child.
	const outputMetadata = `{"output":{"items":[{"name":"alice"},{"name":"bob"},{"name":"carol"}]}}`
	out, err = bdRunWithFlockRetry(t, bd, dir, "update", surveyID, "--metadata", outputMetadata)
	if err != nil {
		t.Fatalf("bd update --metadata failed: %v\n%s", err, out)
	}

	// Close the survey step — this is what fires the on_complete executor.
	out, err = bdRunWithFlockRetry(t, bd, dir, "close", surveyID, "--reason", "survey complete")
	if err != nil {
		t.Fatalf("bd close failed: %v\n%s", err, out)
	}

	// Assert: three worker molecules were spawned under the survey step,
	// each containing a "Worker <name>" step substituted from {item.name}.
	// The direct children of survey-workers are the worker-arm molecule
	// roots; "Worker <name>" lives one level deeper. So we check the full
	// workspace listing.
	listOut2, err := bdRunWithFlockRetry(t, bd, dir, "list", "--all")
	if err != nil {
		t.Fatalf("bd list (post-close) failed: %v\n%s", err, listOut2)
	}
	listStr := string(listOut2)

	for _, name := range []string{"alice", "bob", "carol"} {
		expectedTitle := fmt.Sprintf("Worker %s", name)
		if !strings.Contains(listStr, expectedTitle) {
			t.Errorf("expected a workspace issue with title %q after on_complete fanout; got listing:\n%s", expectedTitle, listStr)
		}
	}

	// Also verify the spawned molecules are attached under the survey step
	// (the bond target), not floating at the workspace root.
	childrenOut, err := bdRunWithFlockRetry(t, bd, dir, "children", surveyID, "--json")
	if err != nil {
		t.Fatalf("bd children failed: %v\n%s", err, childrenOut)
	}
	if !strings.Contains(string(childrenOut), `"title":"worker-arm"`) && !strings.Contains(string(childrenOut), `"title": "worker-arm"`) {
		t.Errorf("expected the spawned worker-arm molecule roots to be direct children of survey-workers; got:\n%s", childrenOut)
	}
}

// findIDByTitle scans a bd list --json blob (which may be pretty-printed,
// hence the whitespace tolerance) for the first issue whose title matches
// exactly and returns its id. Returns empty string on miss.
func findIDByTitle(blob, wantTitle string) string {
	// Match both "title":"X" and "title": "X" (pretty-printed).
	candidates := []string{
		`"title":"` + wantTitle + `"`,
		`"title": "` + wantTitle + `"`,
	}
	var titleIdx int = -1
	var keyLen int
	for _, c := range candidates {
		if i := strings.Index(blob, c); i >= 0 {
			titleIdx = i
			keyLen = len(c)
			break
		}
	}
	if titleIdx < 0 {
		return ""
	}
	objStart := strings.LastIndex(blob[:titleIdx], "{")
	if objStart < 0 {
		return ""
	}
	return extractFirstJSONStringField(blob[objStart:titleIdx+keyLen], "id")
}
