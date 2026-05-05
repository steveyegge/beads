package postgres

import "context"

// isActiveWispID reports whether issueID names a row in the wisps table.
// Used by tx-scoped mutations to route writes to the wisp_* counterparts so
// labels/dependencies/comments/events all land in the correct table.
//
// Mirrors issueops.IsActiveWispInTx for the Dolt backend. A "true" result is
// authoritative: wisps and issues use disjoint ID generators, so a wisp ID
// never collides with a persistent issue ID. Any error from the SELECT is
// treated as "not a wisp" (the row genuinely does not exist), matching the
// Dolt-side fallback.
func isActiveWispID(ctx context.Context, c pgxConn, issueID string) bool {
	var dummy int
	err := c.QueryRow(ctx, "SELECT 1 FROM wisps WHERE id = $1 LIMIT 1", issueID).Scan(&dummy)
	return err == nil
}

// commentTablesForID picks the (commentTable, eventTable) pair for issueID
// based on whether it is an active wisp. Used by AddIssueComment and the
// tx-scoped AddComment so wisp-targeted writes land in wisp_comments /
// wisp_events instead of failing FK constraints.
func commentTablesForID(ctx context.Context, c pgxConn, issueID string) (commentTable, eventTable string) {
	if isActiveWispID(ctx, c, issueID) {
		return "wisp_comments", "wisp_events"
	}
	return "comments", "events"
}

// labelTablesForID picks the (labelTable, eventTable) pair for issueID based
// on wisp routing.
func labelTablesForID(ctx context.Context, c pgxConn, issueID string) (labelTable, eventTable string) {
	if isActiveWispID(ctx, c, issueID) {
		return "wisp_labels", "wisp_events"
	}
	return "labels", "events"
}

// dependencyTablesForID picks the (depTable, eventTable) pair for issueID
// based on wisp routing. Both the issue_id and depends_on_id should be the
// same wisp/persistent class — mixed cases are caller errors.
func dependencyTablesForID(ctx context.Context, c pgxConn, issueID string) (depTable, eventTable string) {
	if isActiveWispID(ctx, c, issueID) {
		return "wisp_dependencies", "wisp_events"
	}
	return "dependencies", "events"
}
