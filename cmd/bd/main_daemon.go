package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cmdImport = "import"
)

// Command group IDs for help organization
const (
	GroupMaintenance  = "maintenance"
	GroupIntegrations = "integrations"
)

// signalOrchestratorActivity writes an activity signal for orchestrator daemon.
// This enables exponential backoff based on bd usage detection.
// Best-effort: silent on any failure, never affects bd operation.
func signalOrchestratorActivity() {
	// Determine town root from environment
	// Priority: GT_ROOT env > skip (no default path detection)
	townRoot := os.Getenv("GT_ROOT")
	if townRoot == "" {
		return // Not in orchestrator environment, skip
	}

	// Ensure daemon directory exists
	daemonDir := filepath.Join(townRoot, "daemon")
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		return
	}

	// Build command line from os.Args
	cmdLine := strings.Join(os.Args, " ")

	// Determine actor (uses git config user.name as default)
	actorName := getActorWithGit()

	// Build activity signal
	activity := struct {
		LastCommand string `json:"last_command"`
		Actor       string `json:"actor"`
		Timestamp   string `json:"timestamp"`
	}{
		LastCommand: cmdLine,
		Actor:       actorName,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(activity)
	if err != nil {
		return
	}

	// Write atomically (write to temp, rename)
	activityPath := filepath.Join(daemonDir, "activity.json")
	tmpPath := activityPath + ".tmp"
	// nolint:gosec // G306: 0644 is appropriate for a status file
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, activityPath)
}
