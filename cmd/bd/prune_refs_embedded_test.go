//go:build cgo

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// createOpenWithBody creates an open task bead with the given description body.
// Returns the issue ID.
func createOpenWithBody(t *testing.T, bd, dir, title, body string) string {
	t.Helper()
	issue := bdCreate(t, bd, dir, title, "-t", "task", "-p", "2", "--description", body)
	return issue.ID
}

// requireBeadExists verifies that a bead with the given ID is accessible via bd show.
func requireBeadExists(t *testing.T, bd, dir, id string) {
	t.Helper()
	bdShow(t, bd, dir, id)
}

// requireBeadAbsent verifies that a bead with the given ID has been deleted.
func requireBeadAbsent(t *testing.T, bd, dir, id string) {
	t.Helper()
	bdShowFail(t, bd, dir, id)
}

// requireJSONField parses JSON output and asserts that key == wantVal.
func requireJSONField(t *testing.T, out, key string, wantVal interface{}) {
	t.Helper()
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("requireJSONField: no JSON object in output:\n%s", out)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out[start:]), &m); err != nil {
		// Try decoder for trailing content.
		dec := json.NewDecoder(strings.NewReader(out[start:]))
		if err2 := dec.Decode(&m); err2 != nil {
			t.Fatalf("requireJSONField: failed to parse JSON: %v\nraw: %s", err, out)
		}
	}
	got, ok := m[key]
	if !ok {
		t.Errorf("requireJSONField: key %q not found in %v", key, m)
		return
	}
	if got != wantVal {
		t.Errorf("requireJSONField: key %q = %v, want %v", key, got, wantVal)
	}
}

// requireJSONFieldAbsent parses JSON output and asserts that key is NOT present.
func requireJSONFieldAbsent(t *testing.T, out, key string) {
	t.Helper()
	start := strings.Index(out, "{")
	if start < 0 {
		// No JSON at all — key is absent by definition.
		return
	}
	var m map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(out[start:]))
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("requireJSONFieldAbsent: failed to parse JSON: %v\nraw: %s", err, out)
	}
	if _, ok := m[key]; ok {
		t.Errorf("requireJSONFieldAbsent: key %q unexpectedly present; full map: %v", key, m)
	}
}

// requireJSONIDSampleContains parses the referenced_ids_sample field from JSON
// output and asserts that id is in the list.
func requireJSONIDSampleContains(t *testing.T, out, id string) {
	t.Helper()
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("requireJSONIDSampleContains: no JSON object in output:\n%s", out)
	}
	var m map[string]interface{}
	dec := json.NewDecoder(strings.NewReader(out[start:]))
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("requireJSONIDSampleContains: failed to parse JSON: %v\nraw: %s", err, out)
	}
	raw, ok := m["referenced_ids_sample"]
	if !ok {
		t.Errorf("requireJSONIDSampleContains: key \"referenced_ids_sample\" not found; map: %v", m)
		return
	}
	sample, ok := raw.([]interface{})
	if !ok {
		t.Errorf("requireJSONIDSampleContains: \"referenced_ids_sample\" is not a list: %T %v", raw, raw)
		return
	}
	for _, v := range sample {
		if s, ok := v.(string); ok && s == id {
			return
		}
	}
	t.Errorf("requireJSONIDSampleContains: %q not found in referenced_ids_sample %v", id, sample)
}

