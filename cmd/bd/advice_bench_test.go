//go:build bench

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// Benchmark configuration constants
const (
	// Scale test parameters
	smallAdviceCount  = 100   // 100 advice items targeting one agent
	mediumAdviceCount = 500   // 500 advice items
	largeAdviceCount  = 1000  // 1000+ total advice items
	xlargeAdviceCount = 5000  // Stress test
	labelsPerAdvice   = 10    // Many labels per advice for compound matching
	numSubscriptions  = 20    // Agent subscriptions for filtering
)

// adviceBenchHelper provides setup methods for advice benchmarks
type adviceBenchHelper struct {
	b      *testing.B
	ctx    context.Context
	store  *sqlite.SQLiteStorage
	tmpDir string
}

// newAdviceBenchHelper creates a benchmark helper with a fresh database
func newAdviceBenchHelper(b *testing.B) *adviceBenchHelper {
	b.Helper()
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, ".beads", "beads.db")

	ctx := context.Background()
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		b.Fatalf("Failed to create store: %v", err)
	}

	// Initialize database with issue prefix
	if err := store.SetConfig(ctx, "issue_prefix", "adv-"); err != nil {
		store.Close()
		b.Fatalf("Failed to set issue_prefix: %v", err)
	}

	return &adviceBenchHelper{
		b:      b,
		ctx:    ctx,
		store:  store,
		tmpDir: tmpDir,
	}
}

// cleanup closes the database
func (h *adviceBenchHelper) cleanup() {
	h.store.Close()
}

// createAdviceWithLabels creates advice items with varying label configurations
func (h *adviceBenchHelper) createAdviceWithLabels(count int, labelGen func(i int) []string) []string {
	h.b.Helper()
	ids := make([]string, count)

	for i := 0; i < count; i++ {
		advice := &types.Issue{
			Title:       fmt.Sprintf("Advice %d: Performance test advice item", i),
			Description: fmt.Sprintf("This is advice item %d for performance testing. It contains detailed instructions.", i),
			Priority:    2,
			IssueType:   types.IssueType("advice"),
			Status:      types.StatusOpen,
			CreatedAt:   time.Now(),
		}

		if err := h.store.CreateIssue(h.ctx, advice, "bench-user"); err != nil {
			h.b.Fatalf("Failed to create advice %d: %v", i, err)
		}

		// Add labels
		labels := labelGen(i)
		for _, label := range labels {
			if err := h.store.AddLabel(h.ctx, advice.ID, label, "bench-user"); err != nil {
				h.b.Fatalf("Failed to add label %s to advice %d: %v", label, i, err)
			}
		}

		ids[i] = advice.ID
	}

	return ids
}

// allTargetOneAgent creates labels so all advice targets the same agent
func allTargetOneAgent(i int) []string {
	// All advice targets agent:beads/polecats/quartz
	labels := []string{
		"global",
		"rig:beads",
		"role:polecat",
		"agent:beads/polecats/quartz",
	}
	// Add some variety in topic labels
	topics := []string{"testing", "security", "performance", "documentation", "ci"}
	labels = append(labels, topics[i%len(topics)])
	return labels
}

// variedTargets creates labels with varied targeting
func variedTargets(i int) []string {
	labels := []string{}

	// 20% global advice
	if i%5 == 0 {
		labels = append(labels, "global")
		return labels
	}

	// Different rigs (40%)
	rigs := []string{"beads", "gastown", "citadel", "thevault"}
	rigIdx := i % len(rigs)
	labels = append(labels, fmt.Sprintf("rig:%s", rigs[rigIdx]))

	// Add role for 30%
	if i%3 == 0 {
		roles := []string{"polecat", "crew", "witness"}
		labels = append(labels, fmt.Sprintf("role:%s", roles[i%len(roles)]))
	}

	// Add agent for 10%
	if i%10 == 0 {
		agents := []string{
			"beads/polecats/quartz",
			"beads/polecats/garnet",
			"beads/crew/wolf",
			"gastown/polecats/alpha",
		}
		labels = append(labels, fmt.Sprintf("agent:%s", agents[i%len(agents)]))
	}

	return labels
}

// manyLabelsPerAdvice creates advice with many labels for compound matching tests
func manyLabelsPerAdvice(i int) []string {
	labels := make([]string, 0, labelsPerAdvice)

	// Base targeting
	labels = append(labels, "global")
	labels = append(labels, fmt.Sprintf("rig:rig%d", i%5))
	labels = append(labels, fmt.Sprintf("role:role%d", i%3))

	// Add compound groups (AND semantics within group)
	// g0: group requires both labels to match
	labels = append(labels, fmt.Sprintf("g0:topic%d", i%10))
	labels = append(labels, fmt.Sprintf("g0:category%d", i%5))

	// g1: another group
	labels = append(labels, fmt.Sprintf("g1:area%d", i%7))
	labels = append(labels, fmt.Sprintf("g1:level%d", i%3))

	// Additional topic labels
	for j := 0; j < 3; j++ {
		labels = append(labels, fmt.Sprintf("topic%d-%d", i%20, j))
	}

	return labels
}

