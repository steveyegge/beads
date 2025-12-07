package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads"
)

// UsageEntry represents a single command usage event
type UsageEntry struct {
	Timestamp string   `json:"ts"`
	Command   string   `json:"cmd"`
	Subcommand string  `json:"subcmd,omitempty"`
	Args      []string `json:"args,omitempty"`
}

// TrackUsage logs command usage to .beads/usage.jsonl
func TrackUsage(cmd string, subcmd string, args []string) {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return // Not in a beads project, skip tracking
	}

	usagePath := filepath.Join(beadsDir, "usage.jsonl")

	// Filter out sensitive args (anything that looks like a path or long text)
	var safeArgs []string
	for _, arg := range args {
		// Skip flags values, keep flag names
		if strings.HasPrefix(arg, "--") || strings.HasPrefix(arg, "-") {
			safeArgs = append(safeArgs, arg)
		} else if len(arg) < 30 && !strings.Contains(arg, "/") {
			// Keep short args that aren't paths
			safeArgs = append(safeArgs, arg)
		}
	}

	entry := UsageEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Command:    cmd,
		Subcommand: subcmd,
		Args:       safeArgs,
	}

	f, err := os.OpenFile(usagePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return // Silent fail - don't break commands for tracking
	}
	defer f.Close()

	data, _ := json.Marshal(entry)
	f.WriteString(string(data) + "\n")
}
