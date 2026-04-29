//go:build cgo

package issueops

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/steveyegge/beads/internal/storage/schema"
	"github.com/steveyegge/beads/internal/types"
)

func TestClaimIssueInTxWithDialectSQLiteRecordsUUIDEvent(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "beads.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := schema.CreateIgnoredTablesSQLite(ctx, db); err != nil {
		t.Fatalf("CreateIgnoredTablesSQLite: %v", err)
	}
	if _, err := schema.MigrateUpSQLite(ctx, db); err != nil {
		t.Fatalf("MigrateUpSQLite: %v", err)
	}

	now := time.Now().UTC()
	issue := &types.Issue{
		ID:        "bd-claim",
		Title:     "claim sqlite",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		CreatedAt: now,
		UpdatedAt: now,
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO issues (
			id, title, description, design, acceptance_criteria, notes, status, priority, issue_type,
			created_at, updated_at, created_by, owner, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, issue.ID, issue.Title, issue.Description, "", "", "", issue.Status, issue.Priority, issue.IssueType,
		issue.CreatedAt, issue.UpdatedAt, "test", "test", "{}"); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := ClaimIssueInTxWithDialect(ctx, tx, issue.ID, "worker", SQLDialectSQLite); err != nil {
		t.Fatalf("ClaimIssueInTxWithDialect: %v", err)
	}

	got, err := GetIssueInTx(ctx, tx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssueInTx: %v", err)
	}
	if got.Assignee != "worker" {
		t.Fatalf("assignee = %q, want worker", got.Assignee)
	}
	if got.Status != types.StatusInProgress {
		t.Fatalf("status = %q, want %q", got.Status, types.StatusInProgress)
	}

	events, err := GetEventsInTx(ctx, tx, issue.ID, 10)
	if err != nil {
		t.Fatalf("GetEventsInTx: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected claim event")
	}
	if events[0].ID == "" {
		t.Fatal("claim event missing UUID")
	}
	if events[0].EventType != types.EventType("claimed") {
		t.Fatalf("event type = %q, want claimed", events[0].EventType)
	}
}
