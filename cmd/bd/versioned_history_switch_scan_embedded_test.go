//go:build cgo

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/workapi"
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
//   - it names every issue the mint would version, whatever its status or type: a closed issue,
//     a pinned one and a gate are all hidden from a default `bd list`, so a scan that read only
//     what `bd list` shows would clear the switch over them,
//   - it names only those: an ephemeral row and a no-history row holding the same value are
//     never versioned, so they are not listed and do not block,
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

	// Three more the mint versions that a default `bd list` does not show. The scan has to ask for
	// every status and every type to see them.
	closed := bdCreateSilent(t, bd, dir, "closed, still versioned", "--metadata", timestampInNanoseconds)
	bdRunOK(t, bd, dir, "close", closed)
	pinned := bdCreateSilent(t, bd, dir, "pinned, still versioned", "--metadata", largeInteger)
	bdRunOK(t, bd, dir, "update", pinned, "--status", "pinned")
	gate := bdCreateSilent(t, bd, dir, "a gate, still versioned", "--type", "gate", "--metadata", timestampInNanoseconds)

	out, code := bdRunFailCode(t, bd, dir, "config", "set", "versioned-history.enabled", "true")
	if code == 0 {
		t.Fatalf("enabling versioned history over unrecordable rows exited 0; out=%s", out)
	}
	offenders := map[string]string{
		"the open issue holding a timestamp":     first,
		"the open issue holding a large integer": second,
		"the closed issue":                       closed,
		"the pinned issue":                       pinned,
		"the gate":                               gate,
	}
	for name, id := range offenders {
		if !strings.Contains(out, id) {
			t.Errorf("the refusal does not name %s (%s), which holds a value a version could not record:\n%s", name, id, out)
		}
	}
	for name, id := range map[string]string{"the clean issue": clean, "the ephemeral issue": wisp, "the no-history issue": noHistory} {
		if strings.Contains(out, id) {
			t.Errorf("the refusal names %s (%s), which the mint would never version:\n%s", name, id, out)
		}
	}
	if want := fmt.Sprintf("%d issues", len(offenders)); !strings.Contains(out, want) {
		t.Errorf("the refusal does not give the count of offending issues (%d):\n%s", len(offenders), out)
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
	bdRunOK(t, bd, dir, "update", closed, "--metadata", `{"ts":"1727000000000000000"}`)
	bdRunOK(t, bd, dir, "update", pinned, "--unset-metadata", "n")
	bdRunOK(t, bd, dir, "update", gate, "--metadata", `{"ts":"1727000000000000000"}`)

	bdConfig(t, bd, dir, "set", "versioned-history.enabled", "true")
	if got := strings.TrimSpace(bdConfig(t, bd, dir, "get", "versioned-history.enabled")); !strings.HasPrefix(got, "true") {
		t.Fatalf("after fixing every row the switch did not turn on: config get says %q", got)
	}
}

// TestEmbeddedVersionedHistorySwitchScansEveryRowNotOnePage pins that the scan reads the whole
// store. A list read that sets no limit returns one page (workapi.DefaultListLimit rows), so a
// scan that stopped asking for everything would clear the switch over any offender past the first
// page and count only that page's worth in its refusal.
//
// The rows are imported in one process: creating a page and a half of issues one command at a
// time costs the better part of a minute in this job.
func TestEmbeddedVersionedHistorySwitchScansEveryRowNotOnePage(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Setenv("BD_VERSIONED_HISTORY_ENABLED", "")

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "vsp")

	// More rows than one page, and more than the refusal lists, so both counts are exercised.
	n := workapi.DefaultListLimit + 10
	var rows strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&rows, `{"title":"offender %03d","metadata":{"ts":1727000000000000000}}`+"\n", i)
	}
	file := filepath.Join(t.TempDir(), "offenders.jsonl")
	if err := os.WriteFile(file, []byte(rows.String()), 0o600); err != nil {
		t.Fatalf("writing the rows to import: %v", err)
	}
	bdRunOK(t, bd, dir, "import", file)

	out, _ := bdRunFailCode(t, bd, dir, "config", "set", "versioned-history.enabled", "true")
	if want := fmt.Sprintf("%d issues", n); !strings.Contains(out, want) {
		t.Errorf("the refusal does not count all %d offending issues (a scan that read one page would say %d):\n%s", n, workapi.DefaultListLimit, out)
	}
	if want := fmt.Sprintf("... and %d more", n-versionedHistoryRefusalListLimit); !strings.Contains(out, want) {
		t.Errorf("the refusal does not say %q after the first %d ids:\n%s", want, versionedHistoryRefusalListLimit, out)
	}
}

// TestEmbeddedVersionedHistorySetManyRefusesUnversionableMetadata drives the REAL
// `bd config set-many versioned-history.enabled=true` against a store that holds a row a version
// could not be recorded for. set-many writes the same setting as `bd config set` through a
// validation loop of its own, and it once turned history on over such a row with no check at
// all: the next write to that row then failed.
//
// What this pins, end to end and in a database of its own:
//   - the batch is refused: it exits non-zero and the refusal names the issue and the remedy,
//   - it writes NOTHING, neither the setting nor the other pair in the same batch, because the
//     check runs before any pair is written,
//   - a batch that turns history OFF never scans,
//   - once the row is fixed, the same batch turns history on and writes its other pair.
func TestEmbeddedVersionedHistorySetManyRefusesUnversionableMetadata(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Setenv("BD_VERSIONED_HISTORY_ENABLED", "")

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "vsm")

	offender := bdCreateSilent(t, bd, dir, "holds a nanosecond timestamp", "--metadata", `{"ts":1727000000000000000}`)

	// The other pair comes first, so a check made after the writes would already have stored it.
	out, _ := bdRunFailCode(t, bd, dir, "config", "set-many", "vsm.marker=1", "versioned-history.enabled=true")
	if !strings.Contains(out, offender) {
		t.Errorf("the refusal does not name %s, which holds a value a version could not record:\n%s", offender, out)
	}
	for _, want := range []string{"1 issue", "bd update", "--metadata", "I-JSON"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not contain %q, so it does not say what is wrong or how to fix it:\n%s", want, out)
		}
	}

	// It refuses WITHOUT writing: the setting is still off and the other pair was not stored.
	if got := strings.TrimSpace(bdConfig(t, bd, dir, "get", "versioned-history.enabled")); strings.HasPrefix(got, "true") {
		t.Fatalf("the refused `config set-many ... versioned-history.enabled=true` wrote the setting anyway: config get says %q", got)
	}
	if got := strings.TrimSpace(bdConfig(t, bd, dir, "get", "vsm.marker")); got == "1" {
		t.Fatalf("the refused batch stored its other pair anyway: config get vsm.marker says %q", got)
	}

	// Turning it off never scans, so a batch that does so works with the offender present.
	bdConfig(t, bd, dir, "set-many", "versioned-history.enabled=false")

	// The remedy the refusal names, then the same batch that was refused goes through.
	bdRunOK(t, bd, dir, "update", offender, "--unset-metadata", "ts")
	bdConfig(t, bd, dir, "set-many", "vsm.marker=1", "versioned-history.enabled=true")
	if got := strings.TrimSpace(bdConfig(t, bd, dir, "get", "versioned-history.enabled")); !strings.HasPrefix(got, "true") {
		t.Fatalf("after fixing the row the batch did not turn the switch on: config get says %q", got)
	}
	if got := strings.TrimSpace(bdConfig(t, bd, dir, "get", "vsm.marker")); got != "1" {
		t.Fatalf("after fixing the row the batch did not store its other pair: config get vsm.marker says %q", got)
	}
}
