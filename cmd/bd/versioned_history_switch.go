package main

import (
	"context"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/versionedhistory"
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

// versionedHistoryEnabled reports whether version recording is on for this store,
// reading BOTH planes bd stores configuration in. It is what `bd versions` calls,
// where the read is the point and no capability gate precedes it.
func versionedHistoryEnabled(ctx context.Context, st storage.DoltStorage) bool {
	return versionedhistory.Enabled(ctx, st)
}