// BenchmarkAdviceQuery100TargetingOneAgent benchmarks querying 100 advice items
// that all target a single agent
func BenchmarkAdviceQuery100TargetingOneAgent(b *testing.B) {
	h := newAdviceBenchHelper(b)
	defer h.cleanup()

	// Create 100 advice items all targeting one agent
	h.createAdviceWithLabels(smallAdviceCount, allTargetOneAgent)

	// Build agent subscriptions
	subscriptions := buildAgentSubscriptions("beads/polecats/quartz", nil)

	// Get the advice type filter
	adviceType := types.IssueType("advice")
	openStatus := types.StatusOpen
	filter := types.IssueFilter{
		IssueType: &adviceType,
		Status:    &openStatus,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Query all advice
		issues, err := h.store.SearchIssues(h.ctx, "", filter)
		if err != nil {
			b.Fatalf("Failed to search: %v", err)
		}

		// Get labels for filtering
		issueIDs := make([]string, len(issues))
		for j, issue := range issues {
			issueIDs[j] = issue.ID
		}
		labelsMap, err := h.store.GetLabelsForIssues(h.ctx, issueIDs)
		if err != nil {
			b.Fatalf("Failed to get labels: %v", err)
		}

		// Filter by subscriptions
		var applicable []*types.Issue
		for _, issue := range issues {
			if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
				applicable = append(applicable, issue)
			}
		}

		if len(applicable) < smallAdviceCount {
			b.Fatalf("Expected at least %d applicable advice, got %d", smallAdviceCount, len(applicable))
		}
	}
}

// BenchmarkAdviceList1000Total benchmarks listing with 1000+ advice items total
func BenchmarkAdviceList1000Total(b *testing.B) {
	h := newAdviceBenchHelper(b)
	defer h.cleanup()

	// Create 1000 advice items with varied targeting
	h.createAdviceWithLabels(largeAdviceCount, variedTargets)

	adviceType := types.IssueType("advice")
	openStatus := types.StatusOpen
	filter := types.IssueFilter{
		IssueType: &adviceType,
		Status:    &openStatus,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		issues, err := h.store.SearchIssues(h.ctx, "", filter)
		if err != nil {
			b.Fatalf("Failed to search: %v", err)
		}

		if len(issues) < largeAdviceCount {
			b.Fatalf("Expected at least %d issues, got %d", largeAdviceCount, len(issues))
		}
	}
}

// BenchmarkAdviceFilteringLatency benchmarks the advice filtering latency
// with various subscription configurations
func BenchmarkAdviceFilteringLatency(b *testing.B) {
	h := newAdviceBenchHelper(b)
	defer h.cleanup()

	// Create 500 advice items with varied targeting
	h.createAdviceWithLabels(mediumAdviceCount, variedTargets)

	// Pre-fetch all advice and labels (simulating cached query)
	adviceType := types.IssueType("advice")
	openStatus := types.StatusOpen
	filter := types.IssueFilter{
		IssueType: &adviceType,
		Status:    &openStatus,
	}
	issues, err := h.store.SearchIssues(h.ctx, "", filter)
	if err != nil {
		b.Fatalf("Failed to search: %v", err)
	}
	issueIDs := make([]string, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}
	labelsMap, err := h.store.GetLabelsForIssues(h.ctx, issueIDs)
	if err != nil {
		b.Fatalf("Failed to get labels: %v", err)
	}

	// Build subscriptions for a typical agent
	subscriptions := buildAgentSubscriptions("beads/polecats/quartz", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Pure filtering (no DB queries)
		var applicable []*types.Issue
		for _, issue := range issues {
			if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
				applicable = append(applicable, issue)
			}
		}
		_ = applicable
	}
}

// BenchmarkLabelMatchingManyLabels benchmarks label matching with many labels per advice
func BenchmarkLabelMatchingManyLabels(b *testing.B) {
	h := newAdviceBenchHelper(b)
	defer h.cleanup()

	// Create advice with many labels per item
	h.createAdviceWithLabels(mediumAdviceCount, manyLabelsPerAdvice)

	// Pre-fetch all advice and labels
	adviceType := types.IssueType("advice")
	openStatus := types.StatusOpen
	filter := types.IssueFilter{
		IssueType: &adviceType,
		Status:    &openStatus,
	}
	issues, err := h.store.SearchIssues(h.ctx, "", filter)
	if err != nil {
		b.Fatalf("Failed to search: %v", err)
	}
	issueIDs := make([]string, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}
	labelsMap, err := h.store.GetLabelsForIssues(h.ctx, issueIDs)
	if err != nil {
		b.Fatalf("Failed to get labels: %v", err)
	}

	// Build subscriptions that will exercise compound matching
	subscriptions := []string{
		"global",
		"rig:rig0",
		"role:role0",
		"topic0",
		"topic1",
		"category0",
		"area0",
		"level0",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var applicable []*types.Issue
		for _, issue := range issues {
			if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
				applicable = append(applicable, issue)
			}
		}
		_ = applicable
	}
}

