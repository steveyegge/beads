//go:build cgo

package main

import (
	"os"
	"strings"
	"testing"
)

// THE NAME IS LOAD-BEARING for the same reason TestEmbeddedVersionedHistorySwitchRoundTrip's is:
// .github/scripts/embedded-test-shard.sh selects the tests the embedded job runs by
// `grep -rh '^func TestEmbedded' cmd/bd/*_embedded_test.go`, so a test that keeps this prefix
// and this file suffix runs in the one job that sets BEADS_TEST_EMBEDDED_DOLT=1.
//
// TestEmbeddedVersionedHistorySwitchRefusesUnversionableMetadata drives the REAL
// `bd config set versioned-history.enabled true` against a real store that already holds rows
// a version could not be recorded for.
//
// With history on, recording a version runs in the same transaction as the write, so a write
// that introduces a number outside the I-JSON exact-integer range already fails atomically.
// What that cannot help is a row that holds such a number BEFORE the switch is turned on:
// written while history was off, or through a path that does not mint (bd sql, an import).
// Once history is on, every later write to that row would fail. The switch therefore checks
// first, with the same function the mint runs, and refuses to turn history on while any
// issue the mint would version holds a value it would refuse.
//
// What this pins, end to end and in a database of its own:
//   - the refusal exits non-zero and prints the count, the ids and the remedy,
//   - it names only the issues the mint would version: an ephemeral row and a no-history row
//     holding the same value are never versioned, so they are not listed and do not block,
//   - it writes NOTHING: the setting is still off afterwards,
//   - turning history OFF never scans,
//   - once the rows are fixed with the command the refusal names, the switch turns on.
func TestEmbeddedVersionedHistorySwitchRefusesUnversionableMetadata(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	// The environment can only turn recording on and would make a passing run prove nothing
	// about the store's own setting, so clear it. No t.Parallel: t.Setenv is incompatible.
	t.Setenv("BD_VERSIONED_HISTORY_ENABLED", "")

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "vsc")

	const timestampInNanoseconds = `{"ts":1727000000000000000}`
	const largeInteger = `{"n":9007199254740993}`

	clean := bdCreateSilent(t, bd, dir, "clean", "--metadata", `{"ok":1,"fraction":0.1}`)
	first := bdCreateSilent(t, bd, dir, "holds a nanosecond timestamp", "--metadata", timestampInNanoseconds)
	second := bdCreateSilent(t, bd, dir, "holds an integer past 2^53", "--metadata", largeInteger)
	wisp := bdCreateSilent(t, bd, dir, "ephemeral, never versioned", "--ephemeral", "--metadata", timestampInNanoseconds)
	noHistory := bdCreateSilent(t, bd, dir, "no-history, never versioned", "--no-history", "--metadata", timestampInNanoseconds)

	out, code := bdRunFailCode(t, bd, dir, "config", "set", "versioned-history.enabled", "true")
	if code == 0 {
		t.Fatalf("enabling versioned history over unrecordable rows exited 0; out=%s", out)
	}
	for _, id := range []string{first, second} {
		if !strings.Contains(out, id) {
			t.Errorf("the refusal does not name %s, which holds a value a version could not record:\n%s", id, out)
		}
	}
	for name, id := range map[string]string{"the clean issue": clean, "the ephemeral issue": wisp, "the no-history issue": noHistory} {
		if strings.Contains(out, id) {
			t.Errorf("the refusal names %s (%s), which the mint would never version:\n%s", name, id, out)
		}
	}
	if !strings.Contains(out, "2 issues") {
		t.Errorf("the refusal does not give the count of offending issues (2):\n%s", out)
	}
	for _, want := range []string{"bd update", "--metadata", "I-JSON"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not contain %q, so it does not say what is wrong or how to fix it:\n%s", want, out)
		}
	}

	// It refuses WITHOUT writing: the setting is still off.
	if got := strings.TrimSpace(bdConfig(t, bd, dir, "get", "versioned-history.enabled")); strings.HasPrefix(got, "true") {
		t.Fatalf("the refused `config set ... true` wrote the setting anyway: config get says %q", got)
	}

	// Turning it off never scans, so it works with offenders present and is how a
	// store that is somehow already on gets out from under a poisoned row.
	bdConfig(t, bd, dir, "set", "versioned-history.enabled", "false")

	// The remedy the refusal names: one update per row, while history is off.
	bdRunOK(t, bd, dir, "update", first, "--metadata", `{"ts":"1727000000000000000"}`)
	bdRunOK(t, bd, dir, "update", second, "--unset-metadata", "n")

	bdConfig(t, bd, dir, "set", "versioned-history.enabled", "true")
	if got := strings.TrimSpace(bdConfig(t, bd, dir, "get", "versioned-history.enabled")); !strings.HasPrefix(got, "true") {
		t.Fatalf("after fixing every row the switch did not turn on: config get says %q", got)
	}
}
