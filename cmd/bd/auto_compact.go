package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

func maybeAutoCompactDaemon(ctx context.Context, closedIssues []*types.Issue, compactFlag bool, client *rpc.Client) {
	if len(closedIssues) == 0 || client == nil {
		return
	}
	specIDs := uniqueSpecIDs(closedIssues)
	if len(specIDs) == 0 {
		return
	}

	for _, specID := range specIDs {
		if !spec.IsScannableSpecID(specID) {
			continue
		}
		resp, err := client.SpecShow(&rpc.SpecShowArgs{SpecID: specID})
		if err != nil {
			fmt.Fprintf(os.Stderr, "spec show failed for %s: %v\n", specID, err)
			continue
		}
		var result rpc.SpecShowResult
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			fmt.Fprintf(os.Stderr, "invalid spec show response for %s: %v\n", specID, err)
			continue
		}
		if result.Spec == nil {
			continue
		}
		if result.Spec.Lifecycle == "archived" && result.Spec.Summary != "" {
			continue
		}
		if hasOpenBeads(result.Beads) {
			continue
		}
		specText := readSpecContent(specID)
		summary := buildAutoSpecSummary(result.Spec, specText, result.Beads)
		if compactFlag {
			now := time.Now().UTC().Truncate(time.Second)
			summaryTokens := len(strings.Fields(summary))
			_, err := client.SpecCompact(&rpc.SpecCompactArgs{
				SpecID:        specID,
				Lifecycle:     "archived",
				Summary:       summary,
				SummaryTokens: summaryTokens,
				ArchivedAt:    &now,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "spec compact failed for %s: %v\n", specID, err)
				continue
			}
			if !jsonOutput {
				fmt.Printf("%s Archived spec: %s\n", ui.RenderPass("✓"), specID)
			}
			continue
		}
		if !jsonOutput {
			printCompactSuggestion(specID, summary)
		}
	}
	_ = ctx
}

func maybeAutoCompactDirect(ctx context.Context, closedIssues []*types.Issue, compactFlag bool, store storage.Storage, specStore spec.SpecRegistryStore) {
	if len(closedIssues) == 0 || store == nil || specStore == nil {
		return
	}
	specIDs := uniqueSpecIDs(closedIssues)
	if len(specIDs) == 0 {
		return
	}

	for _, specID := range specIDs {
		if !spec.IsScannableSpecID(specID) {
			continue
		}
		entry, err := specStore.GetSpecRegistry(ctx, specID)
		if err != nil || entry == nil {
			continue
		}
		if entry.Lifecycle == "archived" && entry.Summary != "" {
			continue
		}
		openIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{
			SpecID:        &specID,
			ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
		})
		if err != nil || len(openIssues) > 0 {
			continue
		}
		beads, err := store.SearchIssues(ctx, "", types.IssueFilter{SpecID: &specID})
		if err != nil {
			continue
		}
		specText := readSpecContent(specID)
		summary := buildAutoSpecSummary(entry, specText, beads)
		if compactFlag {
			now := time.Now().UTC().Truncate(time.Second)
			summaryTokens := len(strings.Fields(summary))
			update := spec.SpecRegistryUpdate{
				Lifecycle:     ptrString("archived"),
				Summary:       &summary,
				SummaryTokens: &summaryTokens,
				ArchivedAt:    &now,
			}
			if err := specStore.UpdateSpecRegistry(ctx, specID, update); err != nil {
				fmt.Fprintf(os.Stderr, "spec compact failed for %s: %v\n", specID, err)
				continue
			}
			if !jsonOutput {
				fmt.Printf("%s Archived spec: %s\n", ui.RenderPass("✓"), specID)
			}
			continue
		}
		if !jsonOutput {
			printCompactSuggestion(specID, summary)
		}
	}
}

func uniqueSpecIDs(issues []*types.Issue) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue == nil || strings.TrimSpace(issue.SpecID) == "" {
			continue
		}
		if _, ok := seen[issue.SpecID]; ok {
			continue
		}
		seen[issue.SpecID] = struct{}{}
		ids = append(ids, issue.SpecID)
	}
	sort.Strings(ids)
	return ids
}

func hasOpenBeads(beads []*types.Issue) bool {
	for _, bead := range beads {
		if bead == nil {
			continue
		}
		if bead.Status != types.StatusClosed && bead.Status != types.StatusTombstone {
			return true
		}
	}
	return false
}

