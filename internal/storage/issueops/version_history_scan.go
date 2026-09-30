package issueops

import "github.com/steveyegge/beads/internal/types"

// UnversionableIssue names one issue whose metadata RecordVersionInTx would
// refuse to version, and the refusal it would return.
type UnversionableIssue struct {
	ID  string
	Err error
}

// FindUnversionableMetadata runs CheckMetadataVersionable over every issue in
// issues that the mint would version and returns the ones it refuses, in the
// order they were given. It is the core of the check made when versioned
// history is switched on: with history on, a write that introduces such a value
// aborts in its own transaction, but a row that already holds one -- written
// while history was off, or by a path that does not mint -- would fail every
// later write to it.
//
// It skips exactly the rows the mint skips, by the mint's own rule (IsWisp):
// ephemeral and no-history rows are never versioned, so a value they hold can
// never abort a write. It does no I/O; the caller loads the rows, which is what
// lets one function serve the direct store and the proxied route.
func FindUnversionableMetadata(issues []*types.Issue) []UnversionableIssue {
	var found []UnversionableIssue
	for _, issue := range issues {
		if issue == nil || IsWisp(issue) {
			continue
		}
		if err := CheckMetadataVersionable(issue.Metadata); err != nil {
			found = append(found, UnversionableIssue{ID: issue.ID, Err: err})
		}
	}
	return found
}
