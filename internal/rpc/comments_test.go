package rpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	sqlitestorage "github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

func TestCommentOperationsViaRPC(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	socketPath := filepath.Join(tmpDir, "bd.sock")

	store, err := sqlitestorage.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	server := NewServer(socketPath, store)

	ctx, cancel := context.WithCancel(context.Background())
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start(ctx)
	}()

	select {
	case <-server.WaitReady():
	case err := <-serverErr:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	client, err := TryConnect(socketPath)
	if err != nil {
		t.Fatalf("failed to connect to server: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil after successful connection")
	}
	defer client.Close()

	createResp, err := client.Create(&CreateArgs{
		Title:     "Comment test",
		IssueType: "task",
		Priority:  2,
	})
	if err != nil {
		t.Fatalf("create issue failed: %v", err)
	}

	var created types.Issue
	if err := json.Unmarshal(createResp.Data, &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected issue ID to be set")
	}

	addResp, err := client.AddComment(&CommentAddArgs{
		ID:     created.ID,
		Author: "tester",
		Text:   "first comment",
	})
	if err != nil {
		t.Fatalf("add comment failed: %v", err)
	}

	var added types.Comment
	if err := json.Unmarshal(addResp.Data, &added); err != nil {
		t.Fatalf("failed to decode add comment response: %v", err)
	}

	if added.Text != "first comment" {
		t.Fatalf("expected comment text 'first comment', got %q", added.Text)
	}

	listResp, err := client.ListComments(&CommentListArgs{ID: created.ID})
	if err != nil {
		t.Fatalf("list comments failed: %v", err)
	}

	var comments []*types.Comment
	if err := json.Unmarshal(listResp.Data, &comments); err != nil {
		t.Fatalf("failed to decode comment list: %v", err)
	}

	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Text != "first comment" {
		t.Fatalf("expected comment text 'first comment', got %q", comments[0].Text)
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}
	cancel()
	select {
	case err := <-serverErr:
		if err != nil && err != context.Canceled {
			t.Fatalf("server returned error: %v", err)
		}
	default:
	}
}
