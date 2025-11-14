// Package fixtures provides realistic test data generation for benchmarks and tests.
package fixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/importer"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// labels used across all fixtures
var commonLabels = []string{
	"backend",
	"frontend",
	"urgent",
	"tech-debt",
	"documentation",
	"performance",
	"security",
	"ux",
	"api",
	"database",
}

// assignees used across all fixtures
var commonAssignees = []string{
	"alice",
	"bob",
	"charlie",
	"diana",
	"eve",
	"frank",
}

// epic titles for realistic data
var epicTitles = []string{
	"User Authentication System",
	"Payment Processing Integration",
	"Mobile App Redesign",
	"Performance Optimization",
	"API v2 Migration",
	"Search Functionality Enhancement",
	"Analytics Dashboard",
	"Multi-tenant Support",
	"Notification System",
	"Data Export Feature",
}

// feature titles (under epics)
var featureTitles = []string{
	"OAuth2 Integration",
	"Password Reset Flow",
	"Two-Factor Authentication",
	"Session Management",
	"API Endpoints",
	"Database Schema",
	"UI Components",
	"Background Jobs",
	"Error Handling",
	"Testing Infrastructure",
}

// task titles (under features)
var taskTitles = []string{
	"Implement login endpoint",
	"Add validation logic",
	"Write unit tests",
	"Update documentation",
	"Fix memory leak",
	"Optimize query performance",
	"Add error logging",
	"Refactor helper functions",
	"Update database migrations",
	"Configure deployment",
}

// LargeSQLite creates a 10K issue database with realistic patterns
func LargeSQLite(ctx context.Context, store storage.Storage) error {
	return generateIssues(ctx, store, 10000, rand.New(rand.NewSource(42)))
}

// XLargeSQLite creates a 20K issue database with realistic patterns
func XLargeSQLite(ctx context.Context, store storage.Storage) error {
	return generateIssues(ctx, store, 20000, rand.New(rand.NewSource(43)))
}

// LargeFromJSONL creates a 10K issue database by exporting to JSONL and reimporting
func LargeFromJSONL(ctx context.Context, store storage.Storage, tempDir string) error {
	return generateFromJSONL(ctx, store, tempDir, 10000, rand.New(rand.NewSource(44)))
}

// XLargeFromJSONL creates a 20K issue database by exporting to JSONL and reimporting
func XLargeFromJSONL(ctx context.Context, store storage.Storage, tempDir string) error {
	return generateFromJSONL(ctx, store, tempDir, 20000, rand.New(rand.NewSource(45)))
}

