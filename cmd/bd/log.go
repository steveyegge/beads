package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	logSession  string
	logIssue    string
	logCategory string
	logSince    string
	logUntil    string
	logLast     int
	logSummary  bool
	logFollow   bool
)

// Event represents a single log entry
type Event struct {
	Timestamp string `json:"timestamp"`
	EventCode string `json:"event_code"`
	IssueID   string `json:"issue_id"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Details   string `json:"details"`
}

// EventSummary represents aggregated event statistics
type EventSummary struct {
	EventCode string `json:"event_code"`
	Count     int    `json:"count"`
}

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Query and filter event logs",
	Long: `Query and filter events from .beads/events.log

Event logs track all beads operations including CLI commands, git hooks,
VS Code skill activations, and agent sessions. Events follow the taxonomy
defined in vscode/events/EVENT_TAXONOMY.md.

Examples:
  # Show last 10 events
  bd log

  # Show last 50 events
  bd log --last 50

  # Filter by session
  bd log --session 1733929442

  # Filter by issue
  bd log --issue bd-0001

  # Filter by category (ep, ss, sk, bd, gt, hk, gd)
  bd log --category sk

  # Show events since timestamp
  bd log --since 2024-12-11T15:00:00Z

  # Show summary statistics
  bd log --summary

  # Output as JSON
  bd log --json

  # Follow log in real-time (like tail -f)
  bd log --follow`,
	Run: runLog,
}

func init() {
	logCmd.Flags().StringVar(&logSession, "session", "", "Filter by session ID")
	logCmd.Flags().StringVar(&logIssue, "issue", "", "Filter by issue ID")
	logCmd.Flags().StringVar(&logCategory, "category", "", "Filter by event category (ep, ss, sk, bd, gt, hk, gd)")
	logCmd.Flags().StringVar(&logSince, "since", "", "Show events since timestamp (RFC3339 format)")
	logCmd.Flags().StringVar(&logUntil, "until", "", "Show events until timestamp (RFC3339 format)")
	logCmd.Flags().IntVar(&logLast, "last", 10, "Show last N events (0 for all)")
	logCmd.Flags().BoolVar(&logSummary, "summary", false, "Show event summary statistics")
	logCmd.Flags().BoolVar(&logFollow, "follow", false, "Follow log output (like tail -f)")
	rootCmd.AddCommand(logCmd)
}

func runLog(cmd *cobra.Command, args []string) {
	// Find project root
	projectRoot, err := findProjectRoot()
	if err != nil {
		FatalError("Not in a beads project (no .beads directory found)")
	}

	logPath := filepath.Join(projectRoot, ".beads", "events.log")

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Println("No events logged yet")
		}
		return
	}

	// Parse time filters
	var sinceTime, untilTime time.Time
	if logSince != "" {
		sinceTime, err = time.Parse(time.RFC3339, logSince)
		if err != nil {
			FatalError("Invalid --since format: %v (use RFC3339, e.g., 2024-12-11T15:00:00Z)", err)
		}
	}
	if logUntil != "" {
		untilTime, err = time.Parse(time.RFC3339, logUntil)
		if err != nil {
			FatalError("Invalid --until format: %v (use RFC3339, e.g., 2024-12-11T15:00:00Z)", err)
		}
	}

	// Read and filter events
	events, err := readEvents(logPath, sinceTime, untilTime)
	if err != nil {
		FatalError("Failed to read event log: %v", err)
	}

	// Apply filters
	events = filterLogEvents(events)

	// Handle summary mode
	if logSummary {
		showSummary(events)
		return
	}

	// Handle follow mode
	if logFollow {
		followLog(logPath)
		return
	}

	// Apply last N filter
	if logLast > 0 && len(events) > logLast {
		events = events[len(events)-logLast:]
	}

	// Output
	if jsonOutput {
		outputJSON(events)
	} else {
		for _, event := range events {
			printLogEvent(event)
		}
	}
}

func readEvents(logPath string, sinceTime, untilTime time.Time) ([]Event, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var events []Event
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		event, err := parseEvent(line)
		if err != nil {
			continue // Skip malformed lines
		}

		// Apply time filters
		if !sinceTime.IsZero() || !untilTime.IsZero() {
			eventTime, err := time.Parse(time.RFC3339, event.Timestamp)
			if err != nil {
				continue
			}
			if !sinceTime.IsZero() && eventTime.Before(sinceTime) {
				continue
			}
			if !untilTime.IsZero() && eventTime.After(untilTime) {
				continue
			}
		}

		events = append(events, event)
	}

	return events, scanner.Err()
}

func parseEvent(line string) (Event, error) {
	// Format: TIMESTAMP|EVENT_CODE|ISSUE_ID|AGENT_ID|SESSION_ID|DETAILS
	parts := strings.SplitN(line, "|", 6)
	if len(parts) < 5 {
		return Event{}, fmt.Errorf("invalid log line format")
	}

	event := Event{
		Timestamp: parts[0],
		EventCode: parts[1],
		IssueID:   parts[2],
		AgentID:   parts[3],
		SessionID: parts[4],
	}
	if len(parts) == 6 {
		event.Details = parts[5]
	}

	return event, nil
}

func filterLogEvents(events []Event) []Event {
	var filtered []Event

	for _, event := range events {
		// Session filter
		if logSession != "" && event.SessionID != logSession {
			continue
		}

		// Issue filter
		if logIssue != "" && event.IssueID != logIssue {
			continue
		}

		// Category filter
		if logCategory != "" && !strings.HasPrefix(event.EventCode, logCategory+".") {
			continue
		}

		filtered = append(filtered, event)
	}

	return filtered
}

func printLogEvent(event Event) {
	// Color-coded output based on category
	category := strings.Split(event.EventCode, ".")[0]

	var color string
	switch category {
	case "ep": // Epoch - cyan
		color = "\033[36m"
	case "ss": // Session - green
		color = "\033[32m"
	case "sk": // Skill - yellow
		color = "\033[33m"
	case "bd": // Beads - blue
		color = "\033[34m"
	case "gt": // Git - magenta
		color = "\033[35m"
	case "hk": // Hook - red
		color = "\033[31m"
	case "gd": // Guard - bright red
		color = "\033[91m"
	default:
		color = "\033[0m"
	}
	reset := "\033[0m"

	fmt.Printf("%s[%s]%s %s%-20s%s | %s%-10s%s | %s\n",
		"\033[90m", event.Timestamp, reset, // gray timestamp
		color, event.EventCode, reset,      // colored event code
		"\033[96m", event.IssueID, reset,   // cyan issue ID
		event.Details,
	)
}

func showSummary(events []Event) {
	// Count events by code
	counts := make(map[string]int)
	for _, event := range events {
		counts[event.EventCode]++
	}

	// Convert to sorted slice
	var summary []EventSummary
	for code, count := range counts {
		summary = append(summary, EventSummary{
			EventCode: code,
			Count:     count,
		})
	}
	sort.Slice(summary, func(i, j int) bool {
		return summary[i].Count > summary[j].Count
	})

	if jsonOutput {
		outputJSON(summary)
	} else {
		fmt.Printf("Total events: %d\n\n", len(events))
		fmt.Printf("%-40s %s\n", "EVENT CODE", "COUNT")
		fmt.Println(strings.Repeat("-", 50))
		for _, s := range summary {
			fmt.Printf("%-40s %d\n", s.EventCode, s.Count)
		}
	}
}

func followLog(logPath string) {
	file, err := os.Open(logPath)
	if err != nil {
		FatalError("Failed to open log file: %v", err)
	}
	defer file.Close()

	// Seek to end
	file.Seek(0, 2)

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		event, err := parseEvent(strings.TrimSpace(line))
		if err != nil {
			continue
		}

		// Apply filters
		filtered := filterLogEvents([]Event{event})
		if len(filtered) > 0 {
			printLogEvent(filtered[0])
		}
	}
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		beadsDir := filepath.Join(dir, ".beads")
		if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a beads project")
		}
		dir = parent
	}
}