// BenchmarkParseGroups benchmarks the parseGroups function with complex label sets
func BenchmarkParseGroups(b *testing.B) {
	// Create a complex label set
	labels := []string{
		"global",
		"rig:beads",
		"role:polecat",
		"g0:topic1",
		"g0:category1",
		"g1:area1",
		"g1:level1",
		"g2:section1",
		"g2:type1",
		"g2:scope1",
		"testing",
		"security",
		"performance",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		groups := parseGroups(labels)
		_ = groups
	}
}

// BenchmarkMatchesSubscriptions benchmarks the core matching function
func BenchmarkMatchesSubscriptions(b *testing.B) {
	issue := &types.Issue{ID: "test-advice-1"}
	labels := []string{
		"global",
		"rig:beads",
		"role:polecat",
		"testing",
		"security",
	}
	subscriptions := []string{
		"global",
		"rig:beads",
		"role:polecat",
		"role:polecats",
		"agent:beads/polecats/quartz",
		"testing",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		matched := matchesSubscriptions(issue, labels, subscriptions)
		_ = matched
	}
}

// BenchmarkMatchesSubscriptionsNoMatch benchmarks matching when no subscriptions match
func BenchmarkMatchesSubscriptionsNoMatch(b *testing.B) {
	issue := &types.Issue{ID: "test-advice-1"}
	labels := []string{
		"rig:gastown",
		"role:crew",
		"agent:gastown/crew/wolf",
	}
	subscriptions := []string{
		"global",
		"rig:beads",
		"role:polecat",
		"agent:beads/polecats/quartz",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		matched := matchesSubscriptions(issue, labels, subscriptions)
		_ = matched
	}
}

// BenchmarkMatchesSubscriptionsCompound benchmarks compound label matching
func BenchmarkMatchesSubscriptionsCompound(b *testing.B) {
	issue := &types.Issue{ID: "test-advice-1"}
	// Labels with compound groups
	labels := []string{
		"g0:role:polecat",
		"g0:rig:beads",
		"g1:testing",
		"g1:go",
		"security",
	}
	subscriptions := []string{
		"role:polecat",
		"rig:beads",
		"testing",
		"go",
		"security",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		matched := matchesSubscriptions(issue, labels, subscriptions)
		_ = matched
	}
}

// BenchmarkBuildAgentSubscriptions benchmarks subscription building for agents
func BenchmarkBuildAgentSubscriptions(b *testing.B) {
	// Don't access the store for this benchmark
	oldStore := store
	store = nil
	defer func() { store = oldStore }()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		subs := buildAgentSubscriptions("beads/polecats/quartz", nil)
		_ = subs
	}
}

// BenchmarkAdviceList5000Total stress tests listing with 5000 advice items
func BenchmarkAdviceList5000Total(b *testing.B) {
	h := newAdviceBenchHelper(b)
	defer h.cleanup()

	// Create 5000 advice items (stress test)
	b.Logf("Creating %d advice items for stress test...", xlargeAdviceCount)
	h.createAdviceWithLabels(xlargeAdviceCount, variedTargets)

	adviceType := types.IssueType("advice")
	openStatus := types.StatusOpen
	filter := types.IssueFilter{
		IssueType: &adviceType,
		Status:    &openStatus,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		issues, err := h.store.SearchIssues(h.ctx, "", filter)
		if err != nil {
			b.Fatalf("Failed to search: %v", err)
		}

		// Get labels and filter
		issueIDs := make([]string, len(issues))
		for j, issue := range issues {
			issueIDs[j] = issue.ID
		}
		labelsMap, err := h.store.GetLabelsForIssues(h.ctx, issueIDs)
		if err != nil {
			b.Fatalf("Failed to get labels: %v", err)
		}

		subscriptions := buildAgentSubscriptions("beads/polecats/quartz", nil)
		var applicable []*types.Issue
		for _, issue := range issues {
			if matchesSubscriptions(issue, labelsMap[issue.ID], subscriptions) {
				applicable = append(applicable, issue)
			}
		}
		_ = applicable
	}
}

// BenchmarkGetLabelsForIssues benchmarks the batch label retrieval
func BenchmarkGetLabelsForIssues(b *testing.B) {
	h := newAdviceBenchHelper(b)
	defer h.cleanup()

	// Create advice with labels
	ids := h.createAdviceWithLabels(largeAdviceCount, variedTargets)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		labelsMap, err := h.store.GetLabelsForIssues(h.ctx, ids)
		if err != nil {
			b.Fatalf("Failed to get labels: %v", err)
		}
		_ = labelsMap
	}
}

// BenchmarkMatchesAnyLabel benchmarks the simple label matching function
func BenchmarkMatchesAnyLabel(b *testing.B) {
	issueLabels := []string{
		"global", "rig:beads", "role:polecat",
		"testing", "security", "performance",
		"documentation", "ci", "code-review",
	}
	filterLabels := []string{"security", "performance"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		matched := matchesAnyLabel(issueLabels, filterLabels)
		_ = matched
	}
}
