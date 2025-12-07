package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads"
)

// ContextEntry represents a single context entry for an issue
type ContextEntry struct {
	Timestamp   string   `json:"timestamp"`
	Type        string   `json:"type"` // note, finding, decision, blocker, link
	Content     string   `json:"content"`
	Source      string   `json:"source,omitempty"`      // For findings
	Confidence  string   `json:"confidence,omitempty"`  // high, medium, low
	Why         string   `json:"why,omitempty"`         // For decisions
	Alternatives []string `json:"alternatives,omitempty"` // For decisions
	Resolution  string   `json:"resolution,omitempty"`  // For blockers
	LinkedIssue string   `json:"linked_issue,omitempty"` // For links
}

// IssueContext holds all context for a single issue
type IssueContext struct {
	Title   string         `json:"title,omitempty"`
	Entries []ContextEntry `json:"entries,omitempty"`
	Tags    []string       `json:"tags,omitempty"`
}

// ContextStore holds context for all issues
type ContextStore struct {
	Issues map[string]*IssueContext `json:"issues"`
}

var (
	contextAddNote       string
	contextAddFinding    string
	contextAddDecision   string
	contextAddTag        string
	contextLink          string
	contextResolved      string
	contextResolution    string
	contextSource        string
	contextConfidence    string
	contextWhy           string
	contextAlternatives  string
	contextSearch        string
	contextTagFilter     string
	contextList          bool
)

var contextCmd = &cobra.Command{
	Use:   "context [issue-id]",
	Short: "Rich context trails for issues",
	Long: `Manage rich context for beads issues.

Context includes notes, findings, decisions, blockers, and links.
This helps preserve session knowledge across conversations.

Examples:
  bd context issue-abc                          Show all context
  bd context issue-abc --add "discovered API limits"
  bd context issue-abc --add-finding "Redis bottleneck" --confidence high
  bd context issue-abc --add-decision "Use PostgreSQL" --why "Better scale"
  bd context --search "bottleneck"              Search all context
  bd context --list                             List issues with context`,
	Run: func(cmd *cobra.Command, args []string) {
		// Track usage
		subcmd := "show"
		if contextSearch != "" {
			subcmd = "search"
		} else if contextList {
			subcmd = "list"
		} else if contextAddNote != "" || contextAddFinding != "" || contextAddDecision != "" {
			subcmd = "add"
		}
		TrackUsage("context", subcmd, args)

		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintln(os.Stderr, "Error: not in a beads project")
			os.Exit(1)
		}

		contextPath := filepath.Join(beadsDir, "context.json")
		store := loadContextStore(contextPath)

		// Handle search
		if contextSearch != "" {
			searchContext(store, contextSearch)
			return
		}

		// Handle tag filter
		if contextTagFilter != "" {
			filterByTag(store, contextTagFilter)
			return
		}

		// Handle list
		if contextList {
			listContextIssues(store)
			return
		}

		// Require issue ID for other operations
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Error: issue ID required")
			fmt.Fprintln(os.Stderr, "Usage: bd context <issue-id> [options]")
			os.Exit(1)
		}

		issueID := args[0]

		// Handle adding content
		if contextAddNote != "" {
			addEntry(store, issueID, "note", contextAddNote, contextPath)
			return
		}

		if contextAddFinding != "" {
			entry := ContextEntry{
				Timestamp:  time.Now().UTC().Format(time.RFC3339),
				Type:       "finding",
				Content:    contextAddFinding,
				Source:     contextSource,
				Confidence: contextConfidence,
			}
			addEntryStruct(store, issueID, entry, contextPath)
			return
		}

		if contextAddDecision != "" {
			var alts []string
			if contextAlternatives != "" {
				alts = strings.Split(contextAlternatives, ",")
				for i := range alts {
					alts[i] = strings.TrimSpace(alts[i])
				}
			}
			entry := ContextEntry{
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
				Type:         "decision",
				Content:      contextAddDecision,
				Why:          contextWhy,
				Alternatives: alts,
			}
			addEntryStruct(store, issueID, entry, contextPath)
			return
		}

		if contextAddTag != "" {
			addTags(store, issueID, contextAddTag, contextPath)
			return
		}

		if contextLink != "" {
			entry := ContextEntry{
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
				Type:        "link",
				Content:     fmt.Sprintf("Linked to %s", contextLink),
				LinkedIssue: contextLink,
			}
			addEntryStruct(store, issueID, entry, contextPath)
			return
		}

		if contextResolved != "" {
			entry := ContextEntry{
				Timestamp:  time.Now().UTC().Format(time.RFC3339),
				Type:       "blocker",
				Content:    contextResolved,
				Resolution: contextResolution,
			}
			addEntryStruct(store, issueID, entry, contextPath)
			return
		}

		// Default: show context
		showContext(store, issueID)
	},
}

