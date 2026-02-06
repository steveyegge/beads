package dolt

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// CreateIssue creates a new issue.
// Delegates to the transaction method for single-source-of-truth logic.
func (s *DoltStore) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.CreateIssue(ctx, issue, actor)
	})
}

// CreateIssues creates multiple issues in a single transaction.
// Delegates to the transaction method for single-source-of-truth logic.
func (s *DoltStore) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	if len(issues) == 0 {
		return nil
	}
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.CreateIssues(ctx, issues, actor)
	})
}

// GetIssue retrieves an issue by ID
func (s *DoltStore) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	issue, err := scanIssue(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, nil
	}

	// Fetch labels
	labels, err := s.GetLabels(ctx, issue.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	issue.Labels = labels

	return issue, nil
}

// GetIssueByExternalRef retrieves an issue by external reference
func (s *DoltStore) GetIssueByExternalRef(ctx context.Context, externalRef string) (*types.Issue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var id string
	err := s.db.QueryRowContext(ctx, "SELECT id FROM issues WHERE external_ref = ?", externalRef).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get issue by external_ref: %w", err)
	}

	return s.GetIssue(ctx, id)
}

// UpdateIssue updates fields on an issue.
// Delegates to the transaction method for single-source-of-truth logic.
func (s *DoltStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.UpdateIssue(ctx, id, updates, actor)
	})
}

// CloseIssue closes an issue with a reason.
// Delegates to the transaction method for single-source-of-truth logic.
func (s *DoltStore) CloseIssue(ctx context.Context, id string, reason string, actor string, session string) error {
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.CloseIssue(ctx, id, reason, actor, session)
	})
}

// DeleteIssue permanently removes an issue.
// Delegates to the transaction method for single-source-of-truth logic.
func (s *DoltStore) DeleteIssue(ctx context.Context, id string) error {
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.DeleteIssue(ctx, id)
	})
}

// =============================================================================
// Helper functions
// =============================================================================

