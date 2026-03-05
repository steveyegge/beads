package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/steveyegge/beads/internal/storage/dolt"
)

// deliverMessageToActiveBranches cherry-picks the latest commit from the current
// branch to all other active branches. Used for cross-branch message delivery.
//
// Messages are append-only inserts with hash-based IDs, so cherry-picks should
// apply cleanly in most cases. On failure for any branch, log and continue.
//
// Called after a message-type issue is created or updated on a non-main branch.
func deliverMessageToActiveBranches(ctx context.Context, s *dolt.DoltStore) {
	if s == nil || s.IsClosed() {
		return
	}

	currentBranch, err := s.CurrentBranch(ctx)
	if err != nil {
		return
	}

	// Get the commit hash to cherry-pick
	commitHash, err := s.GetCurrentCommit(ctx)
	if err != nil {
		log.Printf("message delivery: failed to get current commit: %v", err)
		return
	}

	// Get all active branches except current
	branches, err := s.ListRegisteredBranches(ctx, "active")
	if err != nil {
		log.Printf("message delivery: failed to list active branches: %v", err)
		return
	}

	// Also deliver to main (which isn't in the registry)
	targetBranches := []string{"main"}
	for _, b := range branches {
		if b.Name != currentBranch {
			targetBranches = append(targetBranches, b.Name)
		}
	}

	if len(targetBranches) == 0 {
		return
	}

	// Open a dedicated connection for cherry-pick delivery
	deliveryDB, err := openMergeConnection(s)
	if err != nil {
		log.Printf("message delivery: failed to open delivery connection: %v", err)
		return
	}
	defer deliveryDB.Close()

	for _, target := range targetBranches {
		if err := cherryPickToTarget(ctx, deliveryDB, s, target, commitHash); err != nil {
			log.Printf("message delivery: cherry-pick to %s failed: %v (skipping)", target, err)
			continue
		}
	}
}

// cherryPickToTarget cherry-picks a commit onto a target branch via a dedicated connection.
func cherryPickToTarget(ctx context.Context, db *sql.DB, s *dolt.DoltStore, targetBranch, commitHash string) error {
	// Switch to target branch
	if _, err := db.ExecContext(ctx, "CALL DOLT_CHECKOUT(?)", targetBranch); err != nil {
		return fmt.Errorf("checkout %s: %w", targetBranch, err)
	}

	// Cherry-pick the commit
	if _, err := db.ExecContext(ctx, "CALL DOLT_CHERRY_PICK(?)", commitHash); err != nil {
		// Check for conflicts — don't leave dirty state
		if strings.Contains(err.Error(), "conflict") {
			// Abort the cherry-pick to clean up
			_, _ = db.ExecContext(ctx, "CALL DOLT_MERGE('--abort')")
		}
		return fmt.Errorf("cherry-pick %s onto %s: %w", commitHash[:8], targetBranch, err)
	}

	return nil
}

// maybeSendMessage checks if the given issue is a message type and triggers
// cross-branch delivery if we're on a registered branch.
// Call this after successfully creating or updating a message-type issue.
func maybeSendMessage(ctx context.Context, s *dolt.DoltStore, issueType string) {
	if s == nil || s.IsClosed() {
		return
	}

	// Only deliver messages
	if issueType != "message" {
		return
	}

	branch, err := s.CurrentBranch(ctx)
	if err != nil || branch == "main" {
		return // on main, messages are already visible to everyone
	}

	// Only deliver if on a registered branch
	info, err := s.GetBranchInfo(ctx, branch)
	if err != nil || info == nil {
		return
	}

	deliverMessageToActiveBranches(ctx, s)
}