func buildAutoSpecSummary(entry *spec.SpecRegistryEntry, specText string, beads []*types.Issue) string {
	specTitle := ""
	specID := ""
	if entry != nil {
		specTitle = strings.TrimSpace(entry.Title)
		specID = entry.SpecID
	}
	title := specTitle
	if title == "" {
		if specID != "" {
			title = fmt.Sprintf("Spec %s", specID)
		} else {
			title = "Spec summary"
		}
	}

	overview, bullets := extractSpecHighlights(specText)
	closedTitles := collectClosedTitles(beads, 6)
	countClosed := countClosedBeads(beads)

	var parts []string
	parts = append(parts, fmt.Sprintf("%s.", title))
	if overview != "" {
		parts = append(parts, fmt.Sprintf("Summary: %s.", overview))
	} else if len(bullets) > 0 {
		parts = append(parts, fmt.Sprintf("Key points: %s.", strings.Join(bullets, "; ")))
	}
	if len(closedTitles) > 0 {
		parts = append(parts, fmt.Sprintf("Implemented work: %s.", strings.Join(closedTitles, "; ")))
	} else {
		parts = append(parts, "Implemented work is complete; no linked bead titles were available.")
	}
	parts = append(parts, fmt.Sprintf("Completed beads: %d.", countClosed))

	return trimSummaryWords(strings.Join(parts, " "), 80)
}

func collectClosedTitles(beads []*types.Issue, limit int) []string {
	type item struct {
		title    string
		closedAt time.Time
		id       string
	}
	var items []item
	seen := map[string]struct{}{}
	for _, bead := range beads {
		if bead == nil {
			continue
		}
		if bead.Status != types.StatusClosed {
			continue
		}
		title := strings.TrimSpace(bead.Title)
		if title == "" {
			continue
		}
		if _, ok := seen[title]; ok {
			continue
		}
		seen[title] = struct{}{}
		t := time.Time{}
		if bead.ClosedAt != nil {
			t = *bead.ClosedAt
		}
		items = append(items, item{title: title, closedAt: t, id: bead.ID})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].closedAt.Equal(items[j].closedAt) {
			return items[i].id < items[j].id
		}
		return items[i].closedAt.After(items[j].closedAt)
	})
	if limit <= 0 {
		limit = 5
	}
	if len(items) > limit {
		items = items[:limit]
	}
	titles := make([]string, 0, len(items))
	for _, it := range items {
		titles = append(titles, it.title)
	}
	return titles
}

func countClosedBeads(beads []*types.Issue) int {
	count := 0
	for _, bead := range beads {
		if bead == nil {
			continue
		}
		if bead.Status == types.StatusClosed {
			count++
		}
	}
	return count
}

func printCompactSuggestion(specID, summary string) {
	fmt.Printf("\nSpec %s has no open beads.\n", specID)
	fmt.Printf("Run: bd spec compact %s --summary %q\n", specID, summary)
}

func ptrString(s string) *string {
	return &s
}

func readSpecContent(specID string) string {
	if !spec.IsScannableSpecID(specID) {
		return ""
	}
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return ""
	}
	repoRoot := filepath.Dir(beadsDir)
	path := specID
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, specID)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func extractSpecHighlights(specText string) (string, []string) {
	if strings.TrimSpace(specText) == "" {
		return "", nil
	}

	importantSections := map[string]struct{}{
		"overview": {}, "summary": {}, "requirements": {}, "api": {}, "behavior": {},
		"decisions": {}, "constraints": {}, "non-goals": {}, "non goals": {},
		"out of scope": {}, "risks": {},
	}

	section := ""
	inCode := false
	overview := ""
	var bullets []string

	lines := strings.Split(specText, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if inCode || line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
			headingLower := strings.ToLower(heading)
			section = ""
			for key := range importantSections {
				if strings.Contains(headingLower, key) {
					section = key
					break
				}
			}
			continue
		}
		if section == "" {
			continue
		}
		if isBullet(line) {
			if len(bullets) < 6 {
				bullets = append(bullets, trimBullet(line))
			}
			continue
		}
		if overview == "" && len(line) > 20 {
			overview = line
		}
	}

	return overview, bullets
}

func isBullet(line string) bool {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return true
	}
	if len(line) > 2 && line[0] >= '0' && line[0] <= '9' && line[1] == '.' {
		return true
	}
	return false
}

func trimBullet(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return strings.TrimSpace(line[2:])
	}
	if len(line) > 2 && line[0] >= '0' && line[0] <= '9' && line[1] == '.' {
		return strings.TrimSpace(line[2:])
	}
	return line
}

func trimSummaryWords(text string, maxWords int) string {
	if maxWords <= 0 {
		return text
	}
	words := strings.Fields(text)
	if len(words) <= maxWords {
		return text
	}
	return strings.Join(words[:maxWords], " ") + "..."
}
