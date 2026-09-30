package issueops

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestAsOfReadMapsAnyStoredRestrictionOutsideTheVocabularyToUnknown is finding
// 5 of the #6661 review. removed_restriction is a free VARCHAR(30) written by
// whatever produced the row, and AsOfReadInTx handed it back as an
// AsOfRestriction by a plain cast. The type's doc says the answer is one of four
// values and that AsOfRestrictionLive is never returned (Live is the absence of
// removed_at, not a stored value), yet "live" and any other string passed
// straight through, so a caller switching on the result saw a value the
// vocabulary does not contain and could not tell it from an answer.
//
// A stored value outside the four says nothing about why the row was removed,
// so it is reported as unknown: this store cannot say. The hyphenated spelling
// is in the table because it is what the migration header used to tell a writer
// to store.
func TestAsOfReadMapsAnyStoredRestrictionOutsideTheVocabularyToUnknown(t *testing.T) {
	const (
		issueID  = "bd-1"
		revision = int64(3)
		reason   = "retention window elapsed"
	)
	removedAt := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	for _, tt := range []struct {
		name   string
		stored any // the removed_restriction column; nil is SQL NULL
		want   AsOfRestriction
	}{
		{"retention", "gone_retention", AsOfRestrictionGoneRetention},
		{"erasure", "gone_erasure", AsOfRestrictionGoneErasure},
		{"reorganization", "gone_reorganization", AsOfRestrictionGoneReorganization},
		{"unknown", "unknown", AsOfRestrictionUnknown},
		{"empty string", "", AsOfRestrictionUnknown},
		{"NULL", nil, AsOfRestrictionUnknown},
		{"live is never a stored answer", "live", AsOfRestrictionUnknown},
		{"hyphenated spelling of a real value", "gone-retention", AsOfRestrictionUnknown},
		{"arbitrary text", "quarantined", AsOfRestrictionUnknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, mock, tx := beginMockTx(t)
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM issue_versions WHERE issue_id = ? AND revision = ?`)).
				WithArgs(issueID, revision).
				WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
			mock.ExpectQuery(`SELECT durable_state, removed_at, removed_reason, removed_restriction\s+FROM issue_versions`).
				WithArgs(issueID, revision).
				WillReturnRows(sqlmock.NewRows([]string{"durable_state", "removed_at", "removed_reason", "removed_restriction"}).
					AddRow([]byte("{}"), removedAt, reason, tt.stored))

			got, err := AsOfReadInTx(context.Background(), tx, "store", issueID,
				AsOfSelector{Address: AsOfVersionAddress(issueID, revision)})
			if err != nil {
				t.Fatalf("AsOfReadInTx: %v", err)
			}
			if !got.Refused {
				t.Fatalf("a version row with removed_at set must be refused, got %+v", got)
			}
			if got.Restriction != tt.want {
				t.Errorf("stored removed_restriction %#v: Restriction = %q, want %q", tt.stored, got.Restriction, tt.want)
			}
			if got.Reason != reason {
				t.Errorf("Reason = %q, want %q: the stored reason must survive the restriction being normalised", got.Reason, reason)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}
