package sqlite

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// CollisionResult categorizes incoming issues by their relationship to existing DB state
type CollisionResult struct {
	ExactMatches []string           // IDs that match exactly (idempotent import)
	Collisions   []*CollisionDetail // Issues with same ID but different content
	NewIssues    []string           // IDs that don't exist in DB yet
}

// CollisionDetail provides detailed information about a collision
type CollisionDetail struct {
	ID                string        // The issue ID that collided
	IncomingIssue     *types.Issue  // The issue from the import file
	ExistingIssue     *types.Issue  // The issue currently in the database
	ConflictingFields []string      // List of field names that differ
	ReferenceScore    int           // Number of references to this issue (for scoring)
}

// DetectCollisions compares incoming JSONL issues against DB state
// It distinguishes between:
//  1. Exact match (idempotent) - ID and content are identical
//  2. ID match but different content (collision) - same ID, different fields
//  3. New issue - ID doesn't exist in DB
//
// Returns a CollisionResult categorizing all incoming issues.
func DetectCollisions(ctx context.Context, s *SQLiteStorage, incomingIssues []*types.Issue) (*CollisionResult, error) {
	result := &CollisionResult{
		ExactMatches: make([]string, 0),
		Collisions:   make([]*CollisionDetail, 0),
		NewIssues:    make([]string, 0),
	}

	for _, incoming := range incomingIssues {
		// Check if issue exists in database
		existing, err := s.GetIssue(ctx, incoming.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check issue %s: %w", incoming.ID, err)
		}

		if existing == nil {
			// Issue doesn't exist in DB - it's new
			result.NewIssues = append(result.NewIssues, incoming.ID)
			continue
		}

		// Issue exists - compare content
		conflicts := compareIssues(existing, incoming)
		if len(conflicts) == 0 {
			// No differences - exact match (idempotent)
			result.ExactMatches = append(result.ExactMatches, incoming.ID)
		} else {
			// Same ID but different content - collision
			result.Collisions = append(result.Collisions, &CollisionDetail{
				ID:                incoming.ID,
				IncomingIssue:     incoming,
				ExistingIssue:     existing,
				ConflictingFields: conflicts,
			})
		}
	}

	return result, nil
}

// compareIssues compares two issues and returns a list of field names that differ
// Timestamps (CreatedAt, UpdatedAt, ClosedAt) are intentionally not compared
// Dependencies are also not compared (handled separately in import)
func compareIssues(existing, incoming *types.Issue) []string {
	conflicts := make([]string, 0)

	// Compare all relevant fields
	if existing.Title != incoming.Title {
		conflicts = append(conflicts, "title")
	}
	if existing.Description != incoming.Description {
		conflicts = append(conflicts, "description")
	}
	if existing.Design != incoming.Design {
		conflicts = append(conflicts, "design")
	}
	if existing.AcceptanceCriteria != incoming.AcceptanceCriteria {
		conflicts = append(conflicts, "acceptance_criteria")
	}
	if existing.Notes != incoming.Notes {
		conflicts = append(conflicts, "notes")
	}
	if existing.Status != incoming.Status {
		conflicts = append(conflicts, "status")
	}
	if existing.Priority != incoming.Priority {
		conflicts = append(conflicts, "priority")
	}
	if existing.IssueType != incoming.IssueType {
		conflicts = append(conflicts, "issue_type")
	}
	if existing.Assignee != incoming.Assignee {
		conflicts = append(conflicts, "assignee")
	}

	// Compare EstimatedMinutes (handle nil cases)
	if !equalIntPtr(existing.EstimatedMinutes, incoming.EstimatedMinutes) {
		conflicts = append(conflicts, "estimated_minutes")
	}

	return conflicts
}

