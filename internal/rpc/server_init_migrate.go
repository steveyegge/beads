// Package rpc provides RPC server handlers for init and migrate operations.
// These handlers enable bd init and bd migrate to work via daemon RPC.
package rpc

import (
	"encoding/json"
	"fmt"
)

// handleInit handles the init RPC operation for remote database initialization.
// This creates a new beads database, sets the issue prefix, and optionally imports from JSONL.
//
// NOTE: This is a stub implementation. The full init logic involves complex filesystem
// operations and git integration that are better suited to run locally. This RPC endpoint
// exists primarily for remote daemon scenarios where the daemon needs to re-initialize
// its own database.
func (s *Server) handleInit(req *Request) Response {
	var args InitArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid init arguments: %v", err)}
		}
	}

	// The daemon cannot perform full init operations as they require:
	// 1. Access to the filesystem to create .beads/ directory
	// 2. Git operations for importing from history
	// 3. Config file creation
	//
	// For now, return an error indicating this operation should be done locally.
	// Future: Support lightweight re-initialization for daemon restart scenarios.
	return Response{
		Success: false,
		Error:   "init via RPC is not supported; run 'bd init' locally instead",
	}
}

// handleMigrate handles the migrate RPC operation for remote database migration.
// This detects schema versions, migrates old databases, and updates version metadata.
//
// NOTE: This is a stub implementation. Migration operations are complex and involve
// filesystem operations that are better suited to run locally. This RPC endpoint
// exists for future scenarios where the daemon needs to self-migrate.
func (s *Server) handleMigrate(req *Request) Response {
	var args MigrateArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid migrate arguments: %v", err)}
		}
	}

	// For --inspect, we can return some useful information about the current database
	if args.Inspect {
		return s.handleMigrateInspect(req)
	}

	// The daemon cannot perform full migration operations as they require:
	// 1. Database file detection and renaming
	// 2. Multiple database connections for migration
	// 3. Backup creation before migration
	//
	// For now, return an error indicating this operation should be done locally.
	return Response{
		Success: false,
		Error:   "migrate via RPC is not supported; run 'bd migrate' locally instead",
	}
}

// handleMigrateInspect handles the migrate --inspect RPC operation.
// This returns information about the current database state without making changes.
func (s *Server) handleMigrateInspect(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()
	store := s.storage

	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	// Get schema version from metadata
	version, err := store.GetMetadata(ctx, "bd_version")
	if err != nil {
		version = "unknown"
	}

	// Get issue count
	issueCount := 0
	if stats, err := store.GetStatistics(ctx); err == nil {
		issueCount = stats.TotalIssues
	}

	// Get config
	prefix, _ := store.GetConfig(ctx, "issue_prefix")

	result := MigrateResult{
		Status:          "success",
		CurrentDatabase: s.dbPath,
		Version:         version,
		Message:         fmt.Sprintf("Database at %s, version %s, %d issues, prefix: %s", s.dbPath, version, issueCount, prefix),
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}
