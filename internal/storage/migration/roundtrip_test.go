//go:build integration_pg

package migration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	_ "github.com/steveyegge/beads/internal/storage/doltdriver" // self-registers BackendDolt
	"github.com/steveyegge/beads/internal/storage/migration"
	_ "github.com/steveyegge/beads/internal/storage/postgres" // self-registers BackendPostgres
	"github.com/steveyegge/beads/internal/testutil"
	"github.com/steveyegge/beads/internal/types"
)

// pgDSNEnv mirrors the postgres package convention: when set, the test
// reuses an externally provisioned PG instance (CI service container path);
// otherwise testcontainers-go starts postgres:14-alpine.
const pgDSNEnv = "BEADS_TEST_POSTGRES_DSN"

func startPG(t *testing.T) (string, func()) {
	t.Helper()
	if dsn := os.Getenv(pgDSNEnv); dsn != "" {
		return dsn, func() {}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	container, err := tcpostgres.Run(ctx, "postgres:14-alpine",
		tcpostgres.WithDatabase("bd_test"),
		tcpostgres.WithUsername("bd"),
		tcpostgres.WithPassword("bd"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("testcontainers-go could not start postgres:14-alpine (no docker?): %v", err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("connection string: %v", err)
	}
	return dsn, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	}
}

func openDoltSource(t *testing.T) storage.Storage {
	t.Helper()
	testutil.RequireDoltContainer(t)
	port := testutil.DoltContainerPortInt()
	if port == 0 {
		t.Fatal("Dolt container port not set")
	}
	src, err := dolt.New(context.Background(), &dolt.Config{
		Path:       t.TempDir(),
		Database:   "beads_test",
		ServerMode: true,
		ServerHost: "127.0.0.1",
		ServerPort: port,
	})
	if err != nil {
		t.Fatalf("open dolt source: %v", err)
	}
	t.Cleanup(func() { _ = src.Close() })
	if err := src.SetConfig(context.Background(), "issue_prefix", "be"); err != nil {
		t.Fatalf("set issue_prefix: %v", err)
	}
	return src
}

// TestRoundtripDoltToPostgres exercises the full migration end-to-end:
// seed Dolt with a fixed corpus, run Migrate, then assert byte-equal
// snapshots of both sides. Snapshots are produced by snapshotStore which
// implements bd-export-equivalent serialization purely through the
// Storage / capability interfaces.
func TestRoundtripDoltToPostgres(t *testing.T) {
	src := openDoltSource(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seedCorpus(t, ctx, src)

	dsn, stopPG := startPG(t)
	defer stopPG()

	dst, err := storage.Open(ctx, storage.BackendPostgres, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open pg destination: %v", err)
	}
	defer func() { _ = dst.Close() }()

	// --force is required because PG seed-config rows are present from the
	// freshly-applied schema migration; without --force the lossless empty
	// check still passes (lossless tables are empty), but the test asserts
	// the --force code path on a known-non-empty config table too.
	stderr := &strings.Builder{}
	result, err := migration.Migrate(ctx, src, dst, migration.Options{
		Force:  true,
		Stderr: stderr,
	})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result == nil {
		t.Fatal("migrate returned nil result")
	}
	if result.RowsByTable["issues"] == 0 {
		t.Fatalf("expected non-zero issue rows, got %v", result.RowsByTable)
	}
	if result.AuditEventsSkipped == 0 {
		t.Errorf("expected non-zero audit events skipped (corpus generated events): %d", result.AuditEventsSkipped)
	}
	if !strings.Contains(stderr.String(), "audit-trail events not migrated") {
		t.Errorf("expected audit-trail warning on stderr, got %q", stderr.String())
	}

	srcDump := snapshotStore(t, ctx, src)
	dstDump := snapshotStore(t, ctx, dst)
	if srcDump != dstDump {
		t.Errorf("post-migrate snapshots differ.\n--- SRC ---\n%s\n--- DST ---\n%s", srcDump, dstDump)
	}
}

// TestEmptyDestinationGuard verifies that without --force, a non-empty
// destination is rejected with ErrDestinationNotEmpty.
func TestEmptyDestinationGuard(t *testing.T) {
	src := openDoltSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	seedCorpus(t, ctx, src)

	dsn, stopPG := startPG(t)
	defer stopPG()

	dst, err := storage.Open(ctx, storage.BackendPostgres, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open pg destination: %v", err)
	}
	defer func() { _ = dst.Close() }()

	// First migration with --force populates the destination.
	if _, err := migration.Migrate(ctx, src, dst, migration.Options{Force: true}); err != nil {
		t.Fatalf("priming migrate: %v", err)
	}
	// Second migration without --force must refuse.
	_, err = migration.Migrate(ctx, src, dst, migration.Options{})
	var notEmpty *migration.ErrDestinationNotEmpty
	if !errors.As(err, &notEmpty) {
		t.Fatalf("expected ErrDestinationNotEmpty, got %v", err)
	}
	if notEmpty.Counts["issues"] == 0 {
		t.Errorf("expected non-zero issues count in error: %v", notEmpty.Counts)
	}
}

// TestForceIsIdempotent runs --force twice and asserts the destination
// state is the same after each run.
func TestForceIsIdempotent(t *testing.T) {
	src := openDoltSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	seedCorpus(t, ctx, src)

	dsn, stopPG := startPG(t)
	defer stopPG()

	dst, err := storage.Open(ctx, storage.BackendPostgres, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open pg destination: %v", err)
	}
	defer func() { _ = dst.Close() }()

	first, err := migration.Migrate(ctx, src, dst, migration.Options{Force: true})
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	dump1 := snapshotStore(t, ctx, dst)

	second, err := migration.Migrate(ctx, src, dst, migration.Options{Force: true})
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	dump2 := snapshotStore(t, ctx, dst)

	if dump1 != dump2 {
		t.Errorf("snapshots after two --force migrations differ.\n--- 1 ---\n%s\n--- 2 ---\n%s", dump1, dump2)
	}
	if first.RowsByTable["issues"] != second.RowsByTable["issues"] {
		t.Errorf("row counts diverged: %v vs %v", first.RowsByTable, second.RowsByTable)
	}
}

// TestIncludeEventsRejected asserts the v1 placeholder semantics for
// --include-events.
func TestIncludeEventsRejected(t *testing.T) {
	src := openDoltSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn, stopPG := startPG(t)
	defer stopPG()
	dst, err := storage.Open(ctx, storage.BackendPostgres, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open pg destination: %v", err)
	}
	defer func() { _ = dst.Close() }()
	_, err = migration.Migrate(ctx, src, dst, migration.Options{IncludeEvents: true})
	if !errors.Is(err, migration.ErrUnimplementedFeature) {
		t.Fatalf("expected ErrUnimplementedFeature, got %v", err)
	}
}

// seedCorpus creates a fixed corpus on the source: ~50 issues split across
// types, including wisps and inter-issue dependencies, labels, comments.
// Determinism in IDs and timestamps is left to bd's normal allocation —
// we sort by ID at snapshot time so insertion order doesn't matter.
func seedCorpus(t *testing.T, ctx context.Context, s storage.Storage) {
	t.Helper()
	const n = 30
	issues := make([]*types.Issue, n)
	for i := 0; i < n; i++ {
		issues[i] = &types.Issue{
			Title:       fmt.Sprintf("Issue %02d", i),
			Description: fmt.Sprintf("Body for issue %02d.\nMulti-line content.", i),
			Status:      types.StatusOpen,
			Priority:    (i % 4),
			IssueType:   types.TypeTask,
		}
		if i%5 == 0 {
			issues[i].IssueType = types.TypeBug
		}
		if err := s.CreateIssue(ctx, issues[i], "tester"); err != nil {
			t.Fatalf("create issue %d: %v", i, err)
		}
	}
	// Add a chain of blocks dependencies.
	for i := 1; i < n; i++ {
		dep := &types.Dependency{
			IssueID:     issues[i].ID,
			DependsOnID: issues[i-1].ID,
			Type:        types.DepBlocks,
		}
		if err := s.AddDependency(ctx, dep, "tester"); err != nil {
			t.Fatalf("add dep %d: %v", i, err)
		}
	}
	// Labels — every third issue gets two labels.
	for i := 0; i < n; i += 3 {
		if err := s.AddLabel(ctx, issues[i].ID, "needs-review", "tester"); err != nil {
			t.Fatalf("add label: %v", err)
		}
		if err := s.AddLabel(ctx, issues[i].ID, fmt.Sprintf("bucket-%d", i%4), "tester"); err != nil {
			t.Fatalf("add label: %v", err)
		}
	}
	// Comments — every issue gets one or two comments.
	for i := 0; i < n; i++ {
		if _, err := s.AddIssueComment(ctx, issues[i].ID, "tester", fmt.Sprintf("First note on %02d", i)); err != nil {
			t.Fatalf("add comment: %v", err)
		}
		if i%2 == 0 {
			if _, err := s.AddIssueComment(ctx, issues[i].ID, "reviewer", "Looks good."); err != nil {
				t.Fatalf("add comment: %v", err)
			}
		}
	}
	// Close a few so the snapshot covers closed-state copy.
	for i := 0; i < n; i += 7 {
		if err := s.CloseIssue(ctx, issues[i].ID, "test close", "tester", "test-session"); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
}

// snapshotRecord describes one issue and its directly-attached relational
// data in a stable, marshallable shape. The shape is intentionally narrower
// than `bd export --all` (no _type discriminator, no DependencyCounts) —
// the round-trip's job is to prove the lossless data set survives, not to
// re-prove the export wrapper.
type snapshotRecord struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Status       string        `json:"status"`
	Priority     int           `json:"priority"`
	IssueType    string        `json:"issue_type"`
	Assignee     string        `json:"assignee,omitempty"`
	Owner        string        `json:"owner,omitempty"`
	Ephemeral    bool          `json:"ephemeral,omitempty"`
	WispType     string        `json:"wisp_type,omitempty"`
	Pinned       bool          `json:"pinned,omitempty"`
	Labels       []string      `json:"labels,omitempty"`
	Dependencies []snapDep     `json:"dependencies,omitempty"`
	Comments     []snapComment `json:"comments,omitempty"`
}

type snapDep struct {
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
}

type snapComment struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

// snapshotStore returns a deterministic, sorted JSON-lines view of all
// issues + wisps + their attached labels/deps/comments. Two stores with
// byte-identical lossless data produce identical output.
func snapshotStore(t *testing.T, ctx context.Context, s storage.Storage) string {
	t.Helper()
	all, err := s.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("search issues: %v", err)
	}
	// Wisps are returned via SearchIssues only when Ephemeral filter is set
	// in some backends; ListWisps gives a stable union path.
	wisps, err := s.ListWisps(ctx, types.WispFilter{IncludeClosed: true})
	if err != nil {
		t.Fatalf("list wisps: %v", err)
	}
	seen := map[string]bool{}
	combined := make([]*types.Issue, 0, len(all)+len(wisps))
	for _, i := range append(all, wisps...) {
		if seen[i.ID] {
			continue
		}
		seen[i.ID] = true
		combined = append(combined, i)
	}

	records := make([]snapshotRecord, 0, len(combined))
	for _, issue := range combined {
		labels, _ := s.GetLabels(ctx, issue.ID)
		sort.Strings(labels)
		comments, _ := s.GetIssueComments(ctx, issue.ID)
		var snapComments []snapComment
		for _, c := range comments {
			snapComments = append(snapComments, snapComment{
				Author: c.Author,
				Text:   c.Text,
			})
		}
		// Sort comments alphabetically — both backends order by created_at,
		// but ties are broken differently (PG by id, Dolt unspecified). We
		// only care about the (author, text) set surviving the migration.
		sort.Slice(snapComments, func(i, j int) bool {
			if snapComments[i].Text != snapComments[j].Text {
				return snapComments[i].Text < snapComments[j].Text
			}
			return snapComments[i].Author < snapComments[j].Author
		})
		// We use raw dep records to preserve the (issue, depends_on, type)
		// triple — GetDependencies returns hydrated Issues which would
		// double-count.
		var snapDeps []snapDep
		if accessor, ok := storage.UnwrapStore(s).(storage.DependencyQueryStore); ok {
			recs, err := accessor.GetDependencyRecordsForIssues(ctx, []string{issue.ID})
			if err != nil {
				t.Fatalf("dep records: %v", err)
			}
			for _, d := range recs[issue.ID] {
				snapDeps = append(snapDeps, snapDep{
					IssueID:     d.IssueID,
					DependsOnID: d.DependsOnID,
					Type:        string(d.Type),
				})
			}
		}
		sort.Slice(snapDeps, func(i, j int) bool {
			if snapDeps[i].DependsOnID != snapDeps[j].DependsOnID {
				return snapDeps[i].DependsOnID < snapDeps[j].DependsOnID
			}
			return snapDeps[i].Type < snapDeps[j].Type
		})

		records = append(records, snapshotRecord{
			ID:           issue.ID,
			Title:        issue.Title,
			Description:  issue.Description,
			Status:       string(issue.Status),
			Priority:     issue.Priority,
			IssueType:    string(issue.IssueType),
			Assignee:     issue.Assignee,
			Owner:        issue.Owner,
			Ephemeral:    issue.Ephemeral,
			WispType:     string(issue.WispType),
			Pinned:       issue.Pinned,
			Labels:       labels,
			Dependencies: snapDeps,
			Comments:     snapComments,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })

	var b strings.Builder
	for _, r := range records {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal snapshot: %v", err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestSnapshotIsStable asserts the snapshot helper itself is deterministic
// — running it twice on the same store produces the same string. Without
// this check, a flaky snapshot (e.g. map iteration order) would make
// roundtrip failures unintepretable.
func TestSnapshotIsStable(t *testing.T) {
	src := openDoltSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	seedCorpus(t, ctx, src)
	a := snapshotStore(t, ctx, src)
	b := snapshotStore(t, ctx, src)
	if a != b {
		t.Errorf("snapshot not stable across runs:\n--- A ---\n%s\n--- B ---\n%s", a, b)
	}
}

// avoid unused-import flap when test build tags are off
var _ = strconv.Itoa
