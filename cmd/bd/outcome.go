package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads"
)

// Outcome represents a recorded outcome for an issue
type Outcome struct {
	Issue       string   `json:"issue"`
	Title       string   `json:"title"`
	Closed      string   `json:"closed"`
	Success     bool     `json:"success"`
	DurationMin *int     `json:"duration_min,omitempty"`
	Approach    string   `json:"approach,omitempty"`
	Complexity  string   `json:"complexity,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	BlockersHit int      `json:"blockers_hit,omitempty"`
}

var (
	outcomeSuccess    bool
	outcomeFailure    bool
	outcomeApproach   string
	outcomeComplexity string
	outcomeByApproach bool
	outcomeByTag      bool
	outcomeRecent     int
)

var outcomeCmd = &cobra.Command{
	Use:   "outcome [subcommand]",
	Short: "Track outcomes for emergent learning",
	Long: `Track and analyze outcomes of completed issues.

Outcomes help identify patterns in what approaches work for different
types of problems, enabling emergent learning over time.

Examples:
  bd outcome record issue-abc --success --approach implement-iterate
  bd outcome show issue-abc
  bd outcome stats --by-approach
  bd outcome recent 10`,
}

var outcomeRecordCmd = &cobra.Command{
	Use:   "record <issue-id>",
	Short: "Record outcome when closing an issue",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintln(os.Stderr, "Error: not in a beads project")
			os.Exit(1)
		}

		issueID := args[0]
		outcomesPath := filepath.Join(beadsDir, "outcomes.jsonl")

		// Check if already recorded
		outcomes := loadOutcomes(outcomesPath)
		for _, o := range outcomes {
			if o.Issue == issueID {
				fmt.Fprintf(os.Stderr, "Outcome already recorded for %s\n", issueID)
				os.Exit(1)
			}
		}

		// Get issue details from database
		var title string
		if store != nil {
			issue, err := store.GetIssue(rootCtx, issueID)
			if err == nil && issue != nil {
				title = issue.Title
			}
		}

		success := true
		if outcomeFailure {
			success = false
		}

		outcome := Outcome{
			Issue:      issueID,
			Title:      title,
			Closed:     time.Now().UTC().Format(time.RFC3339),
			Success:    success,
			Approach:   outcomeApproach,
			Complexity: outcomeComplexity,
		}

		// Append to outcomes file
		f, err := os.OpenFile(outcomesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening outcomes file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()

		data, _ := json.Marshal(outcome)
		f.WriteString(string(data) + "\n")

		green := color.New(color.FgGreen).SprintFunc()
		status := "success"
		if !success {
			status = "failure"
		}
		fmt.Printf("%s Recorded %s outcome for %s\n", green("✓"), status, issueID)
	},
}

var outcomeShowCmd = &cobra.Command{
	Use:   "show <issue-id>",
	Short: "Show outcome for a specific issue",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintln(os.Stderr, "Error: not in a beads project")
			os.Exit(1)
		}

		issueID := args[0]
		outcomesPath := filepath.Join(beadsDir, "outcomes.jsonl")
		outcomes := loadOutcomes(outcomesPath)

		for _, o := range outcomes {
			if o.Issue == issueID {
				showOutcome(o)
				return
			}
		}

		fmt.Printf("No outcome recorded for %s\n", issueID)
	},
}

var outcomeStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show aggregate statistics",
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintln(os.Stderr, "Error: not in a beads project")
			os.Exit(1)
		}

		outcomesPath := filepath.Join(beadsDir, "outcomes.jsonl")
		outcomes := loadOutcomes(outcomesPath)

		if len(outcomes) == 0 {
			fmt.Println("No outcomes recorded yet")
			return
		}

		if outcomeByApproach {
			showStatsByApproach(outcomes)
		} else if outcomeByTag {
			showStatsByTag(outcomes)
		} else {
			showOverallStats(outcomes)
		}
	},
}

var outcomeRecentCmd = &cobra.Command{
	Use:   "recent [n]",
	Short: "Show n most recent outcomes",
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintln(os.Stderr, "Error: not in a beads project")
			os.Exit(1)
		}

		n := 10
		if len(args) > 0 {
			fmt.Sscanf(args[0], "%d", &n)
		}

		outcomesPath := filepath.Join(beadsDir, "outcomes.jsonl")
		outcomes := loadOutcomes(outcomesPath)

		if len(outcomes) == 0 {
			fmt.Println("No outcomes recorded yet")
			return
		}

		// Sort by closed date descending
		sort.Slice(outcomes, func(i, j int) bool {
			return outcomes[i].Closed > outcomes[j].Closed
		})

		if n > len(outcomes) {
			n = len(outcomes)
		}

		cyan := color.New(color.FgCyan).SprintFunc()
		green := color.New(color.FgGreen).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()

		fmt.Printf("\n%s Recent outcomes (%d):\n\n", cyan("📊"), n)

		for i := 0; i < n; i++ {
			o := outcomes[i]
			status := green("✓")
			if !o.Success {
				status = red("✗")
			}

			title := o.Title
			if len(title) > 50 {
				title = title[:47] + "..."
			}

			approach := o.Approach
			if approach == "" {
				approach = "unknown"
			}

			fmt.Printf("  %s [%s] %s (%s)\n", status, cyan(o.Issue), title, approach)
		}
		fmt.Println()
	},
}

func init() {
	outcomeRecordCmd.Flags().BoolVar(&outcomeSuccess, "success", false, "Mark as successful (default)")
	outcomeRecordCmd.Flags().BoolVar(&outcomeFailure, "failure", false, "Mark as failed")
	outcomeRecordCmd.Flags().StringVar(&outcomeApproach, "approach", "", "Approach used (e.g., implement-iterate)")
	outcomeRecordCmd.Flags().StringVar(&outcomeComplexity, "complexity", "", "Complexity: low, medium, high")

	outcomeStatsCmd.Flags().BoolVar(&outcomeByApproach, "by-approach", false, "Group stats by approach")
	outcomeStatsCmd.Flags().BoolVar(&outcomeByTag, "by-tag", false, "Group stats by tag")

	outcomeCmd.AddCommand(outcomeRecordCmd)
	outcomeCmd.AddCommand(outcomeShowCmd)
	outcomeCmd.AddCommand(outcomeStatsCmd)
	outcomeCmd.AddCommand(outcomeRecentCmd)

	rootCmd.AddCommand(outcomeCmd)
}

func loadOutcomes(path string) []Outcome {
	var outcomes []Outcome

	f, err := os.Open(path)
	if err != nil {
		return outcomes
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var o Outcome
		if err := json.Unmarshal([]byte(line), &o); err == nil {
			outcomes = append(outcomes, o)
		}
	}

	return outcomes
}

func showOutcome(o Outcome) {
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	status := green("SUCCESS")
	if !o.Success {
		status = red("FAILURE")
	}

	fmt.Printf("\n%s Outcome: %s\n", bold("📋"), o.Issue)
	fmt.Println("═══════════════════════════════════════════════════════════════════")
	fmt.Printf("  Title:      %s\n", cyan(o.Title))
	fmt.Printf("  Status:     %s\n", status)
	fmt.Printf("  Closed:     %s\n", o.Closed[:10])
	if o.Approach != "" {
		fmt.Printf("  Approach:   %s\n", o.Approach)
	}
	if o.Complexity != "" {
		fmt.Printf("  Complexity: %s\n", o.Complexity)
	}
	if o.DurationMin != nil {
		fmt.Printf("  Duration:   %d min\n", *o.DurationMin)
	}
	fmt.Println()
}

func showOverallStats(outcomes []Outcome) {
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	total := len(outcomes)
	successes := 0
	for _, o := range outcomes {
		if o.Success {
			successes++
		}
	}

	rate := float64(successes) / float64(total) * 100

	fmt.Printf("\n%s Outcome Statistics\n", cyan("📊"))
	fmt.Println("═══════════════════════════════════════════════════════════════════")
	fmt.Printf("  Total outcomes: %d\n", total)
	fmt.Printf("  Successes:      %s (%d)\n", green(fmt.Sprintf("%.0f%%", rate)), successes)
	fmt.Printf("  Failures:       %d\n", total-successes)
	fmt.Println()
}

func showStatsByApproach(outcomes []Outcome) {
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	// Group by approach
	type approachStats struct {
		total    int
		success  int
		approach string
	}

	byApproach := make(map[string]*approachStats)
	for _, o := range outcomes {
		approach := o.Approach
		if approach == "" {
			approach = "unknown"
		}
		if byApproach[approach] == nil {
			byApproach[approach] = &approachStats{approach: approach}
		}
		byApproach[approach].total++
		if o.Success {
			byApproach[approach].success++
		}
	}

	// Sort by success rate
	var sorted []*approachStats
	for _, s := range byApproach {
		sorted = append(sorted, s)
	}
	sort.Slice(sorted, func(i, j int) bool {
		rateI := float64(sorted[i].success) / float64(sorted[i].total)
		rateJ := float64(sorted[j].success) / float64(sorted[j].total)
		return rateI > rateJ
	})

	fmt.Printf("\n%s Stats by Approach\n", cyan("📊"))
	fmt.Println("═══════════════════════════════════════════════════════════════════")

	for _, s := range sorted {
		rate := float64(s.success) / float64(s.total) * 100
		bar := strings.Repeat("█", int(rate/10))
		fmt.Printf("  %-20s %s %s (%d/%d)\n",
			s.approach,
			green(bar),
			fmt.Sprintf("%.0f%%", rate),
			s.success, s.total)
	}
	fmt.Println()
}

func showStatsByTag(outcomes []Outcome) {
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()

	// Group by tag
	type tagStats struct {
		total   int
		success int
		tag     string
	}

	byTag := make(map[string]*tagStats)
	for _, o := range outcomes {
		for _, tag := range o.Tags {
			if byTag[tag] == nil {
				byTag[tag] = &tagStats{tag: tag}
			}
			byTag[tag].total++
			if o.Success {
				byTag[tag].success++
			}
		}
	}

	if len(byTag) == 0 {
		fmt.Println("No tags recorded in outcomes")
		return
	}

	// Sort by count
	var sorted []*tagStats
	for _, s := range byTag {
		sorted = append(sorted, s)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].total > sorted[j].total
	})

	fmt.Printf("\n%s Stats by Tag\n", cyan("📊"))
	fmt.Println("═══════════════════════════════════════════════════════════════════")

	for _, s := range sorted {
		rate := float64(s.success) / float64(s.total) * 100
		fmt.Printf("  %-20s %s (%d/%d)\n",
			s.tag,
			green(fmt.Sprintf("%.0f%%", rate)),
			s.success, s.total)
	}
	fmt.Println()
}