func init() {
	contextCmd.Flags().StringVar(&contextAddNote, "add", "", "Add a note")
	contextCmd.Flags().StringVar(&contextAddFinding, "add-finding", "", "Add a finding")
	contextCmd.Flags().StringVar(&contextAddDecision, "add-decision", "", "Add a decision")
	contextCmd.Flags().StringVar(&contextAddTag, "add-tag", "", "Add tags (comma-separated)")
	contextCmd.Flags().StringVar(&contextLink, "link", "", "Link to another issue")
	contextCmd.Flags().StringVar(&contextResolved, "resolved", "", "Record a resolved blocker")
	contextCmd.Flags().StringVar(&contextResolution, "resolution", "", "How the blocker was resolved")
	contextCmd.Flags().StringVar(&contextSource, "source", "", "Source for findings")
	contextCmd.Flags().StringVar(&contextConfidence, "confidence", "", "Confidence level (high/medium/low)")
	contextCmd.Flags().StringVar(&contextWhy, "why", "", "Reason for decision")
	contextCmd.Flags().StringVar(&contextAlternatives, "alternatives", "", "Alternatives considered (comma-separated)")
	contextCmd.Flags().StringVar(&contextSearch, "search", "", "Search across all context")
	contextCmd.Flags().StringVar(&contextTagFilter, "tag", "", "Filter by tag")
	contextCmd.Flags().BoolVar(&contextList, "list", false, "List all issues with context")

	rootCmd.AddCommand(contextCmd)
}

