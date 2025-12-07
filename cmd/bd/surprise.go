package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads"
)

// SurpriseEntry represents a logged surprise
type SurpriseEntry struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"` // manual, duration, error
	Content   string `json:"content"`
	IssueID   string `json:"issue_id,omitempty"`
	ZScore    float64 `json:"z_score,omitempty"`
}

var (
	surpriseSigma float64
)

var surpriseCmd = &cobra.Command{
	Use:   "surprise [subcommand]",
	Short: "Statistical surprise detection (2σ outliers)",
	Long: `Detect statistically surprising patterns using confidence intervals.

Surfaces what's worth remembering based on deviation from baseline.
Uses 2σ (95% confidence interval) by default to flag outliers.

Examples:
  bd surprise analyze              Scan for statistical outliers
  bd surprise baseline             Show baseline statistics
  bd surprise history              Show past surprises
  bd surprise add "SQLite uses ?"  Manually flag a surprise`,
}

var surpriseAnalyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Scan for statistical outliers",
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintln(os.Stderr, "Error: not in a beads project")
			os.Exit(1)
		}

		outcomesPath := filepath.Join(beadsDir, "outcomes.jsonl")
		outcomes := loadOutcomes(outcomesPath)

		cyan := color.New(color.FgCyan).SprintFunc()
		yellow := color.New(color.FgYellow).SprintFunc()
		green := color.New(color.FgGreen).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()

		fmt.Printf("\n%s SURPRISE ANALYSIS\n", cyan("═══"))
		fmt.Println("═══════════════════════════════════════════════════════════════════")

		// Calculate duration baseline
		var durations []float64
		for _, o := range outcomes {
			if o.DurationMin != nil && *o.DurationMin > 0 {
				durations = append(durations, float64(*o.DurationMin))
			}
		}

		if len(durations) > 2 {
			mean, stddev := calcStats(durations)

			fmt.Printf("\n%s Duration Outliers (σ=%.1f):\n", yellow("⏱"), surpriseSigma)

			found := 0
			for _, o := range outcomes {
				if o.DurationMin != nil && *o.DurationMin > 0 {
					z := (float64(*o.DurationMin) - mean) / stddev
					if math.Abs(z) > surpriseSigma {
						title := o.Title
						if len(title) > 40 {
							title = title[:37] + "..."
						}

						if z > 0 {
							fmt.Printf("  %s [%s] %dmin (z=+%.1f) - %s\n",
								red("⚠"), o.Issue, *o.DurationMin, z, title)
							fmt.Println("    → Took longer than expected. Worth noting why?")
						} else {
							fmt.Printf("  %s [%s] %dmin (z=%.1f) - %s\n",
								green("★"), o.Issue, *o.DurationMin, z, title)
							fmt.Println("    → Completed faster than usual. Reusable approach?")
						}
						found++
					}
				}
			}

			if found == 0 {
				fmt.Println("  No duration outliers in outcomes")
			}
		} else {
			fmt.Printf("\n%s Duration Outliers:\n", yellow("⏱"))
			fmt.Println("  Not enough duration data for analysis (need 3+)")
		}

		// Pattern markers
		fmt.Printf("\n%s Pattern Markers:\n", yellow("🔍"))
		fmt.Println("  Look for: 'actually', 'wait', 'oops', 'remember'")
		fmt.Println("  These indicate course corrections worth documenting")

		fmt.Println()
		fmt.Println("─────────────────────────────────────────")
		fmt.Println("Use 'bd surprise add <text>' to manually flag surprises")
		fmt.Println()
	},
}

