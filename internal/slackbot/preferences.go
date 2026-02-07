// Package slackbot provides a Slack bot integration for beads issue tracking.
package slackbot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UserPrefs stores per-user notification preferences.
type UserPrefs struct {
	DMOptIn             bool      `json:"dm_opt_in"`             // Whether user has opted in to DMs
	NotificationLevel   string    `json:"notification_level"`    // all/high/muted
	ThreadNotifications bool      `json:"thread_notifications"`  // Whether to notify on thread replies
	UpdatedAt           time.Time `json:"updated_at"`            // Last update timestamp
}

// DefaultUserPrefs returns the default (safe, opt-out) preferences.
func DefaultUserPrefs() UserPrefs {
	return UserPrefs{
		DMOptIn:             false,  // Opt-out by default
		NotificationLevel:   "high", // Only high-priority by default
		ThreadNotifications: false,  // No thread notifications by default
		UpdatedAt:           time.Now(),
	}
}

// ValidNotificationLevels contains the allowed notification level values.
var ValidNotificationLevels = []string{"all", "high", "muted"}

// IsValidNotificationLevel checks if a level is valid.
func IsValidNotificationLevel(level string) bool {
	for _, l := range ValidNotificationLevels {
		if l == level {
			return true
		}
	}
	return false
}

// PreferenceManager manages per-user Slack notification preferences.
type PreferenceManager struct {
	mu       sync.RWMutex
	prefs    map[string]UserPrefs // userID -> preferences
	filePath string               // Path to persistence file
}

// NewPreferenceManager creates a new PreferenceManager.
// If beadsDir is empty, uses BEADS_DIR environment variable or falls back to
// the current directory.
func NewPreferenceManager(beadsDir string) *PreferenceManager {
	if beadsDir == "" {
		beadsDir = os.Getenv("BEADS_DIR")
	}
	if beadsDir == "" {
		beadsDir = "."
	}

	// Store preferences alongside the beads dir in a sibling settings directory.
	filePath := filepath.Join(beadsDir, "..", "settings", "slack_user_prefs.json")
	if beadsDir == "." {
		filePath = filepath.Join("settings", "slack_user_prefs.json")
	}

	pm := &PreferenceManager{
		prefs:    make(map[string]UserPrefs),
		filePath: filePath,
	}

	// Attempt to load existing preferences (ignore errors for new installations)
	_ = pm.Load()

	return pm
}

// GetUserPreferences returns the preferences for a user.
// Returns default preferences if the user has no stored preferences.
func (pm *PreferenceManager) GetUserPreferences(userID string) UserPrefs {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if prefs, ok := pm.prefs[userID]; ok {
		return prefs
	}
	return DefaultUserPrefs()
}

// SetDMOptIn sets the DM opt-in preference for a user.
func (pm *PreferenceManager) SetDMOptIn(userID string, enabled bool) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	prefs := pm.getOrDefaultLocked(userID)
	prefs.DMOptIn = enabled
	prefs.UpdatedAt = time.Now()
	pm.prefs[userID] = prefs

	return nil
}

// SetNotificationLevel sets the notification level for a user.
// Valid levels: "all", "high", "muted".
func (pm *PreferenceManager) SetNotificationLevel(userID, level string) error {
	if !IsValidNotificationLevel(level) {
		return fmt.Errorf("invalid notification level %q: must be one of %v", level, ValidNotificationLevels)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	prefs := pm.getOrDefaultLocked(userID)
	prefs.NotificationLevel = level
	prefs.UpdatedAt = time.Now()
	pm.prefs[userID] = prefs

	return nil
}

// SetThreadNotifications sets the thread notification preference for a user.
func (pm *PreferenceManager) SetThreadNotifications(userID string, enabled bool) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	prefs := pm.getOrDefaultLocked(userID)
	prefs.ThreadNotifications = enabled
	prefs.UpdatedAt = time.Now()
	pm.prefs[userID] = prefs

	return nil
}

// IsEligibleForDM returns whether a user is eligible to receive DMs.
// A user is eligible if:
//   - They have opted in to DMs (DMOptIn = true)
//   - Their notification level is not "muted"
func (pm *PreferenceManager) IsEligibleForDM(userID string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	prefs, ok := pm.prefs[userID]
	if !ok {
		// Default is opt-out, so not eligible
		return false
	}

	return prefs.DMOptIn && prefs.NotificationLevel != "muted"
}

// getOrDefaultLocked returns existing prefs or creates default (must hold lock).
func (pm *PreferenceManager) getOrDefaultLocked(userID string) UserPrefs {
	if prefs, ok := pm.prefs[userID]; ok {
		return prefs
	}
	return DefaultUserPrefs()
}

// Save persists preferences to the JSON file.
// Uses atomic write (write to temp file, then rename) to prevent corruption.
func (pm *PreferenceManager) Save() error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Ensure settings directory exists
	dir := filepath.Dir(pm.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(pm.prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal preferences: %w", err)
	}

	// Write to temp file first
	tmpPath := pm.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, pm.filePath); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// Load reads preferences from the JSON file.
// Returns nil if file doesn't exist (fresh installation).
func (pm *PreferenceManager) Load() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	data, err := os.ReadFile(pm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Fresh installation, no preferences yet
			return nil
		}
		return fmt.Errorf("read preferences file: %w", err)
	}

	var prefs map[string]UserPrefs
	if err := json.Unmarshal(data, &prefs); err != nil {
		return fmt.Errorf("parse preferences file: %w", err)
	}

	pm.prefs = prefs
	return nil
}

// GetFilePath returns the path to the preferences file.
// Useful for debugging and testing.
func (pm *PreferenceManager) GetFilePath() string {
	return pm.filePath
}

// UserCount returns the number of users with stored preferences.
func (pm *PreferenceManager) UserCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.prefs)
}

// ListUsers returns all user IDs with stored preferences.
func (pm *PreferenceManager) ListUsers() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	users := make([]string, 0, len(pm.prefs))
	for userID := range pm.prefs {
		users = append(users, userID)
	}
	return users
}

// ClearUserPreferences removes all preferences for a user.
func (pm *PreferenceManager) ClearUserPreferences(userID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.prefs, userID)
}