func loadContextStore(path string) *ContextStore {
	store := &ContextStore{
		Issues: make(map[string]*IssueContext),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return store
	}

	// Try to unmarshal as new format first (has "issues" key)
	if err := json.Unmarshal(data, store); err == nil && len(store.Issues) > 0 {
		return store
	}

	// Reset for legacy format attempt
	store.Issues = make(map[string]*IssueContext)

	// Try legacy format (direct map of issue -> entries array)
	var legacy map[string]json.RawMessage
	if err := json.Unmarshal(data, &legacy); err != nil {
		return store
	}

	// Convert legacy format
	for id, raw := range legacy {
		// Skip the "issues" key if present (new format partial parse)
		if id == "issues" {
			continue
		}

		var entries []ContextEntry

		// Try legacy format (array of {date, text})
		var oldEntries []struct {
			Date string `json:"date"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &oldEntries); err == nil && len(oldEntries) > 0 {
			for _, e := range oldEntries {
				if e.Text != "" {
					entries = append(entries, ContextEntry{
						Timestamp: e.Date,
						Type:      "note",
						Content:   e.Text,
					})
				}
			}
		} else {
			// Try new entry format
			if err := json.Unmarshal(raw, &entries); err != nil {
				continue
			}
		}

		if len(entries) > 0 {
			store.Issues[id] = &IssueContext{Entries: entries}
		}
	}

	return store
}

func saveContextStore(store *ContextStore, path string) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func addEntry(store *ContextStore, issueID, entryType, content, path string) {
	entry := ContextEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Type:      entryType,
		Content:   content,
	}
	addEntryStruct(store, issueID, entry, path)
}

func addEntryStruct(store *ContextStore, issueID string, entry ContextEntry, path string) {
	if store.Issues[issueID] == nil {
		store.Issues[issueID] = &IssueContext{}
	}
	store.Issues[issueID].Entries = append(store.Issues[issueID].Entries, entry)

	if err := saveContextStore(store, path); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving context: %v\n", err)
		os.Exit(1)
	}

	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("%s Added %s to %s\n", green("✓"), entry.Type, issueID)
}

func addTags(store *ContextStore, issueID, tags, path string) {
	if store.Issues[issueID] == nil {
		store.Issues[issueID] = &IssueContext{}
	}

	newTags := strings.Split(tags, ",")
	for _, t := range newTags {
		t = strings.TrimSpace(t)
		if t != "" {
			// Check for duplicates
			found := false
			for _, existing := range store.Issues[issueID].Tags {
				if existing == t {
					found = true
					break
				}
			}
			if !found {
				store.Issues[issueID].Tags = append(store.Issues[issueID].Tags, t)
			}
		}
	}

	if err := saveContextStore(store, path); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving context: %v\n", err)
		os.Exit(1)
	}

	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("%s Added tags to %s\n", green("✓"), issueID)
}

func showContext(store *ContextStore, issueID string) {
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	ctx := store.Issues[issueID]
	if ctx == nil || (len(ctx.Entries) == 0 && len(ctx.Tags) == 0) {
		fmt.Printf("\n%s Context: %s\n", bold("📋"), issueID)
		fmt.Println("═══════════════════════════════════════════════════════════════════")
		fmt.Println("  (no context yet)")
		fmt.Println()
		fmt.Println("  Add context with:")
		fmt.Printf("    bd context %s --add \"note\"\n", issueID)
		return
	}

	fmt.Printf("\n%s Context: %s\n", bold("📋"), issueID)
	fmt.Println("═══════════════════════════════════════════════════════════════════")

	// Show tags
	if len(ctx.Tags) > 0 {
		fmt.Printf("\n%s Tags: %s\n", yellow("🏷"), strings.Join(ctx.Tags, ", "))
	}

	// Group entries by type
	var notes, findings, decisions, blockers, links []ContextEntry
	for _, e := range ctx.Entries {
		switch e.Type {
		case "finding":
			findings = append(findings, e)
		case "decision":
			decisions = append(decisions, e)
		case "blocker":
			blockers = append(blockers, e)
		case "link":
			links = append(links, e)
		default:
			notes = append(notes, e)
		}
	}

	// Print each section
	if len(findings) > 0 {
		fmt.Printf("\n%s Findings:\n", cyan("🔍"))
		for _, e := range findings {
			conf := ""
			if e.Confidence != "" {
				conf = fmt.Sprintf(" [%s]", e.Confidence)
			}
			src := ""
			if e.Source != "" {
				src = fmt.Sprintf(" (from: %s)", e.Source)
			}
			fmt.Printf("  • %s%s%s\n", e.Content, conf, src)
			ts := e.Timestamp
			if len(ts) > 10 {
				ts = ts[:10]
			}
			fmt.Printf("    %s\n", color.New(color.FgHiBlack).Sprint(ts))
		}
	}

	if len(decisions) > 0 {
		fmt.Printf("\n%s Decisions:\n", green("✅"))
		for _, e := range decisions {
			fmt.Printf("  • %s\n", bold(e.Content))
			if e.Why != "" {
				fmt.Printf("    Why: %s\n", e.Why)
			}
			if len(e.Alternatives) > 0 {
				fmt.Printf("    Alternatives: %s\n", strings.Join(e.Alternatives, ", "))
			}
			ts := e.Timestamp
			if len(ts) > 10 {
				ts = ts[:10]
			}
			fmt.Printf("    %s\n", color.New(color.FgHiBlack).Sprint(ts))
		}
	}

	if len(blockers) > 0 {
		fmt.Printf("\n%s Resolved Blockers:\n", yellow("🚧"))
		for _, e := range blockers {
			fmt.Printf("  • %s\n", e.Content)
			if e.Resolution != "" {
				fmt.Printf("    Resolution: %s\n", e.Resolution)
			}
			ts := e.Timestamp
			if len(ts) > 10 {
				ts = ts[:10]
			}
			fmt.Printf("    %s\n", color.New(color.FgHiBlack).Sprint(ts))
		}
	}

	if len(links) > 0 {
		fmt.Printf("\n%s Related Issues:\n", cyan("🔗"))
		for _, e := range links {
			fmt.Printf("  • %s\n", e.LinkedIssue)
		}
	}

	if len(notes) > 0 {
		fmt.Printf("\n%s Notes:\n", cyan("📝"))
		for _, e := range notes {
			fmt.Printf("  • %s\n", e.Content)
			ts := e.Timestamp
			if len(ts) > 10 {
				ts = ts[:10]
			}
			fmt.Printf("    %s\n", color.New(color.FgHiBlack).Sprint(ts))
		}
	}

	fmt.Println()
}

func searchContext(store *ContextStore, query string) {
	query = strings.ToLower(query)
	cyan := color.New(color.FgCyan).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	fmt.Printf("\n%s Search results for: %s\n", cyan("🔍"), bold(query))
	fmt.Println("═══════════════════════════════════════════════════════════════════")

	found := 0
	for issueID, ctx := range store.Issues {
		for _, e := range ctx.Entries {
			if strings.Contains(strings.ToLower(e.Content), query) ||
				strings.Contains(strings.ToLower(e.Why), query) ||
				strings.Contains(strings.ToLower(e.Resolution), query) {
				fmt.Printf("\n  [%s] %s\n", cyan(issueID), e.Content)
				if e.Type != "note" {
					fmt.Printf("    Type: %s\n", e.Type)
				}
				found++
			}
		}
	}

	if found == 0 {
		fmt.Println("\n  No matches found")
	}
	fmt.Println()
}

func filterByTag(store *ContextStore, tag string) {
	tag = strings.ToLower(tag)
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Printf("\n%s Issues tagged: %s\n", yellow("🏷"), tag)
	fmt.Println("═══════════════════════════════════════════════════════════════════")

	found := 0
	for issueID, ctx := range store.Issues {
		for _, t := range ctx.Tags {
			if strings.ToLower(t) == tag {
				fmt.Printf("  • %s\n", cyan(issueID))
				found++
				break
			}
		}
	}

	if found == 0 {
		fmt.Println("\n  No issues with this tag")
	}
	fmt.Println()
}

func listContextIssues(store *ContextStore) {
	cyan := color.New(color.FgCyan).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	fmt.Printf("\n%s Issues with context:\n", bold("📋"))
	fmt.Println("═══════════════════════════════════════════════════════════════════")

	if len(store.Issues) == 0 {
		fmt.Println("\n  No issues with context yet")
		fmt.Println()
		return
	}

	for issueID, ctx := range store.Issues {
		entryCount := len(ctx.Entries)
		tagCount := len(ctx.Tags)
		fmt.Printf("  • %s: %d entries", cyan(issueID), entryCount)
		if tagCount > 0 {
			fmt.Printf(", %d tags", tagCount)
		}
		fmt.Println()
	}
	fmt.Println()
}