// equalIntPtr compares two *int pointers for equality
func equalIntPtr(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// ScoreCollisions calculates reference scores for all colliding issues and sorts them
// by score ascending (fewest references first). This minimizes the total number of
// updates needed during renumbering - issues with fewer references are renumbered first.
//
// Reference score = text mentions + dependency references
func ScoreCollisions(ctx context.Context, s *SQLiteStorage, collisions []*CollisionDetail, allIssues []*types.Issue) error {
	// Build a map of all issues for quick lookup
	issueMap := make(map[string]*types.Issue)
	for _, issue := range allIssues {
		issueMap[issue.ID] = issue
	}

	// Get all dependency records for efficient lookup
	allDeps, err := s.GetAllDependencyRecords(ctx)
	if err != nil {
		return fmt.Errorf("failed to get dependency records: %w", err)
	}

	// Calculate reference score for each collision
	for _, collision := range collisions {
		score, err := countReferences(collision.ID, allIssues, allDeps)
		if err != nil {
			return fmt.Errorf("failed to count references for %s: %w", collision.ID, err)
		}
		collision.ReferenceScore = score
	}

	// Sort collisions by reference score ascending (fewest first)
	sort.Slice(collisions, func(i, j int) bool {
		return collisions[i].ReferenceScore < collisions[j].ReferenceScore
	})

	return nil
}

// countReferences counts how many times an issue ID is referenced
// Returns: text mentions + dependency references
func countReferences(issueID string, allIssues []*types.Issue, allDeps map[string][]*types.Dependency) (int, error) {
	count := 0

	// Count text mentions in all issues' text fields
	// Use word boundary regex to match exact IDs (e.g., "bd-10" but not "bd-100")
	pattern := fmt.Sprintf(`\b%s\b`, regexp.QuoteMeta(issueID))
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, fmt.Errorf("failed to compile regex for %s: %w", issueID, err)
	}

	for _, issue := range allIssues {
		// Skip counting references in the issue itself
		if issue.ID == issueID {
			continue
		}

		// Count mentions in description
		count += len(re.FindAllString(issue.Description, -1))

		// Count mentions in design
		count += len(re.FindAllString(issue.Design, -1))

		// Count mentions in notes
		count += len(re.FindAllString(issue.Notes, -1))

		// Count mentions in acceptance criteria
		count += len(re.FindAllString(issue.AcceptanceCriteria, -1))
	}

	// Count dependency references
	// An issue can be referenced as either IssueID or DependsOnID
	for _, deps := range allDeps {
		for _, dep := range deps {
			// Skip self-references
			if dep.IssueID == issueID && dep.DependsOnID == issueID {
				continue
			}

			// Count if this issue is the source (IssueID)
			if dep.IssueID == issueID {
				count++
			}

			// Count if this issue is the target (DependsOnID)
			if dep.DependsOnID == issueID {
				count++
			}
		}
	}

	return count, nil
}

// RemapCollisions handles ID remapping for colliding issues
// Takes sorted collisions (fewest references first) and remaps them to new IDs
// Returns a map of old ID -> new ID for reporting
//
// NOTE: This function is not atomic - it performs multiple separate database operations.
// If an error occurs partway through, some issues may be created without their references
// being updated. This is a known limitation that requires storage layer refactoring to fix.
// See issue bd-25 for transaction support.
func RemapCollisions(ctx context.Context, s *SQLiteStorage, collisions []*CollisionDetail, allIssues []*types.Issue) (map[string]string, error) {
	idMapping := make(map[string]string)

	// For each collision (in order of ascending reference score)
	for _, collision := range collisions {
		oldID := collision.ID

		// Allocate new ID using atomic counter
		prefix, err := s.GetConfig(ctx, "issue_prefix")
		if err != nil || prefix == "" {
			prefix = "bd"
		}
		nextID, err := s.getNextIDForPrefix(ctx, prefix)
		if err != nil {
			return nil, fmt.Errorf("failed to generate new ID for collision %s: %w", oldID, err)
		}
		newID := fmt.Sprintf("%s-%d", prefix, nextID)

		// Record mapping
		idMapping[oldID] = newID

		// Update the issue ID in the incoming issue
		collision.IncomingIssue.ID = newID

		// Create the issue with new ID
		// Note: CreateIssue will use the ID we set
		if err := s.CreateIssue(ctx, collision.IncomingIssue, "import-remap"); err != nil {
			return nil, fmt.Errorf("failed to create remapped issue %s -> %s: %w", oldID, newID, err)
		}
	}

	// Now update all references in text fields and dependencies
	if err := updateReferences(ctx, s, idMapping); err != nil {
		return nil, fmt.Errorf("failed to update references: %w", err)
	}

	return idMapping, nil
}

