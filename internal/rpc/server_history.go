package rpc

import (
	"encoding/json"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
)

// handleHistoryIssue returns the complete version history for an issue.
func (s *Server) handleHistoryIssue(req *Request) Response {
	var args HistoryIssueArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid args: %v", err)}
	}

	if args.IssueID == "" {
		return Response{Success: false, Error: "issue_id is required"}
	}

	vs, ok := storage.AsVersioned(s.storage)
	if !ok {
		return Response{Success: false, Error: "history operations require Dolt backend (versioned storage not available)"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	entries, err := vs.History(ctx, args.IssueID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to get issue history: %v", err)}
	}

	rpcEntries := make([]HistoryEntryRPC, len(entries))
	for i, e := range entries {
		rpcEntries[i] = HistoryEntryRPC{
			CommitHash: e.CommitHash,
			Committer:  e.Committer,
			CommitDate: e.CommitDate,
			Issue:      e.Issue,
		}
	}

	result := HistoryIssueResult{Entries: rpcEntries}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleHistoryDiff returns low-level table-level diffs between two commits.
// Uses UnderlyingDB() directly since GetDiff is a DoltStore-specific method
// not exposed on the VersionedStorage interface.
func (s *Server) handleHistoryDiff(req *Request) Response {
	var args HistoryDiffArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid args: %v", err)}
	}

	if args.FromRef == "" || args.ToRef == "" {
		return Response{Success: false, Error: "from_ref and to_ref are required"}
	}

	if !storage.IsVersioned(s.storage) {
		return Response{Success: false, Error: "history operations require Dolt backend (versioned storage not available)"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	db := s.storage.UnderlyingDB()
	if db == nil {
		return Response{Success: false, Error: "database not available for history diff"}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT table_name, diff_type, from_commit, to_commit
		FROM dolt_diff(?, ?)
	`, args.FromRef, args.ToRef)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to get diff: %v", err)}
	}
	defer rows.Close()

	var entries []HistoryDiffEntryRPC
	for rows.Next() {
		var e HistoryDiffEntryRPC
		if err := rows.Scan(&e.TableName, &e.DiffType, &e.FromCommit, &e.ToCommit); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to scan diff entry: %v", err)}
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to iterate diff rows: %v", err)}
	}

	result := HistoryDiffResult{Entries: entries}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleHistoryIssueDiff returns detailed changes to a specific issue between two commits.
// Uses UnderlyingDB() directly since GetIssueDiff is a DoltStore-specific method
// not exposed on the VersionedStorage interface.
func (s *Server) handleHistoryIssueDiff(req *Request) Response {
	var args HistoryIssueDiffArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid args: %v", err)}
	}

	if args.IssueID == "" || args.FromRef == "" || args.ToRef == "" {
		return Response{Success: false, Error: "issue_id, from_ref, and to_ref are required"}
	}

	if !storage.IsVersioned(s.storage) {
		return Response{Success: false, Error: "history operations require Dolt backend (versioned storage not available)"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	db := s.storage.UnderlyingDB()
	if db == nil {
		return Response{Success: false, Error: "database not available for issue diff"}
	}

	// Validate refs (basic alphanumeric + dash + underscore check)
	for _, ref := range []string{args.FromRef, args.ToRef} {
		for _, c := range ref {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
				return Response{Success: false, Error: fmt.Sprintf("invalid ref format: %s", ref)}
			}
		}
	}

	// nolint:gosec // G201: refs validated above
	query := fmt.Sprintf(`
		SELECT
			from_id, to_id,
			from_title, to_title,
			from_status, to_status,
			from_description, to_description,
			diff_type
		FROM dolt_diff('%s', '%s', 'issues')
		WHERE from_id = ? OR to_id = ?
	`, args.FromRef, args.ToRef)

	var result HistoryIssueDiffResult
	var fromID, toID, fromTitle, toTitle, fromStatus, toStatus *string
	var fromDesc, toDesc *string

	err := db.QueryRowContext(ctx, query, args.IssueID, args.IssueID).Scan(
		&fromID, &toID,
		&fromTitle, &toTitle,
		&fromStatus, &toStatus,
		&fromDesc, &toDesc,
		&result.DiffType,
	)

	if err != nil {
		// sql.ErrNoRows means the issue wasn't in the diff
		if err.Error() == "sql: no rows in result set" {
			result.Found = false
			data, _ := json.Marshal(result)
			return Response{Success: true, Data: data}
		}
		return Response{Success: false, Error: fmt.Sprintf("failed to get issue diff: %v", err)}
	}

	result.Found = true
	if fromID != nil {
		result.FromID = *fromID
	}
	if toID != nil {
		result.ToID = *toID
	}
	if fromTitle != nil {
		result.FromTitle = *fromTitle
	}
	if toTitle != nil {
		result.ToTitle = *toTitle
	}
	if fromStatus != nil {
		result.FromStatus = *fromStatus
	}
	if toStatus != nil {
		result.ToStatus = *toStatus
	}
	if fromDesc != nil {
		result.FromDescription = *fromDesc
	}
	if toDesc != nil {
		result.ToDescription = *toDesc
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleHistoryConflicts returns any merge conflicts in the current state.
func (s *Server) handleHistoryConflicts(req *Request) Response {
	if !storage.IsVersioned(s.storage) {
		return Response{Success: false, Error: "history operations require Dolt backend (versioned storage not available)"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Use underlying DB for internal conflict details (table-level with num_conflicts).
	// VersionedStorage.GetConflicts() only returns Conflict{Field} without counts.
	db := s.storage.UnderlyingDB()
	if db == nil {
		return Response{Success: false, Error: "database not available for conflict query"}
	}

	rows, err := db.QueryContext(ctx, `SELECT table_name, num_conflicts FROM dolt_conflicts`)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to get conflicts: %v", err)}
	}
	defer rows.Close()

	var conflicts []HistoryConflictRPC
	for rows.Next() {
		var c HistoryConflictRPC
		if err := rows.Scan(&c.TableName, &c.NumConflicts); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to scan conflict: %v", err)}
		}
		conflicts = append(conflicts, c)
	}
	if err := rows.Err(); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to iterate conflict rows: %v", err)}
	}

	result := HistoryConflictsResult{Conflicts: conflicts}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleHistoryResolveConflicts resolves merge conflicts using the specified strategy.
func (s *Server) handleHistoryResolveConflicts(req *Request) Response {
	var args HistoryResolveConflictsArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid args: %v", err)}
	}

	if args.Table == "" {
		return Response{Success: false, Error: "table is required"}
	}
	if args.Strategy != "ours" && args.Strategy != "theirs" {
		return Response{Success: false, Error: "strategy must be 'ours' or 'theirs'"}
	}

	vs, ok := storage.AsVersioned(s.storage)
	if !ok {
		return Response{Success: false, Error: "history operations require Dolt backend (versioned storage not available)"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := vs.ResolveConflicts(ctx, args.Table, args.Strategy); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to resolve conflicts: %v", err)}
	}

	result := HistoryResolveConflictsResult{Resolved: true}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleVersionedDiff returns issue-level diffs with full Issue data between two commits.
func (s *Server) handleVersionedDiff(req *Request) Response {
	var args VersionedDiffArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid args: %v", err)}
	}

	if args.FromRef == "" || args.ToRef == "" {
		return Response{Success: false, Error: "from_ref and to_ref are required"}
	}

	vs, ok := storage.AsVersioned(s.storage)
	if !ok {
		return Response{Success: false, Error: "history operations require Dolt backend (versioned storage not available)"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	entries, err := vs.Diff(ctx, args.FromRef, args.ToRef)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to get versioned diff: %v", err)}
	}

	rpcEntries := make([]VersionedDiffEntryRPC, len(entries))
	for i, e := range entries {
		rpcEntries[i] = VersionedDiffEntryRPC{
			IssueID:  e.IssueID,
			DiffType: e.DiffType,
			OldValue: e.OldValue,
			NewValue: e.NewValue,
		}
	}

	result := VersionedDiffResult{Entries: rpcEntries}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}