var surpriseBaselineCmd = &cobra.Command{
	Use:   "baseline",
	Short: "Show baseline statistics",
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintln(os.Stderr, "Error: not in a beads project")
			os.Exit(1)
		}

		outcomesPath := filepath.Join(beadsDir, "outcomes.jsonl")
		outcomes := loadOutcomes(outcomesPath)

		cyan := color.New(color.FgCyan).SprintFunc()
		yellow := color.New(color.FgYellow).SprintFunc()

		fmt.Printf("\n%s BASELINE STATISTICS\n", cyan("═══"))
		fmt.Println("═══════════════════════════════════════════════════════════════════")

		// Duration baseline
		var durations []float64
		for _, o := range outcomes {
			if o.DurationMin != nil && *o.DurationMin > 0 {
				durations = append(durations, float64(*o.DurationMin))
			}
		}

		fmt.Printf("\n%s Task Duration (minutes):\n", yellow("⏱"))
		if len(durations) > 0 {
			mean, stddev := calcStats(durations)
			lower := mean - surpriseSigma*stddev
			upper := mean + surpriseSigma*stddev
			if lower < 0 {
				lower = 0
			}

			fmt.Printf("  Mean:        %.1f\n", mean)
			fmt.Printf("  StdDev:      %.1f\n", stddev)
			fmt.Printf("  Sample size: %d\n", len(durations))
			fmt.Printf("  95%% CI:      [%.1f, %.1f]\n", lower, upper)
		} else {
			fmt.Println("  No duration data recorded yet")
			fmt.Println("  Record durations with: bd outcome record <id> --duration <min>")
		}

		// Approach stats
		approachCounts := make(map[string]int)
		for _, o := range outcomes {
			approach := o.Approach
			if approach == "" {
				approach = "unknown"
			}
			approachCounts[approach]++
		}

		fmt.Printf("\n%s Approach Distribution:\n", yellow("📊"))
		for approach, count := range approachCounts {
			fmt.Printf("  %-20s %d\n", approach, count)
		}

		fmt.Printf("\n%s Surprise Markers:\n", yellow("🎯"))
		fmt.Println("  'actually/wait/oops' = Course correction")
		fmt.Println("  'remember/note' = Explicit memory flag")
		fmt.Println("  Error → Fix sequence = Problem solved")
		fmt.Println()
	},
}

var surpriseHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show past surprises",
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintln(os.Stderr, "Error: not in a beads project")
			os.Exit(1)
		}

		surprisePath := filepath.Join(beadsDir, "surprise.jsonl")
		entries := loadSurprises(surprisePath)

		cyan := color.New(color.FgCyan).SprintFunc()

		fmt.Printf("\n%s SURPRISE HISTORY\n", cyan("═══"))
		fmt.Println("═══════════════════════════════════════════════════════════════════")

		if len(entries) == 0 {
			fmt.Println("\n  No surprises logged yet")
			fmt.Println("  Use: bd surprise add \"what surprised you\"")
			fmt.Println()
			return
		}

		for _, e := range entries {
			ts := e.Timestamp
			if len(ts) > 10 {
				ts = ts[:10]
			}
			fmt.Printf("  %s [%s] %s\n", ts, e.Type, e.Content)
		}
		fmt.Println()
	},
}

var surpriseAddCmd = &cobra.Command{
	Use:   "add <text>",
	Short: "Manually flag a surprise",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintln(os.Stderr, "Error: not in a beads project")
			os.Exit(1)
		}

		text := strings.Join(args, " ")
		surprisePath := filepath.Join(beadsDir, "surprise.jsonl")

		entry := SurpriseEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Type:      "manual",
			Content:   text,
		}

		f, err := os.OpenFile(surprisePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()

		data, _ := json.Marshal(entry)
		f.WriteString(string(data) + "\n")

		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("%s Logged surprise: %s\n", green("✓"), text)
	},
}

func init() {
	surpriseAnalyzeCmd.Flags().Float64VarP(&surpriseSigma, "sigma", "s", 2.0, "Sigma threshold (default 2.0 = 95% CI)")
	surpriseBaselineCmd.Flags().Float64VarP(&surpriseSigma, "sigma", "s", 2.0, "Sigma threshold (default 2.0 = 95% CI)")

	surpriseCmd.AddCommand(surpriseAnalyzeCmd)
	surpriseCmd.AddCommand(surpriseBaselineCmd)
	surpriseCmd.AddCommand(surpriseHistoryCmd)
	surpriseCmd.AddCommand(surpriseAddCmd)

	rootCmd.AddCommand(surpriseCmd)
}

func calcStats(values []float64) (mean, stddev float64) {
	if len(values) == 0 {
		return 0, 0
	}

	// Calculate mean
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean = sum / float64(len(values))

	// Calculate standard deviation
	sumSq := 0.0
	for _, v := range values {
		diff := v - mean
		sumSq += diff * diff
	}
	variance := sumSq / float64(len(values))
	stddev = math.Sqrt(variance)

	return mean, stddev
}

func loadSurprises(path string) []SurpriseEntry {
	var entries []SurpriseEntry

	f, err := os.Open(path)
	if err != nil {
		return entries
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var e SurpriseEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			entries = append(entries, e)
		}
	}

	return entries
}