// updateReferences updates all text field references and dependency records
// to point to new IDs based on the idMapping
func updateReferences(ctx context.Context, s *SQLiteStorage, idMapping map[string]string) error {
	// Update text fields in all issues (both DB and incoming)
	// We need to update issues in the database
	dbIssues, err := s.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return fmt.Errorf("failed to get all issues from DB: %w", err)
	}

	for _, issue := range dbIssues {
		updates := make(map[string]interface{})

		// Update description
		newDesc := replaceIDReferences(issue.Description, idMapping)
		if newDesc != issue.Description {
			updates["description"] = newDesc
		}

		// Update design
		newDesign := replaceIDReferences(issue.Design, idMapping)
		if newDesign != issue.Design {
			updates["design"] = newDesign
		}

		// Update notes
		newNotes := replaceIDReferences(issue.Notes, idMapping)
		if newNotes != issue.Notes {
			updates["notes"] = newNotes
		}

		// Update acceptance criteria
		newAC := replaceIDReferences(issue.AcceptanceCriteria, idMapping)
		if newAC != issue.AcceptanceCriteria {
			updates["acceptance_criteria"] = newAC
		}

		// If there are updates, apply them
		if len(updates) > 0 {
			if err := s.UpdateIssue(ctx, issue.ID, updates, "import-remap"); err != nil {
				return fmt.Errorf("failed to update references in issue %s: %w", issue.ID, err)
			}
		}
	}

	// Update dependency records
	if err := updateDependencyReferences(ctx, s, idMapping); err != nil {
		return fmt.Errorf("failed to update dependency references: %w", err)
	}

	return nil
}

// replaceIDReferences replaces all occurrences of old IDs with new IDs in text
// Uses word-boundary regex to ensure exact matches (bd-10 but not bd-100)
// Uses a two-phase approach to avoid replacement conflicts: first replace with
// placeholders, then replace placeholders with new IDs
func replaceIDReferences(text string, idMapping map[string]string) string {
	// Phase 1: Replace all old IDs with unique placeholders
	placeholders := make(map[string]string)
	result := text
	i := 0
	for oldID, newID := range idMapping {
		placeholder := fmt.Sprintf("__PLACEHOLDER_%d__", i)
		placeholders[placeholder] = newID

		// Use word boundary regex for exact matching
		pattern := fmt.Sprintf(`\b%s\b`, regexp.QuoteMeta(oldID))
		re := regexp.MustCompile(pattern)
		result = re.ReplaceAllString(result, placeholder)
		i++
	}

	// Phase 2: Replace all placeholders with new IDs
	for placeholder, newID := range placeholders {
		result = strings.ReplaceAll(result, placeholder, newID)
	}

	return result
}

// updateDependencyReferences updates dependency records to use new IDs
// This handles both IssueID and DependsOnID fields
func updateDependencyReferences(ctx context.Context, s *SQLiteStorage, idMapping map[string]string) error {
	// Get all dependency records
	allDeps, err := s.GetAllDependencyRecords(ctx)
	if err != nil {
		return fmt.Errorf("failed to get all dependencies: %w", err)
	}

	// Phase 1: Collect all changes to avoid race conditions while iterating
	type depUpdate struct {
		oldIssueID     string
		oldDependsOnID string
		newDep         *types.Dependency
	}
	var updates []depUpdate

	for _, deps := range allDeps {
		for _, dep := range deps {
			needsUpdate := false
			newIssueID := dep.IssueID
			newDependsOnID := dep.DependsOnID

			// Check if either ID was remapped
			if mappedID, ok := idMapping[dep.IssueID]; ok {
				newIssueID = mappedID
				needsUpdate = true
			}
			if mappedID, ok := idMapping[dep.DependsOnID]; ok {
				newDependsOnID = mappedID
				needsUpdate = true
			}

			if needsUpdate {
				updates = append(updates, depUpdate{
					oldIssueID:     dep.IssueID,
					oldDependsOnID: dep.DependsOnID,
					newDep: &types.Dependency{
						IssueID:     newIssueID,
						DependsOnID: newDependsOnID,
						Type:        dep.Type,
					},
				})
			}
		}
	}

	// Phase 2: Apply all collected changes
	for _, update := range updates {
		// Remove old dependency
		if err := s.RemoveDependency(ctx, update.oldIssueID, update.oldDependsOnID, "import-remap"); err != nil {
			// If the dependency doesn't exist (e.g., already removed), that's okay
			// This can happen if both IssueID and DependsOnID were remapped
			continue
		}

		// Add new dependency with updated IDs
		if err := s.AddDependency(ctx, update.newDep, "import-remap"); err != nil {
			return fmt.Errorf("failed to add updated dependency %s -> %s: %w",
				update.newDep.IssueID, update.newDep.DependsOnID, err)
		}
	}

	return nil
}
