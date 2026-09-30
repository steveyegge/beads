package issueops

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/steveyegge/beads/internal/storage/sqlbuild"
	"github.com/steveyegge/beads/internal/types"
)

// With versioned history on, the mint runs in the same transaction as the
// mutation that reaches it, so refusing at the mint IS refusing the write: a
// number outside the I-JSON range aborts the writer's own transaction and
// nothing it did is committed. The mint's reads run first and its two writes,
// the version row and the current_revision advance, come last, so a refusal
// must arrive after the reads and before either write.
//
// The mock scripts exactly the reads. It expects no INSERT into issue_versions
// and no UPDATE of issues.current_revision, so a mint that wrote before it
// refused would make the mock fail the call with an unexpected-statement error
// instead of the refusal asserted here. The caller's own rollback closes the
// transaction, as every store does when the mint returns an error.
func TestRecordVersionInTxRefusesANumberOutsideTheRangeAndWritesNothing(t *testing.T) {
	const id = "bd-9"
	_, mock, tx := beginMockTx(t)
	defer ScopeVersionedHistoryTransaction(tx, true)()

	values := issueRowValues(id, "carries an out-of-range number")
	for i, col := range issueColumns() {
		if col == "metadata" {
			values[i] = `{"ts":1727000000000000000}`
		}
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + IssueSelectColumns + " FROM issues " + sqlbuild.LeaseJoin("issues") + " WHERE id = ?")).
		WithArgs(id).
		WillReturnRows(issueRows().AddRow(values...))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT label FROM labels WHERE issue_id = ? ORDER BY label")).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"label"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1 FROM wisps LIMIT 1")).
		WillReturnRows(sqlmock.NewRows([]string{"1"}))
	mock.ExpectQuery(`FROM dependencies WHERE issue_id IN`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"issue_id", "depends_on_id", "type", "created_at", "created_by", "metadata", "thread_id"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT epoch FROM store_epoch WHERE id = 1")).
		WillReturnRows(sqlmock.NewRows([]string{"epoch"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(revision), 0) + 1 FROM issue_versions WHERE issue_id = ?")).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"next"}).AddRow(1))
	mock.ExpectRollback()

	err := RecordVersionInTx(context.Background(), tx, id, "actor")
	if !errors.Is(err, ErrIntegerNotRepresentable) {
		t.Fatalf("RecordVersionInTx = %v, want ErrIntegerNotRepresentable", err)
	}
	if !strings.Contains(err.Error(), id) {
		t.Errorf("refusal %q does not name the issue %s", err, id)
	}
	if !strings.Contains(err.Error(), "1727000000000000000") {
		t.Errorf("refusal %q does not name the offending literal", err)
	}
	if rbErr := tx.Rollback(); rbErr != nil {
		t.Fatalf("rolling back the writer's transaction: %v", rbErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the mint read or wrote something other than the scripted reads: %v", err)
	}
}

// refusalClass names what an admission verdict is, so two functions can be
// compared for agreement without depending on their exact wording.
func refusalClass(err error) string {
	switch {
	case err == nil:
		return "admitted"
	case errors.Is(err, ErrIntegerNotRepresentable):
		return "number outside the I-JSON range"
	case strings.Contains(err.Error(), "Duplicate key"):
		return "duplicate key"
	default:
		return "other refusal"
	}
}

// The check the pre-enable scan runs and the check the mint runs must never
// disagree, or a store could pass the scan, be switched on, and then refuse the
// first write to the row the scan cleared. So the check is the mint's own two
// steps behind one exported name, and this pins that they agree on every shape
// of metadata: a number past the range, a fraction rounding past it, duplicate
// keys at the top level and nested, ordinary values, null, empty and an empty
// object, and text that is not JSON at all.
func TestCheckMetadataVersionableAgreesWithTheMint(t *testing.T) {
	for _, tc := range []struct {
		name string
		meta string
		want string
	}{
		{"number past the range", `{"ts":1727000000000000000}`, "number outside the I-JSON range"},
		{"fraction rounding past the range", `{"n":9007199254740993.5}`, "number outside the I-JSON range"},
		{"overflowing exponent", `{"n":1e400}`, "number outside the I-JSON range"},
		{"number nested in an array", `{"a":[1,{"b":-9007199254740993}]}`, "number outside the I-JSON range"},
		{"duplicate key", `{"k":1,"k":2}`, "duplicate key"},
		{"nested duplicate key", `{"a":{"k":1,"k":2}}`, "duplicate key"},
		{"ordinary values", `{"a":1,"b":[0.1,2,"x"],"c":{"d":null}}`, "admitted"},
		{"number at the bound", `{"n":9007199254740991}`, "admitted"},
		{"tiny magnitude", `{"n":1e-400}`, "admitted"},
		{"null", `null`, "admitted"},
		{"empty object", `{}`, "admitted"},
		{"empty", ``, "admitted"},
		{"text that is not JSON", `{"a":`, "other refusal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := CheckMetadataVersionable(json.RawMessage(tc.meta))
			_, mint := canonicalDurableState(issueWithMetadata(tc.meta))
			if got := refusalClass(check); got != tc.want {
				t.Errorf("CheckMetadataVersionable(%q) = %v (%s), want %s", tc.meta, check, got, tc.want)
			}
			if got := refusalClass(mint); got != tc.want {
				t.Errorf("canonicalDurableState(metadata %q) = %v (%s), want %s", tc.meta, mint, got, tc.want)
			}
			if refusalClass(check) != refusalClass(mint) {
				t.Errorf("the pre-enable check and the mint disagree on %q: check %v, mint %v", tc.meta, check, mint)
			}
		})
	}
}

// The scan runs the check over every issue the mint would version and skips
// the ones it would not, by the mint's own rule (IsWisp): ephemeral rows and
// no-history rows are never versioned, so a poisoned value in one can never
// abort a write and is not an obstacle to switching history on. The scan
// reports what it found in the order it was given, names each offender with
// the refusal the mint would return, and finds nothing in a clean store.
func TestFindUnversionableMetadataFindsWhatTheMintWouldRefuse(t *testing.T) {
	oversize := json.RawMessage(`{"ts":1727000000000000000}`)
	issues := []*types.Issue{
		{ID: "bd-1", Metadata: oversize},
		{ID: "bd-2", Metadata: json.RawMessage(`{"k":1,"k":2}`)},
		{ID: "bd-3", Metadata: json.RawMessage(`{"ok":1}`)},
		{ID: "bd-4", Ephemeral: true, Metadata: oversize},
		{ID: "bd-5", NoHistory: true, Metadata: oversize},
		{ID: "bd-6"},
		{ID: "bd-7", Metadata: json.RawMessage(`{"n":9007199254740993.5}`)},
		{ID: "bd-8", Ephemeral: true, WispPlaneOverride: boolPtr(false), Metadata: oversize},
		{ID: "bd-9", WispPlaneOverride: boolPtr(true), Metadata: oversize},
	}

	got := FindUnversionableMetadata(issues)

	want := []struct {
		id    string
		class string
	}{
		{"bd-1", "number outside the I-JSON range"},
		{"bd-2", "duplicate key"},
		{"bd-7", "number outside the I-JSON range"},
		// The override decides which plane the mint routes an issue to, and the
		// scan follows it: bd-8 is versioned despite Ephemeral, bd-9 is not.
		{"bd-8", "number outside the I-JSON range"},
	}
	if len(got) != len(want) {
		t.Fatalf("FindUnversionableMetadata found %d issues %v, want %d", len(got), unversionableIDs(got), len(want))
	}
	for i, w := range want {
		if got[i].ID != w.id {
			t.Fatalf("offender %d is %s, want %s (order of the input); got %v", i, got[i].ID, w.id, unversionableIDs(got))
		}
		if class := refusalClass(got[i].Err); class != w.class {
			t.Errorf("%s: refusal class %q (%v), want %q", w.id, class, got[i].Err, w.class)
		}
	}

	clean := []*types.Issue{
		{ID: "bd-1", Metadata: json.RawMessage(`{"ok":1,"f":0.1}`)},
		{ID: "bd-2"},
		{ID: "bd-3", Metadata: json.RawMessage(`null`)},
		{ID: "bd-4", Ephemeral: true, Metadata: oversize},
	}
	if found := FindUnversionableMetadata(clean); len(found) != 0 {
		t.Fatalf("a store with nothing the mint would refuse reported %v", unversionableIDs(found))
	}
	if found := FindUnversionableMetadata(nil); len(found) != 0 {
		t.Fatalf("no issues reported %v", unversionableIDs(found))
	}
}

func unversionableIDs(found []UnversionableIssue) []string {
	ids := make([]string, 0, len(found))
	for _, f := range found {
		ids = append(ids, f.ID)
	}
	return ids
}