// generateIssues creates n issues with realistic epic hierarchies and cross-links
func generateIssues(ctx context.Context, store storage.Storage, n int, rng *rand.Rand) error {
	// Calculate breakdown: 10% epics, 30% features, 60% tasks
	numEpics := n / 10
	numFeatures := (n * 3) / 10
	numTasks := n - numEpics - numFeatures

	// Track created issues for cross-linking
	var allIssues []*types.Issue
	epicIssues := make([]*types.Issue, 0, numEpics)
	featureIssues := make([]*types.Issue, 0, numFeatures)
	taskIssues := make([]*types.Issue, 0, numTasks)

	// Create epics
	for i := 0; i < numEpics; i++ {
		issue := &types.Issue{
			Title:       fmt.Sprintf("%s (Epic %d)", epicTitles[i%len(epicTitles)], i),
			Description: fmt.Sprintf("Epic for %s", epicTitles[i%len(epicTitles)]),
			Status:      randomStatus(rng, 0.5), // 50% open, 50% closed
			Priority:    randomPriority(rng),
			IssueType:   types.TypeEpic,
			Assignee:    commonAssignees[rng.Intn(len(commonAssignees))],
			CreatedAt:   randomTime(rng, 180), // Created up to 180 days ago
			UpdatedAt:   time.Now(),
		}

		if issue.Status == types.StatusClosed {
			closedAt := randomTime(rng, 30)
			issue.ClosedAt = &closedAt
		}

		if err := store.CreateIssue(ctx, issue, "fixture"); err != nil {
			return fmt.Errorf("failed to create epic: %w", err)
		}

		// Add labels to epics
		for j := 0; j < rng.Intn(3)+1; j++ {
			label := commonLabels[rng.Intn(len(commonLabels))]
			_ = store.AddLabel(ctx, issue.ID, label, "fixture")
		}

		epicIssues = append(epicIssues, issue)
		allIssues = append(allIssues, issue)
	}

	// Create features under epics
	for i := 0; i < numFeatures; i++ {
		parentEpic := epicIssues[i%len(epicIssues)]

		issue := &types.Issue{
			Title:       fmt.Sprintf("%s (Feature %d)", featureTitles[i%len(featureTitles)], i),
			Description: fmt.Sprintf("Feature under %s", parentEpic.Title),
			Status:      randomStatus(rng, 0.5),
			Priority:    randomPriority(rng),
			IssueType:   types.TypeFeature,
			Assignee:    commonAssignees[rng.Intn(len(commonAssignees))],
			CreatedAt:   randomTime(rng, 150),
			UpdatedAt:   time.Now(),
		}

		if issue.Status == types.StatusClosed {
			closedAt := randomTime(rng, 30)
			issue.ClosedAt = &closedAt
		}

		if err := store.CreateIssue(ctx, issue, "fixture"); err != nil {
			return fmt.Errorf("failed to create feature: %w", err)
		}

		// Add parent-child dependency to epic
		dep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: parentEpic.ID,
			Type:        types.DepParentChild,
			CreatedAt:   time.Now(),
			CreatedBy:   "fixture",
		}
		if err := store.AddDependency(ctx, dep, "fixture"); err != nil {
			return fmt.Errorf("failed to add feature-epic dependency: %w", err)
		}

		// Add labels
		for j := 0; j < rng.Intn(3)+1; j++ {
			label := commonLabels[rng.Intn(len(commonLabels))]
			_ = store.AddLabel(ctx, issue.ID, label, "fixture")
		}

		featureIssues = append(featureIssues, issue)
		allIssues = append(allIssues, issue)
	}

	// Create tasks under features
	for i := 0; i < numTasks; i++ {
		parentFeature := featureIssues[i%len(featureIssues)]

		issue := &types.Issue{
			Title:       fmt.Sprintf("%s (Task %d)", taskTitles[i%len(taskTitles)], i),
			Description: fmt.Sprintf("Task under %s", parentFeature.Title),
			Status:      randomStatus(rng, 0.5),
			Priority:    randomPriority(rng),
			IssueType:   types.TypeTask,
			Assignee:    commonAssignees[rng.Intn(len(commonAssignees))],
			CreatedAt:   randomTime(rng, 120),
			UpdatedAt:   time.Now(),
		}

		if issue.Status == types.StatusClosed {
			closedAt := randomTime(rng, 30)
			issue.ClosedAt = &closedAt
		}

		if err := store.CreateIssue(ctx, issue, "fixture"); err != nil {
			return fmt.Errorf("failed to create task: %w", err)
		}

		// Add parent-child dependency to feature
		dep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: parentFeature.ID,
			Type:        types.DepParentChild,
			CreatedAt:   time.Now(),
			CreatedBy:   "fixture",
		}
		if err := store.AddDependency(ctx, dep, "fixture"); err != nil {
			return fmt.Errorf("failed to add task-feature dependency: %w", err)
		}

		// Add labels
		for j := 0; j < rng.Intn(2)+1; j++ {
			label := commonLabels[rng.Intn(len(commonLabels))]
			_ = store.AddLabel(ctx, issue.ID, label, "fixture")
		}

		taskIssues = append(taskIssues, issue)
		allIssues = append(allIssues, issue)
	}

	// Add cross-links: 20% of tasks block other tasks across epics
	numCrossLinks := numTasks / 5
	for i := 0; i < numCrossLinks; i++ {
		fromTask := taskIssues[rng.Intn(len(taskIssues))]
		toTask := taskIssues[rng.Intn(len(taskIssues))]

		// Avoid self-dependencies
		if fromTask.ID == toTask.ID {
			continue
		}

		dep := &types.Dependency{
			IssueID:     fromTask.ID,
			DependsOnID: toTask.ID,
			Type:        types.DepBlocks,
			CreatedAt:   time.Now(),
			CreatedBy:   "fixture",
		}

		// Ignore cycle errors for cross-links (they're expected)
		_ = store.AddDependency(ctx, dep, "fixture")
	}

	return nil
}

