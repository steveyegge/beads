package main

import (
	"context"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

type dependentNode struct {
	Issue    *types.Issue     `json:"issue"`
	Children []*dependentNode `json:"children,omitempty"`
}

type specCascadeSummary struct {
	SpecID          string           `json:"spec_id"`
	ChangeCount     int              `json:"change_count"`
	OpenIssues      int              `json:"open_issues"`
	VolatilityLevel string           `json:"volatility_level"`
	DirectIssues    int              `json:"direct_issues"`
	DependentIssues int              `json:"dependent_issues"`
	TotalIssues     int              `json:"total_issues"`
	Tree            []*dependentNode `json:"tree"`
	Recommendation  string           `json:"recommendation,omitempty"`
}

func renderSpecDependents(ctx context.Context, specID string, since time.Time) error {
	issueStore, specStore, cleanup, err := openVolatilityStores(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if daemonClient == nil {
		if err := ensureDatabaseFresh(ctx); err != nil {
			return err
		}
	}

	entry, err := specStore.GetSpecRegistry(ctx, specID)
	if err != nil {
		return err
	}
	if entry == nil || entry.MissingAt != nil {
		return fmt.Errorf("spec not found: %s", specID)
	}

	summary, err := getSpecVolatilitySummary(ctx, specID, since)
	if err != nil {
		return err
	}
	if summary == nil {
		summary = &specVolatilitySummary{}
	}

	roots, dependentCount, tree, err := buildDependentsTree(ctx, issueStore, specID)
	if err != nil {
		return err
	}

	level := classifySpecVolatility(summary.ChangeCount, summary.OpenIssues)
	cascade := specCascadeSummary{
		SpecID:          specID,
		ChangeCount:     summary.ChangeCount,
		OpenIssues:      summary.OpenIssues,
		VolatilityLevel: string(level),
		DirectIssues:    roots,
		DependentIssues: dependentCount,
		TotalIssues:     roots + dependentCount,
		Tree:            tree,
		Recommendation:  recommendationForLevel(level, summary.OpenIssues, dependentCount),
	}

	if jsonOutput {
		outputJSON(cascade)
		return nil
	}

	fmt.Printf("%s (%s volatility: %d changes, %d open)\n",
		specID, formatVolatilityLevel(level), summary.ChangeCount, summary.OpenIssues)

	for _, node := range tree {
		printDependentTree(node, "", true)
	}

	fmt.Println()
	fmt.Println("IMPACT SUMMARY:")
	fmt.Printf("  • %d issues directly affected\n", roots)
	fmt.Printf("  • %d issues blocked downstream\n", dependentCount)
	fmt.Printf("  • Total cascade: %d issues at risk\n", roots+dependentCount)
	if cascade.Recommendation != "" {
		fmt.Printf("\nRECOMMENDATION: %s\n", cascade.Recommendation)
	}
	return nil
}

func renderSpecRecommendations(ctx context.Context, entries []spec.SpecRiskEntry, since time.Time) error {
	issueStore, _, cleanup, err := openVolatilityStores(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if daemonClient == nil {
		if err := ensureDatabaseFresh(ctx); err != nil {
			return err
		}
	}

	if jsonOutput {
		type rec struct {
			SpecID         string `json:"spec_id"`
			Volatility     string `json:"volatility"`
			Changes        int    `json:"changes"`
			OpenIssues     int    `json:"open_issues"`
			DependentCount int    `json:"dependent_count"`
			Action         string `json:"action"`
		}
		results := make([]rec, 0, len(entries))
		for _, entry := range entries {
			dependents, _ := countSpecDependents(ctx, issueStore, entry.SpecID)
			level := classifySpecVolatility(entry.ChangeCount, entry.OpenIssues)
			results = append(results, rec{
				SpecID:         entry.SpecID,
				Volatility:     string(level),
				Changes:        entry.ChangeCount,
				OpenIssues:     entry.OpenIssues,
				DependentCount: dependents,
				Action:         recommendationForLevel(level, entry.OpenIssues, dependents),
			})
		}
		outputJSON(results)
		return nil
	}

	fmt.Println("RECOMMENDATIONS BY SPEC:")
	fmt.Println()
	for _, entry := range entries {
		dependents, _ := countSpecDependents(ctx, issueStore, entry.SpecID)
		level := classifySpecVolatility(entry.ChangeCount, entry.OpenIssues)
		action := recommendationForLevel(level, entry.OpenIssues, dependents)
		fmt.Printf("%s (%s)\n", entry.SpecID, formatVolatilityLevel(level))
		if action != "" {
			fmt.Printf("  Action: %s\n", action)
		}
		fmt.Printf("  Reason: %d changes, %d open issues", entry.ChangeCount, entry.OpenIssues)
		if dependents > 0 {
			fmt.Printf(", %d dependents", dependents)
		}
		fmt.Println()
	}
	return nil
}

func buildDependentsTree(ctx context.Context, issueStore storage.Storage, specID string) (int, int, []*dependentNode, error) {
	filter := types.IssueFilter{
		SpecID:        &specID,
		ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
	}
	issues, err := issueStore.SearchIssues(ctx, "", filter)
	if err != nil {
		return 0, 0, nil, err
	}

	visited := make(map[string]bool)
	dependentIDs := make(map[string]struct{})
	nodes := make([]*dependentNode, 0, len(issues))

	for _, issue := range issues {
		node := buildDependentNode(ctx, issueStore, issue, visited, dependentIDs)
		if node != nil {
			nodes = append(nodes, node)
		}
	}
	return len(issues), len(dependentIDs), nodes, nil
}

func buildDependentNode(ctx context.Context, issueStore storage.Storage, issue *types.Issue, visited map[string]bool, dependentIDs map[string]struct{}) *dependentNode {
	if issue == nil {
		return nil
	}
	if visited[issue.ID] {
		return nil
	}
	visited[issue.ID] = true

	node := &dependentNode{Issue: issue}
	dependents, err := issueStore.GetDependents(ctx, issue.ID)
	if err != nil {
		return node
	}
	for _, dep := range dependents {
		if dep.Status == types.StatusClosed || dep.Status == types.StatusTombstone {
			continue
		}
		if _, ok := dependentIDs[dep.ID]; !ok {
			dependentIDs[dep.ID] = struct{}{}
		}
		child := buildDependentNode(ctx, issueStore, dep, visited, dependentIDs)
		if child != nil {
			node.Children = append(node.Children, child)
		}
	}
	return node
}

func printDependentTree(node *dependentNode, prefix string, isLast bool) {
	if node == nil || node.Issue == nil {
		return
	}
	connector := "├── "
	nextPrefix := prefix + "│   "
	if isLast {
		connector = "└── "
		nextPrefix = prefix + "    "
	}
	status := string(node.Issue.Status)
	fmt.Printf("%s%s%s: %s (%s)\n", prefix, connector, node.Issue.ID, node.Issue.Title, status)
	for i, child := range node.Children {
		printDependentTree(child, nextPrefix, i == len(node.Children)-1)
	}
}

func formatVolatilityLevel(level specVolatilityLevel) string {
	switch level {
	case specVolatilityHigh:
		return ui.RenderWarn("● HIGH")
	case specVolatilityMedium:
		return ui.RenderWarn("◐ MEDIUM")
	case specVolatilityLow:
		return ui.RenderMuted("○ LOW")
	case specVolatilityStable:
		return ui.RenderPass("✓ STABLE")
	default:
		return string(level)
	}
}

func recommendationForLevel(level specVolatilityLevel, openIssues, dependents int) string {
	switch level {
	case specVolatilityHigh:
		if dependents > 0 {
			return "STABILIZE: lock spec and unblock dependents"
		}
		return "STABILIZE: freeze spec changes before more work"
	case specVolatilityMedium:
		return "REVIEW: confirm spec before starting new work"
	case specVolatilityLow:
		if openIssues == 0 {
			return "MONITOR: low activity, likely stable"
		}
		return "MONITOR: proceed with caution"
	case specVolatilityStable:
		if openIssues == 0 {
			return "ARCHIVE: safe to compact"
		}
		return "CONTINUE: stable foundation"
	default:
		return ""
	}
}

func countSpecDependents(ctx context.Context, issueStore storage.Storage, specID string) (int, error) {
	filter := types.IssueFilter{
		SpecID:        &specID,
		ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
	}
	issues, err := issueStore.SearchIssues(ctx, "", filter)
	if err != nil {
		return 0, err
	}
	visited := make(map[string]bool)
	dependentIDs := make(map[string]struct{})
	for _, issue := range issues {
		_ = buildDependentNode(ctx, issueStore, issue, visited, dependentIDs)
	}
	return len(dependentIDs), nil
}
