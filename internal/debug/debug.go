package debug

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	enabled     = os.Getenv("BD_DEBUG") != ""
	verboseMode = false
	quietMode   = false
	logMutex    sync.Mutex
)

func Enabled() bool {
	return enabled || verboseMode
}

// SetVerbose enables verbose/debug output
func SetVerbose(verbose bool) {
	verboseMode = verbose
}

// SetQuiet enables quiet mode (suppress non-essential output)
func SetQuiet(quiet bool) {
	quietMode = quiet
}

// IsQuiet returns true if quiet mode is enabled
func IsQuiet() bool {
	return quietMode
}

func Logf(format string, args ...interface{}) {
	if enabled || verboseMode {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

func Printf(format string, args ...interface{}) {
	if enabled || verboseMode {
		fmt.Printf(format, args...)
	}
}

// PrintNormal prints output unless quiet mode is enabled
// Use this for normal informational output that should be suppressed in quiet mode
func PrintNormal(format string, args ...interface{}) {
	if !quietMode {
		fmt.Printf(format, args...)
	}
}

// PrintlnNormal prints a line unless quiet mode is enabled
func PrintlnNormal(args ...interface{}) {
	if !quietMode {
		fmt.Println(args...)
	}
}

// LogEvent writes an event to .beads/events.log
// Format: TIMESTAMP|EVENT_CODE|ISSUE_ID|AGENT_ID|SESSION_ID|DETAILS
func LogEvent(eventCode, issueID, details string) {
	LogEventWithContext(eventCode, issueID, "", "", details)
}

// LogEventWithContext writes an event with full context
func LogEventWithContext(eventCode, issueID, agentID, sessionID, details string) {
	// Find project root
	projectRoot, err := findProjectRoot()
	if err != nil {
		// Silent fail if not in a project
		return
	}

	logPath := filepath.Join(projectRoot, ".beads", "events.log")

	// Default values
	if issueID == "" {
		issueID = "none"
	}
	if agentID == "" {
		agentID = os.Getenv("BEADS_AGENT_ID")
		if agentID == "" {
			agentID = os.Getenv("USER")
			if agentID == "" {
				agentID = "unknown"
			}
		}
	}
	if sessionID == "" {
		sessionID = os.Getenv("BEADS_SESSION_ID")
		if sessionID == "" {
			sessionID = fmt.Sprintf("%d", time.Now().Unix())
		}
	}

	// Format event
	timestamp := time.Now().UTC().Format(time.RFC3339)
	entry := fmt.Sprintf("%s|%s|%s|%s|%s|%s\n",
		timestamp, eventCode, issueID, agentID, sessionID, details)

	// Thread-safe write
	logMutex.Lock()
	defer logMutex.Unlock()

	// Ensure directory exists
	os.MkdirAll(filepath.Dir(logPath), 0755)

	// Append to log file
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// Silent fail - don't interrupt operations if logging fails
		return
	}
	defer file.Close()

	file.WriteString(entry)
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
