//go:build cgo

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

func TestWithStorage_ReopensUsingMetadata(t *testing.T) {
	ctx := context.Background()
	testDBPath := filepath.Join(t.TempDir(), "dolt")
	newTestStoreIsolatedDB(t, testDBPath, "cfg")

	var gotPrefix string
	err := withStorage(ctx, nil, testDBPath, func(s *dolt.DoltStore) error {
		var err error
		gotPrefix, err = s.GetConfig(ctx, "issue_prefix")
		return err
	})
	if err != nil {
		t.Fatalf("withStorage() error = %v", err)
	}
	if gotPrefix != "cfg" {
		t.Fatalf("issue_prefix = %q, want %q", gotPrefix, "cfg")
	}
}

func TestIssueIDCompletion_UsesMetadataWhenStoreNil(t *testing.T) {
	originalStore := store
	originalDBPath := dbPath
	originalRootCtx := rootCtx
	defer func() {
		store = originalStore
		dbPath = originalDBPath
		rootCtx = originalRootCtx
	}()

	ctx := context.Background()
	rootCtx = ctx

	testDBPath := filepath.Join(t.TempDir(), "dolt")
	testStore := newTestStoreIsolatedDB(t, testDBPath, "cfg")
	if err := testStore.CreateIssue(ctx, &types.Issue{
		ID:        "cfg-abc1",
		Title:     "Completion target",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}, "test"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	store = nil
	dbPath = testDBPath

	completions, directive := issueIDCompletion(&cobra.Command{}, nil, "cfg-a")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %d, want %d", directive, cobra.ShellCompDirectiveNoFileComp)
	}
	if len(completions) != 1 {
		t.Fatalf("len(completions) = %d, want 1 (%v)", len(completions), completions)
	}
	if len(completions[0]) < len("cfg-abc1") || completions[0][:len("cfg-abc1")] != "cfg-abc1" {
		t.Fatalf("completion = %q, want prefix %q", completions[0], "cfg-abc1")
	}
}

func TestGetGitHubConfigValue_UsesMetadataWhenStoreNil(t *testing.T) {
	originalStore := store
	originalDBPath := dbPath
	defer func() {
		store = originalStore
		dbPath = originalDBPath
	}()

	ctx := context.Background()
	testDBPath := filepath.Join(t.TempDir(), "dolt")
	testStore := newTestStoreIsolatedDB(t, testDBPath, "cfg")
	if err := testStore.SetConfig(ctx, "github.token", "ghp_test_token"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	store = nil
	dbPath = testDBPath

	if got := getGitHubConfigValue(ctx, "github.token"); got != "ghp_test_token" {
		t.Fatalf("getGitHubConfigValue() = %q, want %q", got, "ghp_test_token")
	}
}
