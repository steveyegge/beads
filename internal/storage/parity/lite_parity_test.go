//go:build gms_pure_go && integration_pg

// Cross-backend lite-mode parity (be-uwvs.4).
//
// Seeds an identical fixture into a freshly-initialized Dolt backend and a
// freshly-initialized Postgres backend, runs `bd list --json` against each
// (the listing/routing path that `IssueFilter.Lite` targets), and asserts
// the JSON output is byte-identical after normalization.
//
// Why "lite" parity is its own scenario:
//
//   - The other parity scenarios (lifecycle_parity_test.go) exercise
//     mutating verbs and compare `bd export --json` end-state. They are
//     blind to listing-path shape divergence because export touches every
//     row regardless of SELECT projection.
//   - `bd list --json` is the canonical consumer of `IssueFilter.Lite`
//     (designer §2). A backend that returns extra heavy fields, or that
//     drops a heavy field that the other includes, produces silently
//     divergent JSON for hooks/agents downstream.
//   - Per architect R-07, the lite path multiplies the impact of any PG
//     filter-coverage gap (be-tjiy). A regression that lets the lite path
//     skip a filter — without producing a visible error — is exactly the
//     class of bug this test gates.
//
// The seed fixture intentionally populates each heavy column with a
// distinctive marker. Any backend that hydrates fewer columns than its
// peer surfaces as a differing JSON line, and the test diagnostic prints
// the byte-level diff for triage.
//
// Today's state (2026-05-12, validator handoff): `bd list --json` does
// NOT default to `Lite=true`; CLI wiring lands in be-uwvs.2/3. Until that
// wiring is in place, this scenario exercises the FULL path on both
// backends — it remains useful as a parity gate for full-mode `bd list
// --json` and will automatically gate lite-mode parity once the CLI
// default flips.

package parity

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestLiteParity_BdListJSON is the cross-backend parity gate for the
// listing/routing path that IssueFilter.Lite targets. It seeds the same
// heavy-bead fixture into a Dolt-backed and a PG-backed bd home, runs
// `bd list --json` against each, and asserts byte-identical output.
//
// Today, `bd list --json` is full-mode on both backends — so the test
// gates full-mode parity of the listing JSON. When CLI wiring for the
// lite default lands (be-uwvs.2/3), the SAME test gates lite-mode parity
// automatically, with no test edit required. The contract under test is
// "all backends produce the same `bd list --json` output on the same
// fixture" regardless of which SELECT shape backs it.
func TestLiteParity_BdListJSON(t *testing.T) {
	skipOnWindows(t)
	bd := buildBD(t)
	doltDir, pgDir, doltEnv, pgEnv := initLifecyclePair(t, bd)

	seed := []lifecycleCommand{
		// Heavy-bead trio: each issue has a distinct heavy-field marker so
		// a backend that drops a heavy field surfaces as a per-line diff.
		// Three rows is enough that ORDER BY ambiguities (priority,
		// created_at, id) get exercised: rows share a priority so the test
		// indirectly gates that both backends apply the same tiebreaker.
		{
			name: "create-1",
			args: []string{"create", "Alpha", "-t", "task", "-p", "2",
				"-d", "alpha-description",
				"--json"},
			captureID: 1,
		},
		{
			name: "create-2",
			args: []string{"create", "Beta", "-t", "task", "-p", "2",
				"-d", "beta-description",
				"--json"},
			captureID: 2,
		},
		{
			name: "create-3",
			args: []string{"create", "Gamma", "-t", "task", "-p", "1",
				"-d", "gamma-description",
				"--json"},
			captureID: 3,
		},
		{name: "label-1", args: []string{"label", "add", "{ID1}", "area:lite"}},
		{name: "label-2", args: []string{"label", "add", "{ID2}", "area:lite"}},
		{name: "label-3", args: []string{"label", "add", "{ID3}", "area:routing"}},
	}

	doltState := &lifecycleState{}
	pgState := &lifecycleState{}

	doltTranscript := applyLifecycleSteps(t, bd, doltDir, doltEnv, doltState, seed)
	pgTranscript := applyLifecycleSteps(t, bd, pgDir, pgEnv, pgState, seed)

	if len(doltState.ids) != len(pgState.ids) {
		t.Fatalf("captured id count divergence after seed: dolt=%d (%v) pg=%d (%v)",
			len(doltState.ids), doltState.ids, len(pgState.ids), pgState.ids)
	}

	doltList := bdListJSONNormalized(t, bd, doltDir, doltEnv, doltState.ids)
	pgList := bdListJSONNormalized(t, bd, pgDir, pgEnv, pgState.ids)

	if doltList == pgList {
		return
	}

	diff := unifiedDiff(doltList, pgList)
	t.Errorf("backend divergence in bd list --json:\n%s\n--- dolt transcript ---\n%s\n--- pg transcript ---\n%s",
		diff, truncate(doltTranscript, 4000), truncate(pgTranscript, 4000))
}

// bdListJSONNormalized invokes `bd list --json` against dir, substitutes
// captured issue ids with their positional placeholders, and applies the
// shared normalization passes. The returned string is the canonical form
// used for byte-level diffing across backends.
//
// Mirrors exportNormalized (lifecycle_parity_test.go) but targets the
// listing path rather than export. We do NOT strip dependency
// `created_by` / `metadata` fields here because `bd list` does not embed
// dep rows in its output; the per-issue records lack the gap that
// lifecycle parity has to work around.
func bdListJSONNormalized(t *testing.T, bd, dir string, env []string, ids []string) string {
	t.Helper()
	stdout, stderr, err := runBDEnv(bd, dir, env, []string{"list", "--json"})
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("bd list --json (%s): exit=%d\nstdout: %s\nstderr: %s",
				filepath.Base(dir), exitErr.ExitCode(), stdout, stderr)
		}
		t.Fatalf("bd list --json (%s): %v\nstdout: %s\nstderr: %s",
			filepath.Base(dir), err, stdout, stderr)
	}
	rewritten := substituteCapturedIDs(stdout, ids)
	return normalizeOutput(rewritten)
}
