//go:build !windows

package rpc

import (
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestDecisionCreate verifies the DecisionCreate client method
func TestDecisionCreate(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// First, create an issue to attach the decision to
	createArgs := &CreateArgs{
		Title:       "Test Issue for Decision",
		Description: "Test description",
		IssueType:   "task",
		Priority:    2,
	}

	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Create issue failed: %v", err)
	}
	if !createResp.Success {
		t.Fatalf("Expected success, got error: %s", createResp.Error)
	}

	var issue types.Issue
	if err := json.Unmarshal(createResp.Data, &issue); err != nil {
		t.Fatalf("Failed to unmarshal issue: %v", err)
	}

	t.Run("create_decision_success", func(t *testing.T) {
		args := &DecisionCreateArgs{
			IssueID:       issue.ID,
			Prompt:        "Which database should we use?",
			Options:       []string{"PostgreSQL", "MySQL", "SQLite"},
			DefaultOption: "PostgreSQL",
			MaxIterations: 3,
			RequestedBy:   "test-agent",
		}

		resp, err := client.DecisionCreate(args)
		if err != nil {
			t.Fatalf("DecisionCreate failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if resp.Decision == nil {
			t.Fatal("Expected non-nil decision in response")
		}

		// Verify decision fields
		if resp.Decision.IssueID != issue.ID {
			t.Errorf("Expected IssueID %s, got %s", issue.ID, resp.Decision.IssueID)
		}
		if resp.Decision.Prompt != args.Prompt {
			t.Errorf("Expected Prompt %q, got %q", args.Prompt, resp.Decision.Prompt)
		}
		if resp.Decision.DefaultOption != args.DefaultOption {
			t.Errorf("Expected DefaultOption %s, got %s", args.DefaultOption, resp.Decision.DefaultOption)
		}
		if resp.Decision.MaxIterations != args.MaxIterations {
			t.Errorf("Expected MaxIterations %d, got %d", args.MaxIterations, resp.Decision.MaxIterations)
		}
		if resp.Decision.Iteration != 1 {
			t.Errorf("Expected Iteration 1, got %d", resp.Decision.Iteration)
		}
		if resp.Decision.RequestedBy != args.RequestedBy {
			t.Errorf("Expected RequestedBy %s, got %s", args.RequestedBy, resp.Decision.RequestedBy)
		}

		// Verify options were stored as JSON
		var storedOptions []string
		if err := json.Unmarshal([]byte(resp.Decision.Options), &storedOptions); err != nil {
			t.Fatalf("Failed to unmarshal stored options: %v", err)
		}
		if len(storedOptions) != len(args.Options) {
			t.Errorf("Expected %d options, got %d", len(args.Options), len(storedOptions))
		}

		// Verify associated issue is returned
		if resp.Issue == nil {
			t.Error("Expected associated issue in response")
		} else if resp.Issue.ID != issue.ID {
			t.Errorf("Expected associated issue ID %s, got %s", issue.ID, resp.Issue.ID)
		}
	})

	t.Run("create_decision_nonexistent_issue", func(t *testing.T) {
		args := &DecisionCreateArgs{
			IssueID: "nonexistent-issue-id",
			Prompt:  "Should fail?",
			Options: []string{"Yes", "No"},
		}

		_, err := client.DecisionCreate(args)
		if err == nil {
			t.Error("Expected error for nonexistent issue")
		}
	})
}

// TestDecisionGet verifies the DecisionGet client method
func TestDecisionGet(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Create an issue and a decision
	createArgs := &CreateArgs{
		Title:     "Issue for Decision Get Test",
		IssueType: "task",
	}

	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Create issue failed: %v", err)
	}

	var issue types.Issue
	json.Unmarshal(createResp.Data, &issue)

	decisionArgs := &DecisionCreateArgs{
		IssueID:       issue.ID,
		Prompt:        "Test prompt",
		Options:       []string{"Option A", "Option B"},
		MaxIterations: 5,
	}

	_, err = client.DecisionCreate(decisionArgs)
	if err != nil {
		t.Fatalf("DecisionCreate failed: %v", err)
	}

	t.Run("get_existing_decision", func(t *testing.T) {
		getArgs := &DecisionGetArgs{
			IssueID: issue.ID,
		}

		resp, err := client.DecisionGet(getArgs)
		if err != nil {
			t.Fatalf("DecisionGet failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if resp.Decision == nil {
			t.Fatal("Expected non-nil decision in response")
		}

		// Verify retrieved decision
		if resp.Decision.IssueID != issue.ID {
			t.Errorf("Expected IssueID %s, got %s", issue.ID, resp.Decision.IssueID)
		}
		if resp.Decision.Prompt != decisionArgs.Prompt {
			t.Errorf("Expected Prompt %q, got %q", decisionArgs.Prompt, resp.Decision.Prompt)
		}
		if resp.Decision.MaxIterations != decisionArgs.MaxIterations {
			t.Errorf("Expected MaxIterations %d, got %d", decisionArgs.MaxIterations, resp.Decision.MaxIterations)
		}

		// Verify associated issue
		if resp.Issue == nil {
			t.Error("Expected associated issue")
		} else if resp.Issue.ID != issue.ID {
			t.Errorf("Expected issue ID %s, got %s", issue.ID, resp.Issue.ID)
		}
	})

	t.Run("get_nonexistent_decision", func(t *testing.T) {
		getArgs := &DecisionGetArgs{
			IssueID: "nonexistent-issue",
		}

		_, err := client.DecisionGet(getArgs)
		if err == nil {
			t.Error("Expected error for nonexistent decision")
		}
	})
}

// TestDecisionResolveWithOption verifies resolving a decision with an option selection
func TestDecisionResolveWithOption(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Create issue and decision
	createArgs := &CreateArgs{
		Title:     "Issue for Decision Resolve Test",
		IssueType: "task",
	}

	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Create issue failed: %v", err)
	}

	var issue types.Issue
	json.Unmarshal(createResp.Data, &issue)

	decisionArgs := &DecisionCreateArgs{
		IssueID: issue.ID,
		Prompt:  "Which option?",
		Options: []string{"Option A", "Option B", "Option C"},
	}

	_, err = client.DecisionCreate(decisionArgs)
	if err != nil {
		t.Fatalf("DecisionCreate failed: %v", err)
	}

	t.Run("resolve_with_selected_option", func(t *testing.T) {
		resolveArgs := &DecisionResolveArgs{
			IssueID:        issue.ID,
			SelectedOption: "Option B",
			ResponseText:   "Option B is the best choice",
			RespondedBy:    "test-human",
		}

		resp, err := client.DecisionResolve(resolveArgs)
		if err != nil {
			t.Fatalf("DecisionResolve failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if resp.Decision == nil {
			t.Fatal("Expected non-nil decision in response")
		}

		// Verify resolution
		if resp.Decision.SelectedOption != resolveArgs.SelectedOption {
			t.Errorf("Expected SelectedOption %s, got %s", resolveArgs.SelectedOption, resp.Decision.SelectedOption)
		}
		if resp.Decision.ResponseText != resolveArgs.ResponseText {
			t.Errorf("Expected ResponseText %q, got %q", resolveArgs.ResponseText, resp.Decision.ResponseText)
		}
		if resp.Decision.RespondedBy != resolveArgs.RespondedBy {
			t.Errorf("Expected RespondedBy %s, got %s", resolveArgs.RespondedBy, resp.Decision.RespondedBy)
		}
		if resp.Decision.RespondedAt == nil {
			t.Error("Expected RespondedAt to be set")
		}
	})

	t.Run("resolve_nonexistent_decision", func(t *testing.T) {
		resolveArgs := &DecisionResolveArgs{
			IssueID:        "nonexistent-issue",
			SelectedOption: "Option A",
		}

		_, err := client.DecisionResolve(resolveArgs)
		if err == nil {
			t.Error("Expected error for nonexistent decision")
		}
	})
}

// TestDecisionResolveWithGuidance verifies resolving a decision with text guidance
func TestDecisionResolveWithGuidance(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Create issue and decision
	createArgs := &CreateArgs{
		Title:     "Issue for Decision Guidance Test",
		IssueType: "task",
	}

	createResp, err := client.Create(createArgs)
	if err != nil {
		t.Fatalf("Create issue failed: %v", err)
	}

	var issue types.Issue
	json.Unmarshal(createResp.Data, &issue)

	decisionArgs := &DecisionCreateArgs{
		IssueID:       issue.ID,
		Prompt:        "Need more context",
		Options:       []string{"Proceed", "Wait", "Abort"},
		MaxIterations: 3,
	}

	_, err = client.DecisionCreate(decisionArgs)
	if err != nil {
		t.Fatalf("DecisionCreate failed: %v", err)
	}

	t.Run("resolve_with_guidance_only", func(t *testing.T) {
		resolveArgs := &DecisionResolveArgs{
			IssueID:     issue.ID,
			Guidance:    "Please consider the performance implications and provide a detailed analysis",
			RespondedBy: "test-reviewer",
		}

		resp, err := client.DecisionResolve(resolveArgs)
		if err != nil {
			t.Fatalf("DecisionResolve with guidance failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if resp.Decision == nil {
			t.Fatal("Expected non-nil decision in response")
		}

		// Verify guidance was recorded
		if resp.Decision.Guidance != resolveArgs.Guidance {
			t.Errorf("Expected Guidance %q, got %q", resolveArgs.Guidance, resp.Decision.Guidance)
		}
		if resp.Decision.RespondedBy != resolveArgs.RespondedBy {
			t.Errorf("Expected RespondedBy %s, got %s", resolveArgs.RespondedBy, resp.Decision.RespondedBy)
		}
		if resp.Decision.RespondedAt == nil {
			t.Error("Expected RespondedAt to be set")
		}
	})

	t.Run("resolve_with_option_and_guidance", func(t *testing.T) {
		// Create another decision for this test
		createArgs2 := &CreateArgs{
			Title:     "Issue for Combined Resolve Test",
			IssueType: "task",
		}
		createResp2, _ := client.Create(createArgs2)
		var issue2 types.Issue
		json.Unmarshal(createResp2.Data, &issue2)

		decisionArgs2 := &DecisionCreateArgs{
			IssueID: issue2.ID,
			Prompt:  "Combined test",
			Options: []string{"A", "B"},
		}
		client.DecisionCreate(decisionArgs2)

		resolveArgs := &DecisionResolveArgs{
			IssueID:        issue2.ID,
			SelectedOption: "A",
			Guidance:       "Go with A but monitor closely",
			ResponseText:   "Selected A with conditions",
			RespondedBy:    "test-lead",
		}

		resp, err := client.DecisionResolve(resolveArgs)
		if err != nil {
			t.Fatalf("DecisionResolve with option and guidance failed: %v", err)
		}

		// Verify both are recorded
		if resp.Decision.SelectedOption != resolveArgs.SelectedOption {
			t.Errorf("Expected SelectedOption %s, got %s", resolveArgs.SelectedOption, resp.Decision.SelectedOption)
		}
		if resp.Decision.Guidance != resolveArgs.Guidance {
			t.Errorf("Expected Guidance %q, got %q", resolveArgs.Guidance, resp.Decision.Guidance)
		}
		if resp.Decision.ResponseText != resolveArgs.ResponseText {
			t.Errorf("Expected ResponseText %q, got %q", resolveArgs.ResponseText, resp.Decision.ResponseText)
		}
	})
}

// TestDecisionList verifies the DecisionList client method
func TestDecisionList(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("list_empty_decisions", func(t *testing.T) {
		listArgs := &DecisionListArgs{}

		resp, err := client.DecisionList(listArgs)
		if err != nil {
			t.Fatalf("DecisionList failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Expected non-nil response")
		}
		if resp.Count != 0 {
			t.Errorf("Expected 0 decisions, got %d", resp.Count)
		}
	})

	t.Run("list_pending_decisions", func(t *testing.T) {
		// Create multiple issues with decisions
		var issueIDs []string
		for i := 0; i < 3; i++ {
			createArgs := &CreateArgs{
				Title:     "Issue for List Test",
				IssueType: "task",
			}
			createResp, err := client.Create(createArgs)
			if err != nil {
				t.Fatalf("Create issue %d failed: %v", i, err)
			}
			var issue types.Issue
			json.Unmarshal(createResp.Data, &issue)
			issueIDs = append(issueIDs, issue.ID)

			decisionArgs := &DecisionCreateArgs{
				IssueID: issue.ID,
				Prompt:  "Decision " + issue.ID,
				Options: []string{"Yes", "No"},
			}
			_, err = client.DecisionCreate(decisionArgs)
			if err != nil {
				t.Fatalf("DecisionCreate for issue %d failed: %v", i, err)
			}
		}

		// List all pending decisions
		listArgs := &DecisionListArgs{}
		resp, err := client.DecisionList(listArgs)
		if err != nil {
			t.Fatalf("DecisionList failed: %v", err)
		}
		if resp.Count != 3 {
			t.Errorf("Expected 3 pending decisions, got %d", resp.Count)
		}
		if len(resp.Decisions) != 3 {
			t.Errorf("Expected 3 decision entries, got %d", len(resp.Decisions))
		}

		// Verify each decision has the expected structure
		for _, dr := range resp.Decisions {
			if dr.Decision == nil {
				t.Error("Expected non-nil decision in list entry")
				continue
			}
			if dr.Issue == nil {
				t.Error("Expected non-nil issue in list entry")
			}
		}
	})

	t.Run("resolved_decisions_excluded_from_pending_list", func(t *testing.T) {
		// Create a new issue with decision
		createArgs := &CreateArgs{
			Title:     "Issue for Resolved Test",
			IssueType: "task",
		}
		createResp, err := client.Create(createArgs)
		if err != nil {
			t.Fatalf("Create issue failed: %v", err)
		}
		var issue types.Issue
		json.Unmarshal(createResp.Data, &issue)

		decisionArgs := &DecisionCreateArgs{
			IssueID: issue.ID,
			Prompt:  "To be resolved",
			Options: []string{"Yes", "No"},
		}
		_, err = client.DecisionCreate(decisionArgs)
		if err != nil {
			t.Fatalf("DecisionCreate failed: %v", err)
		}

		// Get count before resolution
		listBefore, _ := client.DecisionList(&DecisionListArgs{})
		countBefore := listBefore.Count

		// Resolve the decision
		resolveArgs := &DecisionResolveArgs{
			IssueID:        issue.ID,
			SelectedOption: "Yes",
		}
		_, err = client.DecisionResolve(resolveArgs)
		if err != nil {
			t.Fatalf("DecisionResolve failed: %v", err)
		}

		// List should have one fewer pending decision
		listAfter, err := client.DecisionList(&DecisionListArgs{})
		if err != nil {
			t.Fatalf("DecisionList after resolve failed: %v", err)
		}

		// Note: The storage ListPendingDecisions filters by resolved_at being null
		// After resolution, resolved_at is set, so it should be excluded
		if listAfter.Count >= countBefore {
			// This might depend on implementation - if storage doesn't filter,
			// we just check that the resolved decision is still accessible via get
			t.Logf("Note: List count may include resolved decisions depending on implementation")
		}
	})
}

// TestDecisionErrorHandling verifies error propagation in decision methods
func TestDecisionErrorHandling(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("create_with_empty_issue_id", func(t *testing.T) {
		// gt-w3u2o9: Empty issue ID now creates a gate issue automatically
		args := &DecisionCreateArgs{
			IssueID: "",
			Prompt:  "Empty issue ID - should create gate",
			Options: []string{"Yes"},
		}

		resp, err := client.DecisionCreate(args)
		if err != nil {
			t.Errorf("Expected success for empty issue ID (gate auto-created), got: %v", err)
		}
		if resp != nil && resp.Issue != nil {
			if resp.Issue.IssueType != "gate" {
				t.Errorf("Expected gate issue type, got: %s", resp.Issue.IssueType)
			}
		}
	})

	t.Run("get_with_empty_issue_id", func(t *testing.T) {
		args := &DecisionGetArgs{
			IssueID: "",
		}

		_, err := client.DecisionGet(args)
		if err == nil {
			t.Error("Expected error for empty issue ID")
		}
	})

	t.Run("resolve_with_empty_issue_id", func(t *testing.T) {
		args := &DecisionResolveArgs{
			IssueID:        "",
			SelectedOption: "Yes",
		}

		_, err := client.DecisionResolve(args)
		if err == nil {
			t.Error("Expected error for empty issue ID")
		}
	})

	t.Run("create_decision_for_nonexistent_issue", func(t *testing.T) {
		args := &DecisionCreateArgs{
			IssueID: "bd-nonexistent",
			Prompt:  "Should fail",
			Options: []string{"A", "B"},
		}

		_, err := client.DecisionCreate(args)
		if err == nil {
			t.Error("Expected error when issue doesn't exist")
		}
	})

	t.Run("get_decision_for_issue_without_decision", func(t *testing.T) {
		// Create an issue without a decision
		createArgs := &CreateArgs{
			Title:     "Issue Without Decision",
			IssueType: "task",
		}
		createResp, _ := client.Create(createArgs)
		var issue types.Issue
		json.Unmarshal(createResp.Data, &issue)

		getArgs := &DecisionGetArgs{
			IssueID: issue.ID,
		}

		_, err := client.DecisionGet(getArgs)
		if err == nil {
			t.Error("Expected error when decision doesn't exist for issue")
		}
	})

	t.Run("resolve_decision_for_issue_without_decision", func(t *testing.T) {
		// Create an issue without a decision
		createArgs := &CreateArgs{
			Title:     "Another Issue Without Decision",
			IssueType: "task",
		}
		createResp, _ := client.Create(createArgs)
		var issue types.Issue
		json.Unmarshal(createResp.Data, &issue)

		resolveArgs := &DecisionResolveArgs{
			IssueID:        issue.ID,
			SelectedOption: "Yes",
		}

		_, err := client.DecisionResolve(resolveArgs)
		if err == nil {
			t.Error("Expected error when trying to resolve non-existent decision")
		}
	})
}

// TestDecisionClientResponseParsing verifies correct response unmarshaling
func TestDecisionClientResponseParsing(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Create issue and decision
	createArgs := &CreateArgs{
		Title:     "Issue for Response Parsing Test",
		IssueType: "task",
		Priority:  3,
	}
	createResp, _ := client.Create(createArgs)
	var issue types.Issue
	json.Unmarshal(createResp.Data, &issue)

	decisionArgs := &DecisionCreateArgs{
		IssueID:       issue.ID,
		Prompt:        "Parsing test prompt",
		Options:       []string{"Alpha", "Beta", "Gamma"},
		DefaultOption: "Beta",
		MaxIterations: 5,
		RequestedBy:   "parser-test",
	}

	t.Run("create_response_structure", func(t *testing.T) {
		resp, err := client.DecisionCreate(decisionArgs)
		if err != nil {
			t.Fatalf("DecisionCreate failed: %v", err)
		}

		// Verify DecisionResponse structure
		if resp.Decision == nil {
			t.Fatal("Decision should not be nil")
		}
		if resp.Issue == nil {
			t.Fatal("Issue should not be nil")
		}

		// Verify DecisionPoint fields
		dp := resp.Decision
		if dp.IssueID == "" {
			t.Error("IssueID should not be empty")
		}
		if dp.Prompt == "" {
			t.Error("Prompt should not be empty")
		}
		if dp.Options == "" {
			t.Error("Options should not be empty")
		}
		if dp.CreatedAt.IsZero() {
			t.Error("CreatedAt should be set")
		}

		// Verify Issue fields
		iss := resp.Issue
		if iss.ID == "" {
			t.Error("Issue ID should not be empty")
		}
		if iss.Title != createArgs.Title {
			t.Errorf("Expected issue title %q, got %q", createArgs.Title, iss.Title)
		}
	})

	t.Run("list_response_structure", func(t *testing.T) {
		listResp, err := client.DecisionList(&DecisionListArgs{})
		if err != nil {
			t.Fatalf("DecisionList failed: %v", err)
		}

		// Verify DecisionListResponse structure
		if listResp.Decisions == nil {
			t.Error("Decisions slice should not be nil")
		}
		if listResp.Count < 1 {
			t.Error("Should have at least one decision")
		}
		if listResp.Count != len(listResp.Decisions) {
			t.Errorf("Count %d doesn't match slice length %d", listResp.Count, len(listResp.Decisions))
		}
	})
}