func insertIssue(ctx context.Context, tx *sql.Tx, issue *types.Issue) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO issues (
			id, content_hash, title, description, design, acceptance_criteria, notes,
			status, priority, issue_type, assignee, estimated_minutes,
			created_at, created_by, owner, updated_at, closed_at, external_ref,
			compaction_level, compacted_at, compacted_at_commit, original_size,
			deleted_at, deleted_by, delete_reason, original_type,
			sender, ephemeral, pinned, is_template, crystallizes,
			mol_type, work_type, quality_score, source_system, source_repo, close_reason,
			event_kind, actor, target, payload,
			await_type, await_id, timeout_ns, waiters,
			hook_bead, role_bead, agent_state, last_activity, role_type, rig,
			pod_name, pod_ip, pod_node, pod_status, screen_session,
			due_at, defer_until, metadata,
			advice_hook_command, advice_hook_trigger, advice_hook_timeout, advice_hook_on_failure,
			advice_subscriptions, advice_subscriptions_exclude
		) VALUES (
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?,
			?, ?, ?, ?,
			?, ?
		)
	`,
		issue.ID, issue.ContentHash, issue.Title, issue.Description, issue.Design, issue.AcceptanceCriteria, issue.Notes,
		issue.Status, issue.Priority, issue.IssueType, nullString(issue.Assignee), nullInt(issue.EstimatedMinutes),
		issue.CreatedAt, issue.CreatedBy, issue.Owner, issue.UpdatedAt, issue.ClosedAt, nullStringPtr(issue.ExternalRef),
		issue.CompactionLevel, issue.CompactedAt, nullStringPtr(issue.CompactedAtCommit), nullIntVal(issue.OriginalSize),
		issue.DeletedAt, issue.DeletedBy, issue.DeleteReason, issue.OriginalType,
		issue.Sender, issue.Ephemeral, issue.Pinned, issue.IsTemplate, issue.Crystallizes,
		issue.MolType, issue.WorkType, issue.QualityScore, issue.SourceSystem, issue.SourceRepo, issue.CloseReason,
		issue.EventKind, issue.Actor, issue.Target, issue.Payload,
		issue.AwaitType, issue.AwaitID, issue.Timeout.Nanoseconds(), formatJSONStringArray(issue.Waiters),
		issue.HookBead, issue.RoleBead, issue.AgentState, issue.LastActivity, issue.RoleType, issue.Rig,
		issue.PodName, issue.PodIP, issue.PodNode, issue.PodStatus, issue.ScreenSession,
		issue.DueAt, issue.DeferUntil, jsonMetadata(issue.Metadata),
		// NOTE: advice_target_* columns removed - advice uses labels now
		issue.AdviceHookCommand, issue.AdviceHookTrigger, issue.AdviceHookTimeout, issue.AdviceHookOnFailure,
		formatJSONStringArray(issue.AdviceSubscriptions), formatJSONStringArray(issue.AdviceSubscriptionsExclude),
	)
	return err
}

func scanIssue(ctx context.Context, db *sql.DB, id string) (*types.Issue, error) {
	var issue types.Issue
	var createdAtStr, updatedAtStr sql.NullString // TEXT columns - must parse manually
	var closedAt, compactedAt, deletedAt, lastActivity, dueAt, deferUntil sql.NullTime
	var estimatedMinutes, originalSize, timeoutNs sql.NullInt64
	var assignee, externalRef, compactedAtCommit, owner, createdBy sql.NullString
	var contentHash, sourceRepo, closeReason, deletedBy, deleteReason, originalType sql.NullString
	var workType, sourceSystem sql.NullString
	var sender, molType, eventKind, actor, target, payload sql.NullString
	var awaitType, awaitID, waiters sql.NullString
	var hookBead, roleBead, agentState, roleType, rig sql.NullString
	var podName, podIP, podNode, podStatus, screenSession sql.NullString
	var ephemeral, pinned, isTemplate, crystallizes sql.NullInt64
	var qualityScore sql.NullFloat64
	var metadata sql.NullString
	// Advice hook fields (hq--uaim)
	// NOTE: advice_target_* columns removed - advice uses labels now
	var adviceHookCommand, adviceHookTrigger, adviceHookOnFailure sql.NullString
	var adviceHookTimeout sql.NullInt64
	// Advice subscription fields (gt-w2mh8a.4)
	var adviceSubscriptions, adviceSubscriptionsExclude sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT id, content_hash, title, description, design, acceptance_criteria, notes,
		       status, priority, issue_type, assignee, estimated_minutes,
		       created_at, created_by, owner, updated_at, closed_at, external_ref,
		       compaction_level, compacted_at, compacted_at_commit, original_size, source_repo, close_reason,
		       deleted_at, deleted_by, delete_reason, original_type,
		       sender, ephemeral, pinned, is_template, crystallizes,
		       await_type, await_id, timeout_ns, waiters,
		       hook_bead, role_bead, agent_state, last_activity, role_type, rig,
		       pod_name, pod_ip, pod_node, pod_status, screen_session,
		       mol_type,
		       event_kind, actor, target, payload,
		       due_at, defer_until,
		       quality_score, work_type, source_system, metadata,
		       advice_hook_command, advice_hook_trigger, advice_hook_timeout, advice_hook_on_failure,
		       advice_subscriptions, advice_subscriptions_exclude
		FROM issues
		WHERE id = ?
	`, id).Scan(
		&issue.ID, &contentHash, &issue.Title, &issue.Description, &issue.Design,
		&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
		&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
		&createdAtStr, &createdBy, &owner, &updatedAtStr, &closedAt, &externalRef,
		&issue.CompactionLevel, &compactedAt, &compactedAtCommit, &originalSize, &sourceRepo, &closeReason,
		&deletedAt, &deletedBy, &deleteReason, &originalType,
		&sender, &ephemeral, &pinned, &isTemplate, &crystallizes,
		&awaitType, &awaitID, &timeoutNs, &waiters,
		&hookBead, &roleBead, &agentState, &lastActivity, &roleType, &rig,
		&podName, &podIP, &podNode, &podStatus, &screenSession,
		&molType,
		&eventKind, &actor, &target, &payload,
		&dueAt, &deferUntil,
		&qualityScore, &workType, &sourceSystem, &metadata,
		&adviceHookCommand, &adviceHookTrigger, &adviceHookTimeout, &adviceHookOnFailure,
		&adviceSubscriptions, &adviceSubscriptionsExclude,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
	}

	// Parse timestamp strings (TEXT columns require manual parsing)
	if createdAtStr.Valid {
		issue.CreatedAt = parseTimeString(createdAtStr.String)
	}
	if updatedAtStr.Valid {
		issue.UpdatedAt = parseTimeString(updatedAtStr.String)
	}

	// Map nullable fields
	if contentHash.Valid {
		issue.ContentHash = contentHash.String
	}
	if closedAt.Valid {
		issue.ClosedAt = &closedAt.Time
	}
	if estimatedMinutes.Valid {
		mins := int(estimatedMinutes.Int64)
		issue.EstimatedMinutes = &mins
	}
	if assignee.Valid {
		issue.Assignee = assignee.String
	}
	if owner.Valid {
		issue.Owner = owner.String
	}
	if createdBy.Valid {
		issue.CreatedBy = createdBy.String
	}
	if externalRef.Valid {
		issue.ExternalRef = &externalRef.String
	}
	if compactedAt.Valid {
		issue.CompactedAt = &compactedAt.Time
	}
	if compactedAtCommit.Valid {
		issue.CompactedAtCommit = &compactedAtCommit.String
	}
	if originalSize.Valid {
		issue.OriginalSize = int(originalSize.Int64)
	}
	if sourceRepo.Valid {
		issue.SourceRepo = sourceRepo.String
	}
	if closeReason.Valid {
		issue.CloseReason = closeReason.String
	}
	if deletedAt.Valid {
		issue.DeletedAt = &deletedAt.Time
	}
	if deletedBy.Valid {
		issue.DeletedBy = deletedBy.String
	}
	if deleteReason.Valid {
		issue.DeleteReason = deleteReason.String
	}
	if originalType.Valid {
		issue.OriginalType = originalType.String
	}
	if sender.Valid {
		issue.Sender = sender.String
	}
	if ephemeral.Valid && ephemeral.Int64 != 0 {
		issue.Ephemeral = true
	}
	if pinned.Valid && pinned.Int64 != 0 {
		issue.Pinned = true
	}
	if isTemplate.Valid && isTemplate.Int64 != 0 {
		issue.IsTemplate = true
	}
	if crystallizes.Valid && crystallizes.Int64 != 0 {
		issue.Crystallizes = true
	}
	if awaitType.Valid {
		issue.AwaitType = awaitType.String
	}
	if awaitID.Valid {
		issue.AwaitID = awaitID.String
	}
	if timeoutNs.Valid {
		issue.Timeout = time.Duration(timeoutNs.Int64)
	}
	if waiters.Valid && waiters.String != "" {
		issue.Waiters = parseJSONStringArray(waiters.String)
	}
	if hookBead.Valid {
		issue.HookBead = hookBead.String
	}
	if roleBead.Valid {
		issue.RoleBead = roleBead.String
	}
	if agentState.Valid {
		issue.AgentState = types.AgentState(agentState.String)
	}
	if lastActivity.Valid {
		issue.LastActivity = &lastActivity.Time
	}
	if roleType.Valid {
		issue.RoleType = roleType.String
	}
	if rig.Valid {
		issue.Rig = rig.String
	}
	if podName.Valid {
		issue.PodName = podName.String
	}
	if podIP.Valid {
		issue.PodIP = podIP.String
	}
	if podNode.Valid {
		issue.PodNode = podNode.String
	}
	if podStatus.Valid {
		issue.PodStatus = podStatus.String
	}
	if screenSession.Valid {
		issue.ScreenSession = screenSession.String
	}
	if molType.Valid {
		issue.MolType = types.MolType(molType.String)
	}
	if eventKind.Valid {
		issue.EventKind = eventKind.String
	}
	if actor.Valid {
		issue.Actor = actor.String
	}
	if target.Valid {
		issue.Target = target.String
	}
	if payload.Valid {
		issue.Payload = payload.String
	}
	if dueAt.Valid {
		issue.DueAt = &dueAt.Time
	}
	if deferUntil.Valid {
		issue.DeferUntil = &deferUntil.Time
	}
	if qualityScore.Valid {
		qs := float32(qualityScore.Float64)
		issue.QualityScore = &qs
	}
	if workType.Valid {
		issue.WorkType = types.WorkType(workType.String)
	}
	if sourceSystem.Valid {
		issue.SourceSystem = sourceSystem.String
	}
	// Custom metadata field (GH#1406)
	if metadata.Valid && metadata.String != "" && metadata.String != "{}" {
		issue.Metadata = []byte(metadata.String)
	}
	// NOTE: advice_target_* fields removed - advice uses labels now
	// Advice hook fields (hq--uaim)
	if adviceHookCommand.Valid {
		issue.AdviceHookCommand = adviceHookCommand.String
	}
	if adviceHookTrigger.Valid {
		issue.AdviceHookTrigger = adviceHookTrigger.String
	}
	if adviceHookTimeout.Valid {
		issue.AdviceHookTimeout = int(adviceHookTimeout.Int64)
	}
	if adviceHookOnFailure.Valid {
		issue.AdviceHookOnFailure = adviceHookOnFailure.String
	}
	// Advice subscription fields (gt-w2mh8a.4)
	if adviceSubscriptions.Valid && adviceSubscriptions.String != "" {
		issue.AdviceSubscriptions = parseJSONStringArray(adviceSubscriptions.String)
	}
	if adviceSubscriptionsExclude.Valid && adviceSubscriptionsExclude.String != "" {
		issue.AdviceSubscriptionsExclude = parseJSONStringArray(adviceSubscriptionsExclude.String)
	}

	return &issue, nil
}

