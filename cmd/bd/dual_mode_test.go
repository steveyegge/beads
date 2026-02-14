// dual_mode_test.go - Minimal daemon-only test framework for integration tests.
//
// This is a simplified version that only supports daemon mode (via RPC).
// Direct mode (SQLite) was removed as part of the SQLite-to-Dolt migration.

//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// TestMode indicates which mode the test is running in
type TestMode string

const (
	// DaemonMode: Commands communicate via RPC to a background daemon
	DaemonMode TestMode = "daemon"
)

// DualModeTestEnv provides a test environment for daemon-mode integration tests.
type DualModeTestEnv struct {
	t          *testing.T
	mode       TestMode
	tmpDir     string
	beadsDir   string
	dbPath     string
	socketPath string

	store  storage.Storage
	client *rpc.Client
	server *rpc.Server

	ctx    context.Context
	cancel context.CancelFunc
}

// DualModeTestFunc is the callback signature for RunDaemonModeOnly.
type DualModeTestFunc func(t *testing.T, env *DualModeTestEnv)

// Mode returns the current test mode.
func (e *DualModeTestEnv) Mode() TestMode {
	return e.mode
}

// CreateIssue creates an issue via daemon RPC.
func (e *DualModeTestEnv) CreateIssue(issue *types.Issue) error {
	args := &rpc.CreateArgs{
		Title:       issue.Title,
		Description: issue.Description,
		IssueType:   string(issue.IssueType),
		Priority:    issue.Priority,
	}
	resp, err := e.client.Create(args)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("create failed: %s", resp.Error)
	}

	var createdIssue types.Issue
	if err := json.Unmarshal(resp.Data, &createdIssue); err != nil {
		return fmt.Errorf("failed to parse created issue: %w", err)
	}
	issue.ID = createdIssue.ID
	return nil
}

// GetIssue retrieves an issue by ID via daemon RPC.
func (e *DualModeTestEnv) GetIssue(id string) (*types.Issue, error) {
	args := &rpc.ShowArgs{ID: id}
	resp, err := e.client.Show(args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("show failed: %s", resp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		return nil, fmt.Errorf("failed to parse issue: %w", err)
	}
	return &issue, nil
}

// UpdateIssue updates an issue via daemon RPC.
func (e *DualModeTestEnv) UpdateIssue(id string, updates map[string]interface{}) error {
	args := &rpc.UpdateArgs{ID: id}

	if title, ok := updates["title"].(string); ok {
		args.Title = &title
	}
	if status, ok := updates["status"].(types.Status); ok {
		s := string(status)
		args.Status = &s
	}
	if statusStr, ok := updates["status"].(string); ok {
		args.Status = &statusStr
	}
	if priority, ok := updates["priority"].(int); ok {
		args.Priority = &priority
	}
	if desc, ok := updates["description"].(string); ok {
		args.Description = &desc
	}

	resp, err := e.client.Update(args)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("update failed: %s", resp.Error)
	}
	return nil
}

// ListIssues returns issues matching the filter via daemon RPC.
func (e *DualModeTestEnv) ListIssues(filter types.IssueFilter) ([]*types.Issue, error) {
	args := &rpc.ListArgs{}
	if filter.Status != nil {
		args.Status = string(*filter.Status)
	}
	if filter.Priority != nil {
		args.Priority = filter.Priority
	}
	if filter.IssueType != nil {
		args.IssueType = string(*filter.IssueType)
	}

	resp, err := e.client.List(args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("list failed: %s", resp.Error)
	}

	var issues []*types.Issue
	if err := json.Unmarshal(resp.Data, &issues); err != nil {
		return nil, fmt.Errorf("failed to parse issues: %w", err)
	}
	return issues, nil
}

// RunDaemonModeOnly runs the test function with a daemon-backed environment.
func RunDaemonModeOnly(t *testing.T, name string, testFn DualModeTestFunc) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping daemon test in short mode")
		}
		env := setupDaemonModeEnv(t)
		testFn(t, env)
	})
}

// setupDaemonModeEnv creates a test environment with a running daemon.
func setupDaemonModeEnv(t *testing.T) *DualModeTestEnv {
	t.Helper()

	tmpDir := makeSocketTempDir(t)
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	initTestGitRepo(t, tmpDir)

	dbPath := filepath.Join(beadsDir, "beads.db")
	socketPath := filepath.Join(beadsDir, "bd.sock")
	testStore := newTestStore(t, dbPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log := daemonLogger{logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))}

	server, _, err := startRPCServer(ctx, socketPath, testStore, tmpDir, dbPath, "", "", log)
	if err != nil {
		cancel()
		t.Fatalf("failed to start RPC server: %v", err)
	}

	select {
	case <-server.WaitReady():
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("server did not become ready within 5 seconds")
	}

	client, err := rpc.TryConnect(socketPath)
	if err != nil || client == nil {
		cancel()
		t.Fatalf("failed to connect RPC client: %v", err)
	}

	t.Cleanup(func() {
		if client != nil {
			client.Close()
		}
		if server != nil {
			_ = server.Stop()
		}
		cancel()
		os.RemoveAll(tmpDir)
	})

	return &DualModeTestEnv{
		t:          t,
		mode:       DaemonMode,
		tmpDir:     tmpDir,
		beadsDir:   beadsDir,
		dbPath:     dbPath,
		socketPath: socketPath,
		store:      testStore,
		client:     client,
		server:     server,
		ctx:        ctx,
		cancel:     cancel,
	}
}