// generateFromJSONL creates issues, exports to JSONL, clears DB, and reimports
func generateFromJSONL(ctx context.Context, store storage.Storage, tempDir string, n int, rng *rand.Rand) error {
	// First generate issues normally
	if err := generateIssues(ctx, store, n, rng); err != nil {
		return fmt.Errorf("failed to generate issues: %w", err)
	}

	// Export to JSONL
	jsonlPath := filepath.Join(tempDir, "issues.jsonl")
	if err := exportToJSONL(ctx, store, jsonlPath); err != nil {
		return fmt.Errorf("failed to export to JSONL: %w", err)
	}

	// Clear all issues (we'll reimport them)
	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return fmt.Errorf("failed to get all issues: %w", err)
	}

	for _, issue := range allIssues {
		if err := store.DeleteIssue(ctx, issue.ID); err != nil {
			return fmt.Errorf("failed to delete issue %s: %w", issue.ID, err)
		}
	}

	// Import from JSONL
	if err := importFromJSONL(ctx, store, jsonlPath); err != nil {
		return fmt.Errorf("failed to import from JSONL: %w", err)
	}

	return nil
}

// exportToJSONL exports all issues to a JSONL file
func exportToJSONL(ctx context.Context, store storage.Storage, path string) error {
	// Get all issues
	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return fmt.Errorf("failed to query issues: %w", err)
	}

	// Populate dependencies and labels for each issue
	allDeps, err := store.GetAllDependencyRecords(ctx)
	if err != nil {
		return fmt.Errorf("failed to get dependencies: %w", err)
	}

	for _, issue := range allIssues {
		issue.Dependencies = allDeps[issue.ID]

		labels, err := store.GetLabels(ctx, issue.ID)
		if err != nil {
			return fmt.Errorf("failed to get labels for %s: %w", issue.ID, err)
		}
		issue.Labels = labels
	}

	// Write to JSONL file
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create JSONL file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	for _, issue := range allIssues {
		if err := encoder.Encode(issue); err != nil {
			return fmt.Errorf("failed to encode issue: %w", err)
		}
	}

	return nil
}

// importFromJSONL imports issues from a JSONL file
func importFromJSONL(ctx context.Context, store storage.Storage, path string) error {
	// Read JSONL file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read JSONL file: %w", err)
	}

	// Parse issues
	var issues []*types.Issue
	lines := string(data)
	for i, line := range splitLines(lines) {
		if len(line) == 0 {
			continue
		}

		var issue types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			return fmt.Errorf("failed to parse issue at line %d: %w", i+1, err)
		}

		issues = append(issues, &issue)
	}

	// Import using the importer
	opts := importer.Options{
		OrphanHandling: importer.OrphanAllow,
	}

	// Get database path from storage
	dbPath := store.Path()

	_, err = importer.ImportIssues(ctx, dbPath, store, issues, opts)
	if err != nil {
		return fmt.Errorf("failed to import issues: %w", err)
	}

	return nil
}

// splitLines splits a string by newlines
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// randomStatus returns a random status with given open ratio
func randomStatus(rng *rand.Rand, openRatio float64) types.Status {
	r := rng.Float64()
	if r < openRatio {
		// Open statuses: open, in_progress, blocked
		statuses := []types.Status{types.StatusOpen, types.StatusInProgress, types.StatusBlocked}
		return statuses[rng.Intn(len(statuses))]
	}
	return types.StatusClosed
}

// randomPriority returns a random priority with realistic distribution
// P0: 5%, P1: 15%, P2: 50%, P3: 25%, P4: 5%
func randomPriority(rng *rand.Rand) int {
	r := rng.Intn(100)
	switch {
	case r < 5:
		return 0
	case r < 20:
		return 1
	case r < 70:
		return 2
	case r < 95:
		return 3
	default:
		return 4
	}
}

// randomTime returns a random time up to maxDaysAgo days in the past
func randomTime(rng *rand.Rand, maxDaysAgo int) time.Time {
	daysAgo := rng.Intn(maxDaysAgo)
	return time.Now().Add(-time.Duration(daysAgo) * 24 * time.Hour)
}