func recordEvent(ctx context.Context, tx *sql.Tx, issueID string, eventType types.EventType, actor, oldValue, newValue string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO events (issue_id, event_type, actor, old_value, new_value)
		VALUES (?, ?, ?, ?, ?)
	`, issueID, eventType, actor, oldValue, newValue)
	return err
}

func (s *DoltStore) markDirty(ctx context.Context, tx *sql.Tx, issueID string) error {
	if s.skipDirtyTracking {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO dirty_issues (issue_id, marked_at)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE marked_at = VALUES(marked_at)
	`, issueID, time.Now().UTC())
	return err
}

func isAllowedUpdateField(key string) bool {
	allowed := map[string]bool{
		"status": true, "priority": true, "title": true, "assignee": true,
		"description": true, "design": true, "acceptance_criteria": true, "notes": true,
		"issue_type": true, "estimated_minutes": true, "external_ref": true,
		"closed_at": true, "close_reason": true, "closed_by_session": true,
		"sender": true, "wisp": true, "pinned": true,
		"hook_bead": true, "role_bead": true, "agent_state": true, "last_activity": true,
		"role_type": true, "rig": true, "mol_type": true,
		"pod_name": true, "pod_ip": true, "pod_node": true, "pod_status": true, "screen_session": true,
		"event_category": true, "event_actor": true, "event_target": true, "event_payload": true,
		"due_at": true, "defer_until": true, "await_id": true, "waiters": true,
		"metadata": true,
		// Advice hook fields
		"advice_hook_command": true, "advice_hook_trigger": true,
		"advice_hook_timeout": true, "advice_hook_on_failure": true,
		// Advice subscription fields
		"advice_subscriptions": true, "advice_subscriptions_exclude": true,
	}
	return allowed[key]
}