func TestEmbeddedPruneRefs(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)

	// T1. Referenced-by-open body is preserved (canonical case).
	t.Run("prune_skips_referenced_by_open_body", func(t *testing.T) {
		dir, _, _ := bdInit(t, bd, "--prefix", "pr1")
		referenced := createAndClose(t, bd, dir, "ADR-style closed")
		plain := createAndClose(t, bd, dir, "Plain closed")
		_ = createOpenWithBody(t, bd, dir, "Verifier",
			"See "+referenced+" §3 for the rollback path.")

		out := bdPrune(t, bd, dir, "--pattern", "pr1-*", "--force", "--json")
		requireJSONField(t, out, "pruned_count", float64(1))
		requireJSONField(t, out, "referenced_skipped", float64(1))
		requireJSONIDSampleContains(t, out, referenced)
		requireBeadExists(t, bd, dir, referenced) // protected
		requireBeadAbsent(t, bd, dir, plain)      // got pruned
	})

	// T2. Reference in a comment (not body) is also preserved.
	t.Run("prune_skips_referenced_by_open_comment", func(t *testing.T) {
		dir, _, _ := bdInit(t, bd, "--prefix", "pr2")
		referenced := createAndClose(t, bd, dir, "Closed via comment ref")
		open := bdCreate(t, bd, dir, "Verifier (no body ref)").ID
		bdComment(t, bd, dir, open, "Verified rollback against "+referenced+".")

		bdPrune(t, bd, dir, "--pattern", "pr2-*", "--force")
		requireBeadExists(t, bd, dir, referenced)
	})

	// T3. Word-boundary precision: a superstring of target's ID must NOT match.
	// The open bead body mentions `target + "x"` — not the same ID.
	// \b ensures `be-abc123` does not match inside `be-abc123x` since both
	// sides of the suffix boundary are word characters.
	t.Run("prune_word_boundary_no_substring_collision", func(t *testing.T) {
		dir, _, _ := bdInit(t, bd, "--prefix", "pr3")
		target := createAndClose(t, bd, dir, "Boundary target closed")
		// Append an alphanumeric char to build a superstring.
		_ = createOpenWithBody(t, bd, dir, "Open w/ superstring",
			"Investigated "+target+"x — different bead, do not confuse.")

		bdPrune(t, bd, dir, "--pattern", "pr3-*", "--force")
		// Superstring doesn't match — target is pruned (not protected).
		requireBeadAbsent(t, bd, dir, target)
	})

	// T4. --ignore-references deletes referenced beads.
	t.Run("prune_ignore_references_deletes_anyway", func(t *testing.T) {
		dir, _, _ := bdInit(t, bd, "--prefix", "pr4")
		referenced := createAndClose(t, bd, dir, "Cited but doomed")
		_ = createOpenWithBody(t, bd, dir, "Cite", "ref: "+referenced)

		out := bdPrune(t, bd, dir, "--pattern", "pr4-*",
			"--ignore-references", "--force", "--json")
		requireJSONField(t, out, "pruned_count", float64(1))
		requireJSONFieldAbsent(t, out, "referenced_skipped") // not present when override active
		requireBeadAbsent(t, bd, dir, referenced)
	})

	// T5. Empty candidate set short-circuits — no error, pruned_count=0.
	t.Run("prune_empty_candidates_short_circuits", func(t *testing.T) {
		dir, _, _ := bdInit(t, bd, "--prefix", "pr5")
		// No closed beads at all.
		_ = bdCreate(t, bd, dir, "Just open").ID

		out := bdPrune(t, bd, dir, "--pattern", "pr5-*", "--force", "--json")
		requireJSONField(t, out, "pruned_count", float64(0))
	})

	// T6. Closed bead referencing closed bead → no protection.
	t.Run("prune_closed_bead_referencing_closed_is_not_protected", func(t *testing.T) {
		dir, _, _ := bdInit(t, bd, "--prefix", "pr6")
		target := createAndClose(t, bd, dir, "Will be pruned")
		citerClosed := bdCreate(t, bd, dir, "Cites "+target).ID
		bdUpdate(t, bd, dir, citerClosed,
			"--description", "References "+target+" — but I'm closed too.")
		bdClose(t, bd, dir, citerClosed)

		bdPrune(t, bd, dir, "--pattern", "pr6-*", "--force")
		// Closed-to-closed reference: no protection.
		requireBeadAbsent(t, bd, dir, target)
	})

	// T7. Reference in notes (not description or comments) is also preserved.
	t.Run("prune_skips_referenced_by_open_notes", func(t *testing.T) {
		dir, _, _ := bdInit(t, bd, "--prefix", "pr7")
		referenced := createAndClose(t, bd, dir, "Cited via notes")
		open := bdCreate(t, bd, dir, "Notes citer").ID
		bdUpdate(t, bd, dir, open,
			"--append-notes", "Carry-forward: "+referenced+".")

		bdPrune(t, bd, dir, "--pattern", "pr7-*", "--force")
		requireBeadExists(t, bd, dir, referenced)
	})
}
