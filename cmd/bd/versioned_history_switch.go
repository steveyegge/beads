package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	storageissueops "github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/versionedhistory"
	"github.com/steveyegge/beads/issueops"
)

// The resolver for the versioned-history switch -- which planes it reads, why they
// are OR'd, how a store that cannot answer is handled -- lives in
// internal/versionedhistory, so that bd doctor's fix package (which cannot import
// package main) applies the same rule. This file keeps the two names the rest of
// cmd/bd uses.

// versionedHistorySettingKey is the one spelling of the switch, used for both the
// store-config read and the `bd config set` line the help text and the off-refusal
// tell people to run. They must not drift, so it is defined once, in the package
// that reads it.
const versionedHistorySettingKey = versionedhistory.ConfigKey

// versionedHistoryEnabled reports whether THIS STORE is recording versions: its
// own settings row, and nothing else. It is what `bd versions` calls, where the
// read is the point and no capability gate precedes it.
//
// It deliberately does not fold in the environment or config.yaml the way the
// rule for writers does. Those are process-wide and can only turn recording on
// for the writes this process makes; `bd versions` only reads, so they cannot
// make the store record anything. Letting them answer here made a store that
// never recorded read as one that was, and "No versions recorded yet" is the
// empty answer this command's three outcomes exist to prevent. What other
// clients of the store will do is what the store's row says.
func versionedHistoryEnabled(ctx context.Context, st storage.DoltStorage) bool {
	return versionedhistory.StoreSetting(ctx, st)
}

// versionedHistoryRefusalListLimit is how many offending issues the refusal spells
// out. The count it leads with is always the whole number.
const versionedHistoryRefusalListLimit = 20

// checkVersionedHistoryCanBeEnabled is the check `bd config set
// versioned-history.enabled true` makes before it writes the setting, and it
// makes it for that value alone: any other key, and every value that does not
// turn recording on (`false` included), passes without reading a row.
//
// With history on, recording a version runs in the same transaction as the write,
// so a write that introduces a number outside the I-JSON exact-integer range, or
// a duplicate key, already fails atomically. A row that holds one BEFORE the
// switch is turned on -- written while history was off, or by a path that does not
// record -- would fail every later write to it. So the switch reads every issue
// the store would version, runs the very function recording runs over each one's
// metadata (issueops.FindUnversionableMetadata), and refuses, writing nothing,
// while any is refused.
//
// It reads through the same role accessor `bd list` does, so the direct and the
// proxied route both answer, and with the request that means "everything": every
// status, pinned rows, every type, and both planes (the scan itself skips the rows
// recording never versions). Limit 0 with no MaxRows is an unbounded read: a store
// larger than a page must not be checked on a page. A read that fails refuses the
// switch too, because a check that could not run has not passed.
//
// The environment and config.yaml planes do not pass through `bd config set`, so
// they are not checked here; for those, recording's own refusal at write time is
// the control.
func checkVersionedHistoryCanBeEnabled(ctx context.Context, key, value string) error {
	if key != versionedHistorySettingKey || !versionedhistory.ValueEnables(value) {
		return nil
	}
	reader, err := openIssueReader()
	if err != nil {
		return fmt.Errorf("cannot check the store's metadata before turning versioned history on: %w", err)
	}
	unlimited := 0
	page, err := reader.List(ctx, issueops.ListRequest{
		AllFlag:         true,
		IncludeAllTypes: true,
		Limit:           &unlimited,
		Brief:           true,
		SkipLabels:      true,
		SkipCounts:      true,
	})
	if err != nil {
		return fmt.Errorf("cannot check the store's metadata before turning versioned history on: %w", err)
	}
	rows := make([]*types.Issue, 0, len(page.Items))
	for _, item := range page.Items {
		rows = append(rows, item.Issue)
	}
	if found := storageissueops.FindUnversionableMetadata(rows); len(found) > 0 {
		return errors.New(unversionableMetadataRefusal(found))
	}
	return nil
}

// unversionableMetadataRefusal is the text of the refusal: how many issues, which
// (the first versionedHistoryRefusalListLimit), what is wrong with each, and the
// fix. It says there is no override because there is none: the fix is one
// `bd update` per issue, made while history is off.
func unversionableMetadataRefusal(found []storageissueops.UnversionableIssue) string {
	noun := "issues"
	if len(found) == 1 {
		noun = "issue"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "cannot turn versioned history on: %d %s hold metadata a version could not record.\n", len(found), noun)
	shown := found
	if len(shown) > versionedHistoryRefusalListLimit {
		shown = shown[:versionedHistoryRefusalListLimit]
	}
	for _, f := range shown {
		fmt.Fprintf(&b, "  %s  %v\n", f.ID, f.Err)
	}
	if len(found) > len(shown) {
		fmt.Fprintf(&b, "  ... and %d more\n", len(found)-len(shown))
	}
	b.WriteString("With history on, the first write to each of these issues would fail. Fix each one while history is off, one command per issue, then run this command again:\n")
	b.WriteString("  bd update <id> --metadata '{\"<key>\": \"<value as a string>\"}'   replace the value; a string keeps it exact\n")
	b.WriteString("  bd update <id> --unset-metadata <key>                              or remove the key\n")
	b.WriteString("There is no override.")
	return b.String()
}