func manageClosedAt(oldIssue *types.Issue, updates map[string]interface{}, setClauses []string, args []interface{}) ([]string, []interface{}) {
	statusVal, hasStatus := updates["status"]
	_, hasExplicitClosedAt := updates["closed_at"]
	if hasExplicitClosedAt || !hasStatus {
		return setClauses, args
	}

	var newStatus string
	switch v := statusVal.(type) {
	case string:
		newStatus = v
	case types.Status:
		newStatus = string(v)
	default:
		return setClauses, args
	}

	if newStatus == string(types.StatusClosed) {
		now := time.Now().UTC()
		setClauses = append(setClauses, "closed_at = ?")
		args = append(args, now)
	} else if oldIssue.Status == types.StatusClosed {
		setClauses = append(setClauses, "closed_at = ?", "close_reason = ?")
		args = append(args, nil, "")
	}

	return setClauses, args
}

func determineEventType(oldIssue *types.Issue, updates map[string]interface{}) types.EventType {
	statusVal, hasStatus := updates["status"]
	if !hasStatus {
		return types.EventUpdated
	}

	newStatus, ok := statusVal.(string)
	if !ok {
		return types.EventUpdated
	}

	if newStatus == string(types.StatusClosed) {
		return types.EventClosed
	}
	if oldIssue.Status == types.StatusClosed {
		return types.EventReopened
	}
	return types.EventStatusChanged
}

// Helper functions for nullable values
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullStringPtr(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

func nullInt(i *int) interface{} {
	if i == nil {
		return nil
	}
	return *i
}

func nullIntVal(i int) interface{} {
	if i == 0 {
		return nil
	}
	return i
}

// jsonMetadata returns the metadata as a string, or "{}" if empty.
// Dolt's JSON column type requires valid JSON, so we can't insert empty strings.
func jsonMetadata(m []byte) string {
	if len(m) == 0 {
		return "{}"
	}
	return string(m)
}

func parseJSONStringArray(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil
	}
	return result
}

func formatJSONStringArray(arr []string) string {
	if len(arr) == 0 {
		return ""
	}
	data, err := json.Marshal(arr)
	if err != nil {
		return ""
	}
	return string(data)
}
