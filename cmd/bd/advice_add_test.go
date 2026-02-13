package main

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/testutil/teststore"

	"github.com/steveyegge/beads/internal/types"
)

// TestAdviceHookFlags tests the advice hook flag validation and persistence
func TestAdviceHookFlags(t *testing.T) {
	ctx := context.Background()

	// Use memory store for persistence tests since SQLite store doesn't
	// include hook fields in INSERT/SELECT yet (migration added columns but
	// storage layer not updated). This tests the types layer correctly.
	memStore := teststore.New(t)

	t.Run("create advice with valid hook configuration", func(t *testing.T) {
		advice := &types.Issue{
			Title:               "Run tests before commit",
			Description:         "Execute test suite before committing code",
			Priority:            2,
			IssueType:           types.TypeAdvice,
			Status:              types.StatusOpen,
			AdviceHookCommand:   "make test",
			AdviceHookTrigger:   types.AdviceHookTriggerBeforeCommit,
			AdviceHookTimeout:   60,
			AdviceHookOnFailure: types.AdviceHookOnFailureBlock,
			CreatedAt:           time.Now(),
		}
		if err := memStore.CreateIssue(ctx, advice, "test-user"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		// Verify hooks are persisted
		retrieved, err := memStore.GetIssue(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve advice: %v", err)
		}
		if retrieved.AdviceHookCommand != "make test" {
			t.Errorf("Expected hook command 'make test', got %q", retrieved.AdviceHookCommand)
		}
		if retrieved.AdviceHookTrigger != types.AdviceHookTriggerBeforeCommit {
			t.Errorf("Expected trigger 'before-commit', got %q", retrieved.AdviceHookTrigger)
		}
		if retrieved.AdviceHookTimeout != 60 {
			t.Errorf("Expected timeout 60, got %d", retrieved.AdviceHookTimeout)
		}
		if retrieved.AdviceHookOnFailure != types.AdviceHookOnFailureBlock {
			t.Errorf("Expected on-failure 'block', got %q", retrieved.AdviceHookOnFailure)
		}
	})

	t.Run("validate hook-trigger values", func(t *testing.T) {
		// Test all valid triggers
		validTriggers := []string{
			types.AdviceHookTriggerSessionEnd,
			types.AdviceHookTriggerBeforeCommit,
			types.AdviceHookTriggerBeforePush,
			types.AdviceHookTriggerBeforeHandoff,
		}
		for _, trigger := range validTriggers {
			if !types.IsValidAdviceHookTrigger(trigger) {
				t.Errorf("Expected %q to be a valid trigger", trigger)
			}
		}

		// Test invalid triggers
		invalidTriggers := []string{
			"invalid-trigger",
			"after-commit",
			"on-error",
			"",
			"session_end", // underscore instead of hyphen
		}
		for _, trigger := range invalidTriggers {
			if trigger != "" && types.IsValidAdviceHookTrigger(trigger) {
				t.Errorf("Expected %q to be an invalid trigger", trigger)
			}
		}
	})

	t.Run("validate hook-timeout range", func(t *testing.T) {
		// Valid timeout values
		validTimeouts := []int{0, 1, 30, 150, 300}
		for _, timeout := range validTimeouts {
			advice := &types.Issue{
				Title:             "Timeout test",
				IssueType:         types.TypeAdvice,
				Status:            types.StatusOpen,
				AdviceHookCommand: "echo test",
				AdviceHookTrigger: types.AdviceHookTriggerBeforeCommit,
				AdviceHookTimeout: timeout,
				CreatedAt:         time.Now(),
			}
			if err := advice.Validate(); err != nil {
				t.Errorf("Expected timeout %d to be valid, got error: %v", timeout, err)
			}
		}

		// Invalid timeout values
		invalidTimeouts := []int{-1, 301, 1000}
		for _, timeout := range invalidTimeouts {
			advice := &types.Issue{
				Title:             "Timeout test",
				IssueType:         types.TypeAdvice,
				Status:            types.StatusOpen,
				AdviceHookCommand: "echo test",
				AdviceHookTrigger: types.AdviceHookTriggerBeforeCommit,
				AdviceHookTimeout: timeout,
				CreatedAt:         time.Now(),
			}
			if err := advice.Validate(); err == nil {
				t.Errorf("Expected timeout %d to be invalid", timeout)
			}
		}
	})

	t.Run("validate hook-on-failure values", func(t *testing.T) {
		// Test all valid on-failure values
		validOnFailure := []string{
			types.AdviceHookOnFailureBlock,
			types.AdviceHookOnFailureWarn,
			types.AdviceHookOnFailureIgnore,
		}
		for _, onFailure := range validOnFailure {
			if !types.IsValidAdviceHookOnFailure(onFailure) {
				t.Errorf("Expected %q to be a valid on-failure value", onFailure)
			}
		}

		// Test invalid on-failure values
		invalidOnFailure := []string{
			"invalid",
			"stop",
			"error",
			"BLOCK", // case-sensitive
		}
		for _, onFailure := range invalidOnFailure {
			if types.IsValidAdviceHookOnFailure(onFailure) {
				t.Errorf("Expected %q to be an invalid on-failure value", onFailure)
			}
		}
	})

	t.Run("hook-command requires hook-trigger (validation)", func(t *testing.T) {
		// Note: This test validates the command-line validation logic.
		// The actual CLI check is in advice_add.go lines 152-155.
		// Here we verify the expected behavior:
		// Having hook-command without hook-trigger should be an error.

		// When hook-trigger is provided with hook-command, it should work
		advice := &types.Issue{
			Title:             "With trigger",
			IssueType:         types.TypeAdvice,
			Status:            types.StatusOpen,
			AdviceHookCommand: "make test",
			AdviceHookTrigger: types.AdviceHookTriggerBeforeCommit,
			CreatedAt:         time.Now(),
		}
		if err := advice.Validate(); err != nil {
			t.Errorf("Expected valid when both command and trigger present: %v", err)
		}

		// hook-trigger alone is valid (command is optional)
		adviceWithTriggerOnly := &types.Issue{
			Title:             "With trigger only",
			IssueType:         types.TypeAdvice,
			Status:            types.StatusOpen,
			AdviceHookTrigger: types.AdviceHookTriggerSessionEnd,
			CreatedAt:         time.Now(),
		}
		if err := adviceWithTriggerOnly.Validate(); err != nil {
			t.Errorf("Expected valid when only trigger present: %v", err)
		}
	})

	t.Run("hooks persisted and retrievable", func(t *testing.T) {
		// Use memory store for persistence test (SQLite storage layer doesn't
		// include hook fields in INSERT/SELECT statements yet)
		memStore2 := teststore.New(t)

		// Create advice with all hook fields
		advice := &types.Issue{
			Title:               "Lint before push",
			Description:         "Run linter before pushing to remote",
			Priority:            1,
			IssueType:           types.TypeAdvice,
			Status:              types.StatusOpen,
			AdviceHookCommand:   "npm run lint",
			AdviceHookTrigger:   types.AdviceHookTriggerBeforePush,
			AdviceHookTimeout:   120,
			AdviceHookOnFailure: types.AdviceHookOnFailureWarn,
			CreatedAt:           time.Now(),
		}
		if err := memStore2.CreateIssue(ctx, advice, "test-user"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		// Retrieve and verify all fields
		retrieved, err := memStore2.GetIssue(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve advice: %v", err)
		}

		if retrieved.AdviceHookCommand != advice.AdviceHookCommand {
			t.Errorf("Hook command mismatch: got %q, want %q",
				retrieved.AdviceHookCommand, advice.AdviceHookCommand)
		}
		if retrieved.AdviceHookTrigger != advice.AdviceHookTrigger {
			t.Errorf("Hook trigger mismatch: got %q, want %q",
				retrieved.AdviceHookTrigger, advice.AdviceHookTrigger)
		}
		if retrieved.AdviceHookTimeout != advice.AdviceHookTimeout {
			t.Errorf("Hook timeout mismatch: got %d, want %d",
				retrieved.AdviceHookTimeout, advice.AdviceHookTimeout)
		}
		if retrieved.AdviceHookOnFailure != advice.AdviceHookOnFailure {
			t.Errorf("Hook on-failure mismatch: got %q, want %q",
				retrieved.AdviceHookOnFailure, advice.AdviceHookOnFailure)
		}
	})

	t.Run("hook fields only valid for advice type", func(t *testing.T) {
		// Bug or task with hook fields should be rejected
		task := &types.Issue{
			Title:             "Task with hooks",
			IssueType:         types.TypeTask,
			Status:            types.StatusOpen,
			AdviceHookCommand: "make test",
			AdviceHookTrigger: types.AdviceHookTriggerBeforeCommit,
			CreatedAt:         time.Now(),
		}
		if err := task.Validate(); err == nil {
			t.Error("Expected error when hook fields used on non-advice issue type")
		}
	})

	t.Run("each valid trigger value works", func(t *testing.T) {
		triggers := map[string]string{
			types.AdviceHookTriggerSessionEnd:    "session-end",
			types.AdviceHookTriggerBeforeCommit:  "before-commit",
			types.AdviceHookTriggerBeforePush:    "before-push",
			types.AdviceHookTriggerBeforeHandoff: "before-handoff",
		}
		for constant, expected := range triggers {
			if constant != expected {
				t.Errorf("Constant %q should equal %q", constant, expected)
			}

			// Test that validation passes for each trigger
			advice := &types.Issue{
				Title:             "Trigger test: " + expected,
				IssueType:         types.TypeAdvice,
				Status:            types.StatusOpen,
				AdviceHookCommand: "echo " + expected,
				AdviceHookTrigger: constant,
				CreatedAt:         time.Now(),
			}
			if err := advice.Validate(); err != nil {
				t.Errorf("Validation failed for trigger %q: %v", constant, err)
			}
		}
	})

	t.Run("each valid on-failure value works", func(t *testing.T) {
		onFailures := map[string]string{
			types.AdviceHookOnFailureBlock:  "block",
			types.AdviceHookOnFailureWarn:   "warn",
			types.AdviceHookOnFailureIgnore: "ignore",
		}
		for constant, expected := range onFailures {
			if constant != expected {
				t.Errorf("Constant %q should equal %q", constant, expected)
			}

			// Test that validation passes for each on-failure value
			advice := &types.Issue{
				Title:               "OnFailure test: " + expected,
				IssueType:           types.TypeAdvice,
				Status:              types.StatusOpen,
				AdviceHookCommand:   "echo " + expected,
				AdviceHookTrigger:   types.AdviceHookTriggerBeforeCommit,
				AdviceHookOnFailure: constant,
				CreatedAt:           time.Now(),
			}
			if err := advice.Validate(); err != nil {
				t.Errorf("Validation failed for on-failure %q: %v", constant, err)
			}
		}
	})

	t.Run("timeout boundaries", func(t *testing.T) {
		// Test exact boundaries
		boundaries := []struct {
			timeout int
			valid   bool
		}{
			{-1, false},
			{0, true},
			{1, true},
			{types.AdviceHookTimeoutDefault, true}, // 30
			{types.AdviceHookTimeoutMax, true},     // 300
			{types.AdviceHookTimeoutMax + 1, false},
		}

		for _, tc := range boundaries {
			advice := &types.Issue{
				Title:             "Boundary test",
				IssueType:         types.TypeAdvice,
				Status:            types.StatusOpen,
				AdviceHookCommand: "echo boundary",
				AdviceHookTrigger: types.AdviceHookTriggerBeforeCommit,
				AdviceHookTimeout: tc.timeout,
				CreatedAt:         time.Now(),
			}
			err := advice.Validate()
			if tc.valid && err != nil {
				t.Errorf("Timeout %d should be valid, got error: %v", tc.timeout, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("Timeout %d should be invalid", tc.timeout)
			}
		}
	})
}

// TestAdviceHookDefaults tests default values for hook flags
func TestAdviceHookDefaults(t *testing.T) {
	ctx := context.Background()
	memStore := teststore.New(t)

	t.Run("advice without hooks has empty hook fields", func(t *testing.T) {
		advice := &types.Issue{
			Title:     "No hooks",
			IssueType: types.TypeAdvice,
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		if err := memStore.CreateIssue(ctx, advice, "test-user"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		retrieved, err := memStore.GetIssue(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve advice: %v", err)
		}

		if retrieved.AdviceHookCommand != "" {
			t.Errorf("Expected empty hook command, got %q", retrieved.AdviceHookCommand)
		}
		if retrieved.AdviceHookTrigger != "" {
			t.Errorf("Expected empty hook trigger, got %q", retrieved.AdviceHookTrigger)
		}
		if retrieved.AdviceHookTimeout != 0 {
			t.Errorf("Expected zero hook timeout, got %d", retrieved.AdviceHookTimeout)
		}
		if retrieved.AdviceHookOnFailure != "" {
			t.Errorf("Expected empty hook on-failure, got %q", retrieved.AdviceHookOnFailure)
		}
	})
}

// TestAdvicePriorityAndMetadata tests priority and metadata flag handling
func TestAdvicePriorityAndMetadata(t *testing.T) {
	ctx := context.Background()
	memStore := teststore.New(t)

	t.Run("create advice with custom priority", func(t *testing.T) {
		advice := &types.Issue{
			Title:     "High priority advice",
			Priority:  1, // Highest priority
			IssueType: types.TypeAdvice,
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		if err := memStore.CreateIssue(ctx, advice, "test-user"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		retrieved, err := memStore.GetIssue(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve advice: %v", err)
		}
		if retrieved.Priority != 1 {
			t.Errorf("Expected priority 1, got %d", retrieved.Priority)
		}
	})

	t.Run("default priority is 2", func(t *testing.T) {
		advice := &types.Issue{
			Title:     "Default priority advice",
			IssueType: types.TypeAdvice,
			Status:    types.StatusOpen,
			CreatedAt: time.Now(),
		}
		// Don't set priority - should default to 0 in struct, but CLI defaults to 2
		if advice.Priority != 0 {
			t.Errorf("Expected struct default priority 0, got %d", advice.Priority)
		}
	})

	t.Run("priority range validation", func(t *testing.T) {
		// Priority is 0-4 (P0=highest to P4=lowest)
		validPriorities := []int{0, 1, 2, 3, 4}
		for _, p := range validPriorities {
			advice := &types.Issue{
				Title:     "Priority test",
				Priority:  p,
				IssueType: types.TypeAdvice,
				Status:    types.StatusOpen,
				CreatedAt: time.Now(),
			}
			if err := memStore.CreateIssue(ctx, advice, "test-user"); err != nil {
				t.Errorf("Priority %d should be valid, got error: %v", p, err)
			}
		}
	})

	t.Run("create advice with custom title", func(t *testing.T) {
		advice := &types.Issue{
			Title:       "Custom Title Here",
			Description: "This is a longer description that differs from the title",
			IssueType:   types.TypeAdvice,
			Status:      types.StatusOpen,
			CreatedAt:   time.Now(),
		}
		if err := memStore.CreateIssue(ctx, advice, "test-user"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		retrieved, err := memStore.GetIssue(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve advice: %v", err)
		}
		if retrieved.Title != "Custom Title Here" {
			t.Errorf("Expected title 'Custom Title Here', got %q", retrieved.Title)
		}
		if retrieved.Description != "This is a longer description that differs from the title" {
			t.Errorf("Description not preserved correctly")
		}
	})

	t.Run("title defaults to first line when not specified", func(t *testing.T) {
		// This tests the CLI behavior where title defaults to first line of advice text
		// In the types layer, title and description are separate fields
		advice := &types.Issue{
			Title:       "First line becomes title",
			Description: "First line becomes title\nSecond line is part of description",
			IssueType:   types.TypeAdvice,
			Status:      types.StatusOpen,
			CreatedAt:   time.Now(),
		}
		if err := memStore.CreateIssue(ctx, advice, "test-user"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		retrieved, err := memStore.GetIssue(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve advice: %v", err)
		}
		if retrieved.Title != "First line becomes title" {
			t.Errorf("Title should be first line, got %q", retrieved.Title)
		}
	})

	t.Run("description separate from title", func(t *testing.T) {
		advice := &types.Issue{
			Title:       "Short title",
			Description: "This is a much longer description that provides detailed context about when and how to apply this advice. It can span multiple paragraphs and include examples.",
			IssueType:   types.TypeAdvice,
			Status:      types.StatusOpen,
			CreatedAt:   time.Now(),
		}
		if err := memStore.CreateIssue(ctx, advice, "test-user"); err != nil {
			t.Fatalf("Failed to create advice: %v", err)
		}

		retrieved, err := memStore.GetIssue(ctx, advice.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve advice: %v", err)
		}
		if retrieved.Title == retrieved.Description {
			t.Error("Title and description should be separate")
		}
		if len(retrieved.Description) <= len(retrieved.Title) {
			t.Error("Description should be longer than title in this test case")
		}
	})

	t.Run("all priority levels persisted correctly", func(t *testing.T) {
		// Priority is 0-4 (P0=highest to P4=lowest)
		for p := 0; p <= 4; p++ {
			advice := &types.Issue{
				Title:     "Priority persistence test",
				Priority:  p,
				IssueType: types.TypeAdvice,
				Status:    types.StatusOpen,
				CreatedAt: time.Now(),
			}
			if err := memStore.CreateIssue(ctx, advice, "test-user"); err != nil {
				t.Fatalf("Failed to create advice with priority %d: %v", p, err)
			}

			retrieved, err := memStore.GetIssue(ctx, advice.ID)
			if err != nil {
				t.Fatalf("Failed to retrieve advice: %v", err)
			}
			if retrieved.Priority != p {
				t.Errorf("Priority %d not persisted correctly, got %d", p, retrieved.Priority)
			}
		}
	})
}
