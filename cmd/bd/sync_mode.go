package main

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
)

// Sync mode constants define how beads synchronizes data with git.
const (
	// SyncModeGitPortable exports to JSONL on push, imports on pull.
	// This is the default mode - works with standard git workflows.
	SyncModeGitPortable = "git-portable"

	// SyncModeRealtime exports to JSONL on every database mutation.
	// Provides immediate persistence but more git noise.
	SyncModeRealtime = "realtime"

	// SyncModeDoltNative uses Dolt remotes for sync, skipping JSONL.
	// Requires Dolt backend and configured Dolt remote.
	SyncModeDoltNative = "dolt-native"

	// SyncModeBeltAndSuspenders uses both Dolt remotes AND JSONL.
	// Maximum redundancy - Dolt for versioning, JSONL for git portability.
	SyncModeBeltAndSuspenders = "belt-and-suspenders"

	// SyncModeConfigKey is the database config key for sync mode.
	SyncModeConfigKey = "sync.mode"

	// SyncExportOnConfigKey controls when JSONL export happens.
	SyncExportOnConfigKey = "sync.export_on"

	// SyncImportOnConfigKey controls when JSONL import happens.
	SyncImportOnConfigKey = "sync.import_on"

	// SyncAutoDoltCommitKey controls automatic Dolt commits after sync.
	// When enabled (default), data changes are committed to Dolt after each sync.
	SyncAutoDoltCommitKey = "sync.auto_dolt_commit"

	// SyncAutoDoltPushKey controls automatic Dolt push after sync.
	// When enabled, commits are pushed to the Dolt remote after each sync.
	SyncAutoDoltPushKey = "sync.auto_dolt_push"
)

// Trigger constants for export_on and import_on settings.
const (
	// TriggerPush triggers on git push (export) or git pull (import).
	TriggerPush = "push"
	TriggerPull = "pull"

	// TriggerChange triggers on every database mutation (realtime mode).
	TriggerChange = "change"
)

// GetSyncMode returns the configured sync mode, defaulting to git-portable.
// Reads from config.yaml first (sync.mode is a yaml-only key), then falls back
// to database config for backwards compatibility and testing.
func GetSyncMode(ctx context.Context, s storage.Storage) string {
	// Try yaml config first (preferred source)
	mode := config.GetSyncMode()

	// Fall back to database config for backwards compatibility
	if mode == "" && s != nil {
		mode, _ = s.GetConfig(ctx, SyncModeConfigKey)
	}

	if mode == "" {
		return SyncModeGitPortable
	}

	// Validate mode
	switch mode {
	case SyncModeGitPortable, SyncModeRealtime, SyncModeDoltNative, SyncModeBeltAndSuspenders:
		return mode
	default:
		// Invalid mode, return default
		return SyncModeGitPortable
	}
}

// SetSyncMode sets the sync mode configuration.
func SetSyncMode(ctx context.Context, s storage.Storage, mode string) error {
	// Validate mode
	switch mode {
	case SyncModeGitPortable, SyncModeRealtime, SyncModeDoltNative, SyncModeBeltAndSuspenders:
		// Valid
	default:
		return fmt.Errorf("invalid sync mode: %s (valid: %s, %s, %s, %s)",
			mode, SyncModeGitPortable, SyncModeRealtime, SyncModeDoltNative, SyncModeBeltAndSuspenders)
	}

	return s.SetConfig(ctx, SyncModeConfigKey, mode)
}

// GetExportTrigger returns when JSONL export should happen.
func GetExportTrigger(ctx context.Context, s storage.Storage) string {
	trigger, err := s.GetConfig(ctx, SyncExportOnConfigKey)
	if err != nil || trigger == "" {
		// Default based on sync mode
		mode := GetSyncMode(ctx, s)
		if mode == SyncModeRealtime {
			return TriggerChange
		}
		return TriggerPush
	}
	return trigger
}

// GetImportTrigger returns when JSONL import should happen.
func GetImportTrigger(ctx context.Context, s storage.Storage) string {
	trigger, err := s.GetConfig(ctx, SyncImportOnConfigKey)
	if err != nil || trigger == "" {
		return TriggerPull
	}
	return trigger
}

// ShouldExportJSONL returns true if the current sync mode uses JSONL export.
func ShouldExportJSONL(ctx context.Context, s storage.Storage) bool {
	mode := GetSyncMode(ctx, s)
	// All modes except dolt-native use JSONL
	return mode != SyncModeDoltNative
}

// ShouldUseDoltRemote returns true if the current sync mode uses Dolt remotes.
func ShouldUseDoltRemote(ctx context.Context, s storage.Storage) bool {
	mode := GetSyncMode(ctx, s)
	return mode == SyncModeDoltNative || mode == SyncModeBeltAndSuspenders
}

// ShouldAutoDoltCommit returns true if Dolt commits should happen automatically after sync.
// Defaults to true to prevent journal corruption.
func ShouldAutoDoltCommit(ctx context.Context, s storage.Storage) bool {
	val, err := s.GetConfig(ctx, SyncAutoDoltCommitKey)
	if err != nil || val == "" {
		return true // Default: enabled
	}
	return val == "true" || val == "1" || val == "yes"
}

// ShouldAutoDoltPush returns true if Dolt pushes should happen automatically after sync.
// Defaults to false (requires explicit configuration).
func ShouldAutoDoltPush(ctx context.Context, s storage.Storage) bool {
	val, err := s.GetConfig(ctx, SyncAutoDoltPushKey)
	if err != nil || val == "" {
		return false // Default: disabled
	}
	return val == "true" || val == "1" || val == "yes"
}

// SyncModeDescription returns a human-readable description of the sync mode.
func SyncModeDescription(mode string) string {
	switch mode {
	case SyncModeGitPortable:
		return "JSONL exported on push, imported on pull"
	case SyncModeRealtime:
		return "JSONL exported on every change"
	case SyncModeDoltNative:
		return "Dolt remotes only, no JSONL"
	case SyncModeBeltAndSuspenders:
		return "Both Dolt remotes and JSONL"
	default:
		return "unknown mode"
	}
}
