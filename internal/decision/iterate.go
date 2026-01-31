// Package decision provides decision point iteration logic.
// When a user provides text guidance without selecting an option,
// the iteration system creates a new decision point for refinement.
//
// hq-946577.23: Iterative refinement for decision points
package decision

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// Storage defines the minimal storage interface needed for iteration.
// This avoids importing the full storage package and allows for easier testing.
type Storage interface {
	CreateIssue(ctx context.Context, issue *types.Issue, actor string) error
	CreateDecisionPoint(ctx context.Context, dp *types.DecisionPoint) error
	GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error)
	AddDependency(ctx context.Context, dep *types.Dependency, actor string) error
	GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error)
}

// IterationResult contains the result of creating a new iteration.
type IterationResult struct {
	NewDecisionID string             // ID of the new decision point
	Issue         *types.Issue       // The new gate issue
	DecisionPoint *types.DecisionPoint // The new decision point data
	MaxReached    bool               // True if this is the final iteration
}

// CreateNextIteration creates a new decision point iteration based on text guidance.
// It closes the current decision and creates a new one with iteration N+1.
//
// The new decision point:
//   - Has ID format: {base}.r{N} (e.g., mol.decision-1 -> mol.decision-1.r2)
//   - Links to prior via PriorID
//   - Copies the human's guidance text
//   - Reuses the same prompt (agent will refine options later)
//
// Returns nil IterationResult if max iterations already reached.
func CreateNextIteration(
	ctx context.Context,
	store Storage,
	currentDP *types.DecisionPoint,
	currentIssue *types.Issue,
	guidance string,
	respondedBy string,
	actor string,
) (*IterationResult, error) {
	// Check if max iterations reached
	if currentDP.Iteration >= currentDP.MaxIterations {
		return &IterationResult{
			MaxReached: true,
		}, nil
	}

	// Generate new iteration ID
	newID := generateIterationID(currentDP.IssueID, currentDP.Iteration+1)

	// Create the new gate issue
	now := time.Now()
	newIssue := &types.Issue{
		ID:        newID,
		Title:     currentIssue.Title, // Keep same title
		IssueType: types.IssueType("gate"),
		Status:    types.StatusOpen,
		Priority:  currentIssue.Priority,
		AwaitType: "decision",
		Timeout:   currentIssue.Timeout,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Create the new decision point
	newDP := &types.DecisionPoint{
		IssueID:       newID,
		Prompt:        currentDP.Prompt,
		Options:       currentDP.Options, // Keep same options (agent will refine)
		DefaultOption: currentDP.DefaultOption,
		Iteration:     currentDP.Iteration + 1,
		MaxIterations: currentDP.MaxIterations,
		PriorID:       currentDP.IssueID,
		Guidance:      guidance,
		CreatedAt:     now,
	}

	// Create the new gate issue
	if err := store.CreateIssue(ctx, newIssue, actor); err != nil {
		return nil, fmt.Errorf("creating iteration gate: %w", err)
	}

	// Create the decision point record
	if err := store.CreateDecisionPoint(ctx, newDP); err != nil {
		return nil, fmt.Errorf("creating decision point: %w", err)
	}

	// Copy parent-child dependency from original
	// (new iteration should have same parent)
	deps, err := store.GetDependencyRecords(ctx, currentDP.IssueID)
	if err == nil {
		for _, dep := range deps {
			if dep.Type == types.DepParentChild {
				newDep := &types.Dependency{
					IssueID:     newID,
					DependsOnID: dep.DependsOnID,
					Type:        types.DepParentChild,
					CreatedAt:   now,
				}
				// Best effort - non-fatal if fails
				_ = store.AddDependency(ctx, newDep, actor)
			}
		}
	}

	// Copy blocking dependencies - the new iteration should block
	// the same issues the original blocked
	dependents, err := store.GetDependents(ctx, currentDP.IssueID)
	if err == nil {
		for _, dependent := range dependents {
			// Add block from dependent to new iteration
			newDep := &types.Dependency{
				IssueID:     dependent.ID,
				DependsOnID: newID,
				Type:        types.DepBlocks,
				CreatedAt:   now,
			}
			// Best effort - non-fatal if fails
			_ = store.AddDependency(ctx, newDep, actor)
		}
	}

	return &IterationResult{
		NewDecisionID: newID,
		Issue:         newIssue,
		DecisionPoint: newDP,
		MaxReached:    newDP.Iteration >= newDP.MaxIterations,
	}, nil
}

// generateIterationID creates the ID for a decision iteration.
// Format: base.rN where N is the iteration number (2+).
//
// Examples:
//   - mol.decision-1 iteration 2 -> mol.decision-1.r2
//   - mol.decision-1.r2 iteration 3 -> mol.decision-1.r3
func generateIterationID(baseID string, iteration int) string {
	// Strip any existing .rN suffix to get the true base
	base := baseID
	if idx := strings.LastIndex(baseID, ".r"); idx != -1 {
		// Check if what follows is a number
		suffix := baseID[idx+2:]
		isNumber := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				isNumber = false
				break
			}
		}
		if isNumber && len(suffix) > 0 {
			base = baseID[:idx]
		}
	}

	return fmt.Sprintf("%s.r%d", base, iteration)
}

// IsMaxIteration returns true if the decision point is at its max iteration.
func IsMaxIteration(dp *types.DecisionPoint) bool {
	return dp.Iteration >= dp.MaxIterations
}

// GetIterationSuffix returns the iteration suffix for display (e.g., " [iter 2/3]").
func GetIterationSuffix(dp *types.DecisionPoint) string {
	if dp.Iteration == 1 && dp.MaxIterations == 3 {
		// Default, don't show
		return ""
	}
	return fmt.Sprintf(" [iter %d/%d]", dp.Iteration, dp.MaxIterations)
}
