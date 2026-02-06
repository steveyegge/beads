package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/eventbus"
	"github.com/steveyegge/beads/internal/idgen"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/util"
	"github.com/steveyegge/beads/internal/utils"
)

// containsLabel checks if a label exists in the list
func containsLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

// removeLabel removes a label from the list if present
func removeLabel(labels []string, label string) []string {
	result := make([]string, 0, len(labels))
	for _, l := range labels {
		if l != label {
			result = append(result, l)
		}
	}
	return result
}

// applyUpdatesToIssue applies a map of updates to an issue struct.
// Used for wisp updates where we can't use the storage layer.
func applyUpdatesToIssue(issue *types.Issue, updates map[string]interface{}) {
	for key, value := range updates {
		switch key {
		case "title":
			if v, ok := value.(string); ok {
				issue.Title = v
			}
		case "description":
			if v, ok := value.(string); ok {
				issue.Description = v
			}
		case "status":
			if v, ok := value.(string); ok {
				issue.Status = types.Status(v)
			}
		case "priority":
			if v, ok := value.(int); ok {
				issue.Priority = v
			}
		case "issue_type":
			if v, ok := value.(string); ok {
				issue.IssueType = types.IssueType(v)
			}
		case "assignee":
			if v, ok := value.(string); ok {
				issue.Assignee = v
			}
		case "notes":
			if v, ok := value.(string); ok {
				issue.Notes = v
			}
		case "design":
			if v, ok := value.(string); ok {
				issue.Design = v
			}
		case "acceptance_criteria":
			if v, ok := value.(string); ok {
				issue.AcceptanceCriteria = v
			}
		case "pinned":
			if v, ok := value.(bool); ok {
				issue.Pinned = v
			}
		}
	}
	issue.UpdatedAt = time.Now()
}

// parseTimeRPC parses time strings in multiple formats (RFC3339, YYYY-MM-DD, etc.)
// Matches the parseTimeFlag behavior in cmd/bd/list.go for CLI parity
func parseTimeRPC(s string) (time.Time, error) {
	// Try RFC3339 first (ISO 8601 with timezone)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	
	// Try YYYY-MM-DD format (common user input)
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	
	// Try YYYY-MM-DD HH:MM:SS format
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t, nil
	}
	
	return time.Time{}, fmt.Errorf("unsupported date format: %q (use YYYY-MM-DD or RFC3339)", s)
}

func strValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func updatesFromArgs(a UpdateArgs) (map[string]interface{}, error) {
	u := map[string]interface{}{}
	if a.Title != nil {
		u["title"] = *a.Title
	}
	if a.Description != nil {
		u["description"] = *a.Description
	}
	if a.Status != nil {
		u["status"] = *a.Status
	}
	if a.Priority != nil {
		u["priority"] = *a.Priority
	}
	if a.Design != nil {
		u["design"] = *a.Design
	}
	if a.AcceptanceCriteria != nil {
		u["acceptance_criteria"] = *a.AcceptanceCriteria
	}
	if a.Notes != nil {
		u["notes"] = *a.Notes
	}
	if a.Assignee != nil {
		u["assignee"] = *a.Assignee
	}
	if a.ExternalRef != nil {
		u["external_ref"] = *a.ExternalRef
	}
	if a.EstimatedMinutes != nil {
		u["estimated_minutes"] = *a.EstimatedMinutes
	}
	if a.IssueType != nil {
		u["issue_type"] = *a.IssueType
	}
	// Messaging fields
	if a.Sender != nil {
		u["sender"] = *a.Sender
	}
	if a.Ephemeral != nil {
		u["wisp"] = *a.Ephemeral // Storage API uses "wisp", maps to "ephemeral" column
	}
	if a.RepliesTo != nil {
		u["replies_to"] = *a.RepliesTo
	}
	// Graph link fields
	if a.RelatesTo != nil {
		u["relates_to"] = *a.RelatesTo
	}
	if a.DuplicateOf != nil {
		u["duplicate_of"] = *a.DuplicateOf
	}
	if a.SupersededBy != nil {
		u["superseded_by"] = *a.SupersededBy
	}
	// Pinned field
	if a.Pinned != nil {
		u["pinned"] = *a.Pinned
	}
	// Agent slot fields
	if a.HookBead != nil {
		u["hook_bead"] = *a.HookBead
	}
	if a.RoleBead != nil {
		u["role_bead"] = *a.RoleBead
	}
	// Agent state fields
	if a.AgentState != nil {
		u["agent_state"] = *a.AgentState
	}
	if a.LastActivity != nil && *a.LastActivity {
		u["last_activity"] = time.Now()
	}
	// Agent identity fields
	if a.RoleType != nil {
		u["role_type"] = *a.RoleType
	}
	if a.Rig != nil {
		u["rig"] = *a.Rig
	}
	// Agent pod fields (gt-el7sxq.7)
	if a.PodName != nil {
		u["pod_name"] = *a.PodName
	}
	if a.PodIP != nil {
		u["pod_ip"] = *a.PodIP
	}
	if a.PodNode != nil {
		u["pod_node"] = *a.PodNode
	}
	if a.PodStatus != nil {
		u["pod_status"] = *a.PodStatus
	}
	if a.ScreenSession != nil {
		u["screen_session"] = *a.ScreenSession
	}
	// Event fields
	if a.EventCategory != nil {
		u["event_category"] = *a.EventCategory
	}
	if a.EventActor != nil {
		u["event_actor"] = *a.EventActor
	}
	if a.EventTarget != nil {
		u["event_target"] = *a.EventTarget
	}
	if a.EventPayload != nil {
		u["event_payload"] = *a.EventPayload
	}
	// Gate fields
	if a.AwaitID != nil {
		u["await_id"] = *a.AwaitID
	}
	if len(a.Waiters) > 0 {
		u["waiters"] = a.Waiters
	}
	// Slot fields
	if a.Holder != nil {
		u["holder"] = *a.Holder
	}
	// Time-based scheduling fields (GH#820)
	if a.DueAt != nil {
		if *a.DueAt == "" {
			u["due_at"] = nil // Clear the field
		} else {
			// Try date-only format first (YYYY-MM-DD)
			if t, err := time.ParseInLocation("2006-01-02", *a.DueAt, time.Local); err == nil {
				u["due_at"] = t
			} else if t, err := time.Parse(time.RFC3339, *a.DueAt); err == nil {
				// Try RFC3339 format (2025-01-15T10:00:00Z)
				u["due_at"] = t
			} else {
				return nil, fmt.Errorf("invalid due_at format %q: use YYYY-MM-DD or RFC3339", *a.DueAt)
			}
		}
	}
	if a.DeferUntil != nil {
		if *a.DeferUntil == "" {
			u["defer_until"] = nil // Clear the field
		} else {
			// Try date-only format first (YYYY-MM-DD)
			if t, err := time.ParseInLocation("2006-01-02", *a.DeferUntil, time.Local); err == nil {
				u["defer_until"] = t
			} else if t, err := time.Parse(time.RFC3339, *a.DeferUntil); err == nil {
				// Try RFC3339 format (2025-01-15T10:00:00Z)
				u["defer_until"] = t
			} else {
				return nil, fmt.Errorf("invalid defer_until format %q: use YYYY-MM-DD or RFC3339", *a.DeferUntil)
			}
		}
	}
	// NOTE: Legacy advice targeting fields removed - use labels instead
	// Advice hook fields (hq--uaim)
	if a.AdviceHookCommand != nil {
		u["advice_hook_command"] = *a.AdviceHookCommand
	}
	if a.AdviceHookTrigger != nil {
		u["advice_hook_trigger"] = *a.AdviceHookTrigger
	}
	if a.AdviceHookTimeout != nil {
		u["advice_hook_timeout"] = *a.AdviceHookTimeout
	}
	if a.AdviceHookOnFailure != nil {
		u["advice_hook_on_failure"] = *a.AdviceHookOnFailure
	}
	// Advice subscription fields (gt-w2mh8a.6)
	if len(a.AdviceSubscriptions) > 0 {
		u["advice_subscriptions"] = a.AdviceSubscriptions
	}
	if len(a.AdviceSubscriptionsExclude) > 0 {
		u["advice_subscriptions_exclude"] = a.AdviceSubscriptionsExclude
	}
	return u, nil
}

func (s *Server) handleCreate(req *Request) Response {
	var createArgs CreateArgs
	if err := json.Unmarshal(req.Args, &createArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid create args: %v", err),
		}
	}

	// Check for conflicting flags
	if createArgs.ID != "" && createArgs.Parent != "" {
		return Response{
			Success: false,
			Error:   "cannot specify both ID and Parent",
		}
	}

	// Warn if creating an issue without a description (unless it's a test issue)
	if createArgs.Description == "" && !strings.Contains(strings.ToLower(createArgs.Title), "test") {
		// Log warning to daemon logs (stderr goes to daemon logs)
		fmt.Fprintf(os.Stderr, "[WARNING] Creating issue '%s' without description. Issues without descriptions lack context for future work.\n", createArgs.Title)
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// If parent is specified, generate child ID
	issueID := createArgs.ID
	if createArgs.Parent != "" {
		childID, err := store.GetNextChildID(ctx, createArgs.Parent)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to generate child ID: %v", err),
			}
		}
		issueID = childID
	}

	// Resolve TargetRig to prefix via route beads (gt-oasyjm.1 - routes in beads)
	// This enables --rig flag to work with remote daemon by looking up the prefix
	// for the target rig from route beads in the single canonical Dolt database.
	var prefixOverride string

	// Direct prefix override from client's local config.yaml (gt-wnbjj8.3).
	// This allows each rig to use its own prefix when creating issues via the
	// shared daemon, without needing to resolve via TargetRig/route beads.
	if createArgs.Prefix != "" && issueID == "" {
		prefixOverride = createArgs.Prefix
	}

	if createArgs.TargetRig != "" && issueID == "" && prefixOverride == "" {
		// Query route beads to find the prefix for this rig
		// Route beads have type=route, status=open, title format "prefix → path"
		routeType := types.IssueType("route")
		openStatus := types.StatusOpen
		routeBeads, err := store.SearchIssues(ctx, "", types.IssueFilter{
			IssueType: &routeType,
			Status:    &openStatus,
		})
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to query route beads: %v", err),
			}
		}
		for _, issue := range routeBeads {
			route := routing.ParseRouteFromTitle(issue.Title)
			// Match rig name (first component of path) or the path itself
			project := routing.ExtractProjectFromPath(route.Path)
			if project == createArgs.TargetRig || route.Path == createArgs.TargetRig {
				// Found matching route - extract prefix (without trailing hyphen for PrefixOverride)
				prefixOverride = strings.TrimSuffix(route.Prefix, "-")
				break
			}
		}
		if prefixOverride == "" {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("rig %q not found in route beads", createArgs.TargetRig),
			}
		}
	}

	// Generate wisp ID if not provided (gt-tlrw90)
	// Wisps bypass the regular storage path which auto-generates IDs,
	// so we need to generate one here when creating ephemeral issues without an ID
	if issueID == "" && createArgs.Ephemeral {
		// Get configured prefix for ID generation
		// Use prefixOverride from TargetRig if set, otherwise use config
		basePrefix := prefixOverride
		if basePrefix == "" {
			configPrefix, err := store.GetConfig(ctx, "issue_prefix")
			if err != nil || configPrefix == "" {
				configPrefix = "bd" // fallback to default prefix
			}
			basePrefix = configPrefix
		}
		// Combine with IDPrefix if set (e.g., "hq" + "wisp" → "hq-wisp")
		wispPrefix := basePrefix
		if createArgs.IDPrefix != "" {
			wispPrefix = basePrefix + "-" + createArgs.IDPrefix
		} else {
			wispPrefix = basePrefix + "-wisp"
		}
		// Generate hash-based ID using title, description, and timestamp
		issueID = idgen.GenerateHashID(wispPrefix, createArgs.Title, createArgs.Description, s.reqActor(req), time.Now(), 6, 0)
	}

	var design, acceptance, notes, assignee, externalRef *string
	if createArgs.Design != "" {
		design = &createArgs.Design
	}
	if createArgs.AcceptanceCriteria != "" {
		acceptance = &createArgs.AcceptanceCriteria
	}
	if createArgs.Notes != "" {
		notes = &createArgs.Notes
	}
	if createArgs.Assignee != "" {
		assignee = &createArgs.Assignee
	}
	if createArgs.ExternalRef != "" {
		externalRef = &createArgs.ExternalRef
	}

	// Parse DueAt if provided (GH#820)
	var dueAt *time.Time
	if createArgs.DueAt != "" {
		// Try date-only format first (YYYY-MM-DD)
		if t, err := time.ParseInLocation("2006-01-02", createArgs.DueAt, time.Local); err == nil {
			dueAt = &t
		} else if t, err := time.Parse(time.RFC3339, createArgs.DueAt); err == nil {
			// Try RFC3339 format (2025-01-15T10:00:00Z)
			dueAt = &t
		} else {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid due_at format %q. Examples: 2025-01-15, 2025-01-15T10:00:00Z", createArgs.DueAt),
			}
		}
	}

	// Parse DeferUntil if provided (GH#820, GH#950, GH#952)
	var deferUntil *time.Time
	if createArgs.DeferUntil != "" {
		// Try date-only format first (YYYY-MM-DD)
		if t, err := time.ParseInLocation("2006-01-02", createArgs.DeferUntil, time.Local); err == nil {
			deferUntil = &t
		} else if t, err := time.Parse(time.RFC3339, createArgs.DeferUntil); err == nil {
			// Try RFC3339 format (2025-01-15T10:00:00Z)
			deferUntil = &t
		} else {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid defer_until format %q. Examples: 2025-01-15, 2025-01-15T10:00:00Z", createArgs.DeferUntil),
			}
		}
	}

	issue := &types.Issue{
		ID:                 issueID,
		Title:              createArgs.Title,
		Description:        createArgs.Description,
		IssueType:          types.IssueType(createArgs.IssueType),
		Priority:           createArgs.Priority,
		Design:             strValue(design),
		AcceptanceCriteria: strValue(acceptance),
		Notes:              strValue(notes),
		Assignee:           strValue(assignee),
		ExternalRef:        externalRef,
		EstimatedMinutes:   createArgs.EstimatedMinutes,
		Status:             types.StatusOpen,
		// Messaging fields
		Sender:    createArgs.Sender,
		Ephemeral: createArgs.Ephemeral,
		Pinned:    createArgs.Pinned,
		AutoClose: createArgs.AutoClose,
		// NOTE: RepliesTo now handled via replies-to dependency (Decision 004)
		// ID generation
		IDPrefix:       createArgs.IDPrefix,
		PrefixOverride: prefixOverride, // TargetRig resolution (gt-oasyjm.1)
		CreatedBy:      createArgs.CreatedBy,
		Owner:     createArgs.Owner,
		// Molecule type
		MolType: types.MolType(createArgs.MolType),
		// Agent identity fields
		RoleType: createArgs.RoleType,
		Rig:      createArgs.Rig,
		// Event fields (map protocol names to internal names)
		EventKind: createArgs.EventCategory,
		Actor:     createArgs.EventActor,
		Target:    createArgs.EventTarget,
		Payload:   createArgs.EventPayload,
		// Time-based scheduling (GH#820, GH#950, GH#952)
		DueAt:      dueAt,
		DeferUntil: deferUntil,
		// Gate fields (async coordination - hq-b0b22c.3)
		AwaitType: createArgs.AwaitType,
		AwaitID:   createArgs.AwaitID,
		Timeout:   createArgs.Timeout,
		Waiters:   createArgs.Waiters,
		// Skill fields (only valid when IssueType == "skill")
		SkillName:       createArgs.SkillName,
		SkillVersion:    createArgs.SkillVersion,
		SkillCategory:   createArgs.SkillCategory,
		SkillInputs:     createArgs.SkillInputs,
		SkillOutputs:    createArgs.SkillOutputs,
		SkillExamples:   createArgs.SkillExamples,
		ClaudeSkillPath: createArgs.ClaudeSkillPath,
		SkillContent:    createArgs.SkillContent,
		// NOTE: Legacy advice targeting fields removed - use labels instead
		// Advice hook fields (hq--uaim)
		AdviceHookCommand:   createArgs.AdviceHookCommand,
		AdviceHookTrigger:   createArgs.AdviceHookTrigger,
		AdviceHookTimeout:   createArgs.AdviceHookTimeout,
		AdviceHookOnFailure: createArgs.AdviceHookOnFailure,
	}

	// Check if any dependencies are discovered-from type
	// If so, inherit source_repo from the parent issue
	var discoveredFromParentID string
	for _, depSpec := range createArgs.Dependencies {
		depSpec = strings.TrimSpace(depSpec)
		if depSpec == "" {
			continue
		}
		
		var depType types.DependencyType
		var dependsOnID string
		
		if strings.Contains(depSpec, ":") {
			parts := strings.SplitN(depSpec, ":", 2)
			if len(parts) == 2 {
				depType = types.DependencyType(strings.TrimSpace(parts[0]))
				dependsOnID = strings.TrimSpace(parts[1])
				
				if depType == types.DepDiscoveredFrom {
					discoveredFromParentID = dependsOnID
					break
				}
			}
		}
	}
	
	// If we found a discovered-from dependency, inherit source_repo from parent
	if discoveredFromParentID != "" {
		parentIssue, err := store.GetIssue(ctx, discoveredFromParentID)
		if err == nil && parentIssue.SourceRepo != "" {
			issue.SourceRepo = parentIssue.SourceRepo
		}
		// If error getting parent or parent has no source_repo, continue with default
	}

	// Route wisps (ephemeral issues) to in-memory WispStore
	if isWisp(issue) && s.wispStore != nil {
		// Generate ID for wisp if not already set (gt-tlrw90)
		// Wisps use in-memory store which doesn't generate IDs like SQLite does
		if issue.ID == "" {
			// Use IDPrefix for wisp-specific prefix, fallback to "wisp"
			prefix := "wisp"
			if issue.IDPrefix != "" {
				prefix = issue.IDPrefix
			}
			// Generate a hash-based ID (6 chars is usually sufficient for wisps)
			issue.ID = idgen.GenerateHashID(prefix, issue.Title, issue.Description, s.reqActor(req), issue.CreatedAt, 6, 0)
		}
		if err := s.wispStore.Create(ctx, issue); err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to create wisp: %v", err),
			}
		}
		// Emit mutation event for event-driven daemon
		s.emitMutation(MutationCreate, issue.ID, issue.Title, issue.Assignee)

		// Wisps don't support dependencies, labels, or other persistent relations
		// They are purely in-memory ephemeral issues
		data, _ := json.Marshal(issue)
		return Response{
			Success: true,
			Data:    data,
		}
	}

	// Pre-validate dependency specs and waits-for gate before starting the transaction
	// so we can return errors without rolling back
	type parsedDep struct {
		depType     types.DependencyType
		dependsOnID string
	}
	var parsedDeps []parsedDep
	for _, depSpec := range createArgs.Dependencies {
		depSpec = strings.TrimSpace(depSpec)
		if depSpec == "" {
			continue
		}

		var depType types.DependencyType
		var dependsOnID string

		if strings.Contains(depSpec, ":") {
			parts := strings.SplitN(depSpec, ":", 2)
			if len(parts) != 2 {
				return Response{
					Success: false,
					Error:   fmt.Sprintf("invalid dependency format '%s', expected 'type:id' or 'id'", depSpec),
				}
			}
			depType = types.DependencyType(strings.TrimSpace(parts[0]))
			dependsOnID = strings.TrimSpace(parts[1])
		} else {
			depType = types.DepBlocks
			dependsOnID = depSpec
		}

		if !depType.IsValid() {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid dependency type '%s' (valid: blocks, related, parent-child, discovered-from)", depType),
			}
		}
		parsedDeps = append(parsedDeps, parsedDep{depType: depType, dependsOnID: dependsOnID})
	}

	// Pre-validate waits-for gate
	var waitsForMeta string
	if createArgs.WaitsFor != "" {
		gate := createArgs.WaitsForGate
		if gate == "" {
			gate = types.WaitsForAllChildren
		}
		if gate != types.WaitsForAllChildren && gate != types.WaitsForAnyChildren {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid waits_for_gate value '%s' (valid: all-children, any-children)", gate),
			}
		}
		meta := types.WaitsForMeta{Gate: gate}
		metaJSON, err := json.Marshal(meta)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to serialize waits-for metadata: %v", err),
			}
		}
		waitsForMeta = string(metaJSON)
	}

	// Wrap all writes in a single transaction to prevent FK constraint violations
	// when creating issues with dependencies and labels (hq-7yh6lz)
	actor := s.reqActor(req)
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		if err := tx.CreateIssue(ctx, issue, actor); err != nil {
			return fmt.Errorf("failed to create issue: %w", err)
		}

		// Parent-child dependency
		if createArgs.Parent != "" {
			dep := &types.Dependency{
				IssueID:     issue.ID,
				DependsOnID: createArgs.Parent,
				Type:        types.DepParentChild,
			}
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("failed to add parent-child dependency %s -> %s: %w", issue.ID, createArgs.Parent, err)
			}
		}

		// Replies-to dependency (Decision 004)
		if createArgs.RepliesTo != "" {
			dep := &types.Dependency{
				IssueID:     issue.ID,
				DependsOnID: createArgs.RepliesTo,
				Type:        types.DepRepliesTo,
				ThreadID:    createArgs.RepliesTo,
			}
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("failed to add replies-to dependency %s -> %s: %w", issue.ID, createArgs.RepliesTo, err)
			}
		}

		// Labels
		for _, label := range createArgs.Labels {
			if err := tx.AddLabel(ctx, issue.ID, label, actor); err != nil {
				return fmt.Errorf("failed to add label %s: %w", label, err)
			}
		}

		// Auto-add role_type/rig labels for agent beads
		if containsLabel(createArgs.Labels, "gt:agent") {
			if issue.RoleType != "" {
				if err := tx.AddLabel(ctx, issue.ID, "role_type:"+issue.RoleType, actor); err != nil {
					return fmt.Errorf("failed to add role_type label: %w", err)
				}
			}
			if issue.Rig != "" {
				if err := tx.AddLabel(ctx, issue.ID, "rig:"+issue.Rig, actor); err != nil {
					return fmt.Errorf("failed to add rig label: %w", err)
				}
			}
		}

		// Dependencies
		for _, pd := range parsedDeps {
			dep := &types.Dependency{
				IssueID:     issue.ID,
				DependsOnID: pd.dependsOnID,
				Type:        pd.depType,
			}
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("failed to add dependency %s -> %s: %w", issue.ID, pd.dependsOnID, err)
			}
		}

		// Waits-for dependency
		if createArgs.WaitsFor != "" {
			dep := &types.Dependency{
				IssueID:     issue.ID,
				DependsOnID: createArgs.WaitsFor,
				Type:        types.DepWaitsFor,
				Metadata:    waitsForMeta,
			}
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("failed to add waits-for dependency %s -> %s: %w", issue.ID, createArgs.WaitsFor, err)
			}
		}

		return nil
	})
	if err != nil {
		return Response{
			Success: false,
			Error:   err.Error(),
		}
	}

	// Validate type schema after transaction commits (gt-pozvwr.6)
	// GetTypeSchema is a read-only operation not on the Transaction interface
	if schema, schemaErr := store.GetTypeSchema(ctx, string(issue.IssueType)); schemaErr == nil && schema != nil {
		if err := issue.ValidateAgainstSchema(schema, createArgs.Labels); err != nil {
			return Response{
				Success: false,
				Error:   err.Error(),
			}
		}
	}

	// Emit mutation event for event-driven daemon (after transaction commits)
	s.emitMutation(MutationCreate, issue.ID, issue.Title, issue.Assignee)

	// Emit advice bus event if this is an advice bead (bd-z4cu.2)
	if issue.IssueType == types.TypeAdvice {
		s.emitAdviceEvent(eventbus.EventAdviceCreated, AdviceEventPayload{
			ID:                  issue.ID,
			Title:               issue.Title,
			Labels:              createArgs.Labels,
			AdviceHookCommand:   issue.AdviceHookCommand,
			AdviceHookTrigger:   issue.AdviceHookTrigger,
			AdviceHookTimeout:   issue.AdviceHookTimeout,
			AdviceHookOnFailure: issue.AdviceHookOnFailure,
		})
	}

	// Update label cache for the new issue
	if s.labelCache != nil && len(createArgs.Labels) > 0 {
		s.labelCache.SetLabels(issue.ID, issue.Labels)
	}

	data, _ := json.Marshal(issue)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleUpdate(req *Request) Response {
	var updateArgs UpdateArgs
	if err := json.Unmarshal(req.Args, &updateArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid update args: %v", err),
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Check WispStore first for wisp IDs
	if s.wispStore != nil && isWispID(updateArgs.ID) {
		issue, err := s.wispStore.Get(ctx, updateArgs.ID)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to get wisp: %v", err),
			}
		}
		if issue != nil {
			// Found in WispStore - apply updates to wisp
			updates, err := updatesFromArgs(updateArgs)
			if err != nil {
				return Response{
					Success: false,
					Error:   err.Error(),
				}
			}

			// Apply updates to the wisp issue
			applyUpdatesToIssue(issue, updates)

			// Handle label operations for wisps (stored directly on issue)
			if len(updateArgs.SetLabels) > 0 {
				issue.Labels = updateArgs.SetLabels
			}
			for _, label := range updateArgs.AddLabels {
				if !containsLabel(issue.Labels, label) {
					issue.Labels = append(issue.Labels, label)
				}
			}
			for _, label := range updateArgs.RemoveLabels {
				issue.Labels = removeLabel(issue.Labels, label)
			}

			if err := s.wispStore.Update(ctx, issue); err != nil {
				return Response{
					Success: false,
					Error:   fmt.Sprintf("failed to update wisp: %v", err),
				}
			}

			// Emit mutation event
			s.emitMutation(MutationUpdate, issue.ID, issue.Title, issue.Assignee)

			data, _ := json.Marshal(issue)
			return Response{
				Success: true,
				Data:    data,
			}
		}
		// Not found in WispStore, fall through to regular storage
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	// Check if issue is a template (beads-1ra): templates are read-only
	issue, err := store.GetIssue(ctx, updateArgs.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get issue: %v", err),
		}
	}
	if issue == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("issue %s not found", updateArgs.ID),
		}
	}
	if issue.IsTemplate {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("cannot update template %s: templates are read-only; use 'bd molecule instantiate' to create a work item", updateArgs.ID),
		}
	}

	actor := s.reqActor(req)

	// Pre-validate claim before transaction (pure check against pre-fetched issue)
	if updateArgs.Claim && issue.Assignee != "" {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("already claimed by %s", issue.Assignee),
		}
	}

	// Parse updates before transaction (pure validation, no DB)
	updates, err := updatesFromArgs(updateArgs)
	if err != nil {
		return Response{
			Success: false,
			Error:   err.Error(),
		}
	}

	// Wrap all writes in a single transaction to prevent FK constraint violations (hq-7yh6lz)
	err = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Claim operation
		if updateArgs.Claim {
			claimUpdates := map[string]interface{}{
				"assignee": actor,
				"status":   "in_progress",
			}
			if err := tx.UpdateIssue(ctx, updateArgs.ID, claimUpdates, actor); err != nil {
				return fmt.Errorf("failed to claim issue: %w", err)
			}
		}

		// Regular field updates
		if len(updates) > 0 {
			if err := tx.UpdateIssue(ctx, updateArgs.ID, updates, actor); err != nil {
				return fmt.Errorf("failed to update issue: %w", err)
			}
		}

		// Set labels (replaces all existing labels)
		if len(updateArgs.SetLabels) > 0 {
			currentLabels, err := tx.GetLabels(ctx, updateArgs.ID)
			if err != nil {
				return fmt.Errorf("failed to get current labels: %w", err)
			}
			for _, label := range currentLabels {
				if err := tx.RemoveLabel(ctx, updateArgs.ID, label, actor); err != nil {
					return fmt.Errorf("failed to remove label %s: %w", label, err)
				}
			}
			for _, label := range updateArgs.SetLabels {
				if err := tx.AddLabel(ctx, updateArgs.ID, label, actor); err != nil {
					return fmt.Errorf("failed to set label %s: %w", label, err)
				}
			}
		}

		// Add labels
		for _, label := range updateArgs.AddLabels {
			if err := tx.AddLabel(ctx, updateArgs.ID, label, actor); err != nil {
				return fmt.Errorf("failed to add label %s: %w", label, err)
			}
		}

		// Remove labels
		for _, label := range updateArgs.RemoveLabels {
			if err := tx.RemoveLabel(ctx, updateArgs.ID, label, actor); err != nil {
				return fmt.Errorf("failed to remove label %s: %w", label, err)
			}
		}

		// Auto-add role_type/rig labels for agent beads
		issueLabels, _ := tx.GetLabels(ctx, updateArgs.ID)
		if containsLabel(issueLabels, "gt:agent") {
			if updateArgs.RoleType != nil && *updateArgs.RoleType != "" {
				for _, l := range issueLabels {
					if strings.HasPrefix(l, "role_type:") {
						_ = tx.RemoveLabel(ctx, updateArgs.ID, l, actor)
					}
				}
				if err := tx.AddLabel(ctx, updateArgs.ID, "role_type:"+*updateArgs.RoleType, actor); err != nil {
					return fmt.Errorf("failed to add role_type label: %w", err)
				}
			}
			if updateArgs.Rig != nil && *updateArgs.Rig != "" {
				for _, l := range issueLabels {
					if strings.HasPrefix(l, "rig:") {
						_ = tx.RemoveLabel(ctx, updateArgs.ID, l, actor)
					}
				}
				if err := tx.AddLabel(ctx, updateArgs.ID, "rig:"+*updateArgs.Rig, actor); err != nil {
					return fmt.Errorf("failed to add rig label: %w", err)
				}
			}
		}

		// Reparenting
		if updateArgs.Parent != nil {
			newParentID := *updateArgs.Parent

			// Validate new parent exists (unless empty string to remove parent)
			if newParentID != "" {
				newParent, err := tx.GetIssue(ctx, newParentID)
				if err != nil {
					return fmt.Errorf("failed to get new parent: %w", err)
				}
				if newParent == nil {
					return fmt.Errorf("parent issue %s not found", newParentID)
				}
			}

			// Find and remove existing parent-child dependency
			deps, err := tx.GetDependencyRecords(ctx, updateArgs.ID)
			if err != nil {
				return fmt.Errorf("failed to get dependencies: %w", err)
			}
			for _, dep := range deps {
				if dep.Type == types.DepParentChild {
					if err := tx.RemoveDependency(ctx, updateArgs.ID, dep.DependsOnID, actor); err != nil {
						return fmt.Errorf("failed to remove old parent dependency: %w", err)
					}
					break
				}
			}

			// Add new parent-child dependency (if not removing parent)
			if newParentID != "" {
				newDep := &types.Dependency{
					IssueID:     updateArgs.ID,
					DependsOnID: newParentID,
					Type:        types.DepParentChild,
				}
				if err := tx.AddDependency(ctx, newDep, actor); err != nil {
					return fmt.Errorf("failed to add parent dependency: %w", err)
				}
			}
		}

		return nil
	})
	if err != nil {
		return Response{
			Success: false,
			Error:   err.Error(),
		}
	}

	// Re-validate type schema after transaction commits (gt-pozvwr.6)
	// GetTypeSchema is a read-only operation not on the Transaction interface
	if len(updates) > 0 || len(updateArgs.SetLabels) > 0 || len(updateArgs.AddLabels) > 0 || len(updateArgs.RemoveLabels) > 0 {
		updatedIssue, err := store.GetIssue(ctx, updateArgs.ID)
		if err == nil && updatedIssue != nil {
			if schema, err := store.GetTypeSchema(ctx, string(updatedIssue.IssueType)); err == nil && schema != nil {
				currentLabels, _ := store.GetLabels(ctx, updateArgs.ID)
				if err := updatedIssue.ValidateAgainstSchema(schema, currentLabels); err != nil {
					return Response{
						Success: false,
						Error:   err.Error(),
					}
				}
			}
		}
	}

	// Emit mutation event for event-driven daemon (after transaction commits)
	if len(updates) > 0 || len(updateArgs.SetLabels) > 0 || len(updateArgs.AddLabels) > 0 || len(updateArgs.RemoveLabels) > 0 || updateArgs.Parent != nil {
		effectiveAssignee := issue.Assignee
		if updateArgs.Assignee != nil && *updateArgs.Assignee != "" {
			effectiveAssignee = *updateArgs.Assignee
		}

		if updateArgs.Status != nil && *updateArgs.Status != string(issue.Status) {
			s.emitRichMutation(MutationEvent{
				Type:      MutationStatus,
				IssueID:   updateArgs.ID,
				Title:     issue.Title,
				Assignee:  effectiveAssignee,
				Actor:     actor,
				OldStatus: string(issue.Status),
				NewStatus: *updateArgs.Status,
			})
		} else {
			s.emitRichMutation(MutationEvent{
				Type:     MutationUpdate,
				IssueID:  updateArgs.ID,
				Title:    issue.Title,
				Assignee: effectiveAssignee,
				Actor:    actor,
			})
		}
	}

	updatedIssue, getErr := store.GetIssue(ctx, updateArgs.ID)
	if getErr != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get updated issue: %v", getErr),
		}
	}

	// Invalidate label cache if labels were modified
	if s.labelCache != nil && (len(updateArgs.SetLabels) > 0 || len(updateArgs.AddLabels) > 0 || len(updateArgs.RemoveLabels) > 0) {
		s.labelCache.InvalidateIssue(updateArgs.ID)
	}

	// Emit advice bus event if this is an advice bead (bd-z4cu.2)
	if updatedIssue != nil && updatedIssue.IssueType == types.TypeAdvice {
		labels, _ := store.GetLabels(ctx, updatedIssue.ID)
		s.emitAdviceEvent(eventbus.EventAdviceUpdated, AdviceEventPayload{
			ID:                  updatedIssue.ID,
			Title:               updatedIssue.Title,
			Labels:              labels,
			AdviceHookCommand:   updatedIssue.AdviceHookCommand,
			AdviceHookTrigger:   updatedIssue.AdviceHookTrigger,
			AdviceHookTimeout:   updatedIssue.AdviceHookTimeout,
			AdviceHookOnFailure: updatedIssue.AdviceHookOnFailure,
		})
	}

	data, _ := json.Marshal(updatedIssue)
	return Response{
		Success: true,
		Data:    data,
	}
}

// handleUpdateWithComment handles atomic update + comment in a single transaction.
// This ensures that either both the update and comment succeed, or neither does.
func (s *Server) handleUpdateWithComment(req *Request) Response {
	var args UpdateWithCommentArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid update_with_comment args: %v", err),
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	// Validate issue exists and is not a template
	issue, err := store.GetIssue(ctx, args.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get issue: %v", err),
		}
	}
	if issue == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("issue %s not found", args.ID),
		}
	}
	if issue.IsTemplate {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("cannot update template %s: templates are read-only", args.ID),
		}
	}

	actor := s.reqActor(req)
	commentAuthor := args.CommentAuthor
	if commentAuthor == "" {
		commentAuthor = actor
	}

	// Build updates map from args
	updates, err := updatesFromArgs(args.UpdateArgs)
	if err != nil {
		return Response{
			Success: false,
			Error:   err.Error(),
		}
	}

	// Execute update and comment atomically in a transaction
	var updatedIssue *types.Issue
	err = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Apply field updates if any
		if len(updates) > 0 {
			if err := tx.UpdateIssue(ctx, args.ID, updates, actor); err != nil {
				return fmt.Errorf("failed to update issue: %w", err)
			}
		}

		// Handle label operations within transaction
		if len(args.SetLabels) > 0 {
			currentLabels, err := tx.GetLabels(ctx, args.ID)
			if err != nil {
				return fmt.Errorf("failed to get current labels: %w", err)
			}
			for _, label := range currentLabels {
				if err := tx.RemoveLabel(ctx, args.ID, label, actor); err != nil {
					return fmt.Errorf("failed to remove label %s: %w", label, err)
				}
			}
			for _, label := range args.SetLabels {
				if err := tx.AddLabel(ctx, args.ID, label, actor); err != nil {
					return fmt.Errorf("failed to set label %s: %w", label, err)
				}
			}
		}

		for _, label := range args.AddLabels {
			if err := tx.AddLabel(ctx, args.ID, label, actor); err != nil {
				return fmt.Errorf("failed to add label %s: %w", label, err)
			}
		}

		for _, label := range args.RemoveLabels {
			if err := tx.RemoveLabel(ctx, args.ID, label, actor); err != nil {
				return fmt.Errorf("failed to remove label %s: %w", label, err)
			}
		}

		// Add comment if provided
		if args.CommentText != "" {
			if err := tx.AddComment(ctx, args.ID, commentAuthor, args.CommentText); err != nil {
				return fmt.Errorf("failed to add comment: %w", err)
			}
		}

		// Get the updated issue for return
		updatedIssue, err = tx.GetIssue(ctx, args.ID)
		if err != nil {
			return fmt.Errorf("failed to get updated issue: %w", err)
		}

		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   err.Error(),
		}
	}

	// Emit mutation events (outside transaction)
	if len(updates) > 0 || len(args.SetLabels) > 0 || len(args.AddLabels) > 0 || len(args.RemoveLabels) > 0 {
		effectiveAssignee := issue.Assignee
		if args.Assignee != nil && *args.Assignee != "" {
			effectiveAssignee = *args.Assignee
		}

		if args.Status != nil && *args.Status != string(issue.Status) {
			s.emitRichMutation(MutationEvent{
				Type:      MutationStatus,
				IssueID:   args.ID,
				Title:     issue.Title,
				Assignee:  effectiveAssignee,
				Actor:     actor,
				OldStatus: string(issue.Status),
				NewStatus: *args.Status,
			})
		} else {
			s.emitRichMutation(MutationEvent{
				Type:     MutationUpdate,
				IssueID:  args.ID,
				Title:    issue.Title,
				Assignee: effectiveAssignee,
				Actor:    actor,
			})
		}
	}

	if args.CommentText != "" {
		s.emitMutation(MutationComment, args.ID, issue.Title, issue.Assignee)
	}

	// Invalidate label cache if labels were modified
	if s.labelCache != nil && (len(args.SetLabels) > 0 || len(args.AddLabels) > 0 || len(args.RemoveLabels) > 0) {
		s.labelCache.InvalidateIssue(args.ID)
	}

	data, _ := json.Marshal(updatedIssue)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleClose(req *Request) Response {
	var closeArgs CloseArgs
	if err := json.Unmarshal(req.Args, &closeArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid close args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Check if issue is a template (beads-1ra): templates are read-only
	issue, err := store.GetIssue(ctx, closeArgs.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get issue: %v", err),
		}
	}
	if issue != nil && issue.IsTemplate {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("cannot close template %s: templates are read-only", closeArgs.ID),
		}
	}

	// Check if issue has open blockers (GH#962)
	if !closeArgs.Force {
		blocked, blockers, err := store.IsBlocked(ctx, closeArgs.ID)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to check blockers: %v", err),
			}
		}
		if blocked && len(blockers) > 0 {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("cannot close %s: blocked by open issues %v (use --force to override)", closeArgs.ID, blockers),
			}
		}
	}

	// Capture old status for rich mutation event
	oldStatus := ""
	if issue != nil {
		oldStatus = string(issue.Status)
	}

	if err := store.CloseIssue(ctx, closeArgs.ID, closeArgs.Reason, s.reqActor(req), closeArgs.Session); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to close issue: %v", err),
		}
	}

	// Emit rich status change event for event-driven daemon
	s.emitRichMutation(MutationEvent{
		Type:      MutationStatus,
		IssueID:   closeArgs.ID,
		Title:     issue.Title,
		Assignee:  issue.Assignee,
		OldStatus: oldStatus,
		NewStatus: "closed",
	})

	// Emit advice.deleted bus event when closing an advice bead (bd-z4cu.2)
	if issue != nil && issue.IssueType == types.TypeAdvice {
		labels, _ := store.GetLabels(ctx, issue.ID)
		s.emitAdviceEvent(eventbus.EventAdviceDeleted, AdviceEventPayload{
			ID:                  issue.ID,
			Title:               issue.Title,
			Labels:              labels,
			AdviceHookCommand:   issue.AdviceHookCommand,
			AdviceHookTrigger:   issue.AdviceHookTrigger,
			AdviceHookTimeout:   issue.AdviceHookTimeout,
			AdviceHookOnFailure: issue.AdviceHookOnFailure,
		})
	}

	closedIssue, _ := store.GetIssue(ctx, closeArgs.ID)

	// If SuggestNext is requested, find newly unblocked issues (GH#679)
	if closeArgs.SuggestNext {
		unblocked, err := store.GetNewlyUnblockedByClose(ctx, closeArgs.ID)
		if err != nil {
			// Non-fatal: still return the closed issue
			unblocked = nil
		}
		result := CloseResult{
			Closed:    closedIssue,
			Unblocked: unblocked,
		}
		data, _ := json.Marshal(result)
		return Response{
			Success: true,
			Data:    data,
		}
	}

	// Backward compatible: just return the closed issue
	data, _ := json.Marshal(closedIssue)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleDelete(req *Request) Response {
	var deleteArgs DeleteArgs
	if err := json.Unmarshal(req.Args, &deleteArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid delete args: %v", err),
		}
	}

	// Validate that we have issue IDs to delete
	if len(deleteArgs.IDs) == 0 {
		return Response{
			Success: false,
			Error:   "no issue IDs provided for deletion",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Check WispStore for wisp IDs - handle them separately
	if s.wispStore != nil {
		var wispIDs, regularIDs []string
		for _, id := range deleteArgs.IDs {
			if isWispID(id) {
				wispIDs = append(wispIDs, id)
			} else {
				regularIDs = append(regularIDs, id)
			}
		}

		// Delete wisps from WispStore
		for _, wispID := range wispIDs {
			if err := s.wispStore.Delete(ctx, wispID); err != nil {
				// Wisp not found is not an error - might already be deleted
				if !strings.Contains(err.Error(), "not found") {
					return Response{
						Success: false,
						Error:   fmt.Sprintf("failed to delete wisp %s: %v", wispID, err),
					}
				}
			}
			s.emitMutation(MutationDelete, wispID, "", "")
		}

		// If all IDs were wisps, return success
		if len(regularIDs) == 0 {
			data, _ := json.Marshal(map[string]interface{}{
				"deleted_count": len(wispIDs),
				"total_count":   len(deleteArgs.IDs),
			})
			return Response{
				Success: true,
				Data:    data,
			}
		}

		// Continue with regular IDs
		deleteArgs.IDs = regularIDs
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	// Use batch delete for cascade/multi-issue operations on SQLite storage
	// This handles cascade delete properly by expanding dependents recursively
	// For simple single-issue deletes, use the direct path to preserve custom reason
	if sqlStore, ok := store.(*sqlite.SQLiteStorage); ok {
		// Use batch delete if: cascade enabled, force enabled, multiple IDs, or dry-run
		useBatchDelete := deleteArgs.Cascade || deleteArgs.Force || len(deleteArgs.IDs) > 1 || deleteArgs.DryRun
		if useBatchDelete {
			// Pre-fetch advice issues for bus events before deletion (bd-z4cu.2)
			var advicePayloads []AdviceEventPayload
			for _, id := range deleteArgs.IDs {
				if iss, err := store.GetIssue(ctx, id); err == nil && iss != nil && iss.IssueType == types.TypeAdvice {
					labels, _ := store.GetLabels(ctx, id)
					advicePayloads = append(advicePayloads, AdviceEventPayload{
						ID:                  iss.ID,
						Title:               iss.Title,
						Labels:              labels,
						AdviceHookCommand:   iss.AdviceHookCommand,
						AdviceHookTrigger:   iss.AdviceHookTrigger,
						AdviceHookTimeout:   iss.AdviceHookTimeout,
						AdviceHookOnFailure: iss.AdviceHookOnFailure,
					})
				}
			}

			result, err := sqlStore.DeleteIssues(ctx, deleteArgs.IDs, deleteArgs.Cascade, deleteArgs.Force, deleteArgs.DryRun)
			if err != nil {
				return Response{
					Success: false,
					Error:   fmt.Sprintf("delete failed: %v", err),
				}
			}

			// Emit mutation events for deleted issues
			if !deleteArgs.DryRun {
				for _, issueID := range deleteArgs.IDs {
					s.emitMutation(MutationDelete, issueID, "", "")
				}
				// Emit advice.deleted bus events for advice beads (bd-z4cu.2)
				for _, payload := range advicePayloads {
					s.emitAdviceEvent(eventbus.EventAdviceDeleted, payload)
				}
			}

			// Build response
			responseData := map[string]interface{}{
				"deleted_count": result.DeletedCount,
				"total_count":   len(deleteArgs.IDs),
			}
			if deleteArgs.DryRun {
				responseData["dry_run"] = true
				responseData["issue_count"] = result.DeletedCount
			}
			if result.DependenciesCount > 0 {
				responseData["dependencies_removed"] = result.DependenciesCount
			}
			if result.LabelsCount > 0 {
				responseData["labels_removed"] = result.LabelsCount
			}
			if result.EventsCount > 0 {
				responseData["events_removed"] = result.EventsCount
			}
			if len(result.OrphanedIssues) > 0 {
				responseData["orphaned_issues"] = result.OrphanedIssues
			}

			data, _ := json.Marshal(responseData)
			return Response{
				Success: true,
				Data:    data,
			}
		}
	}

	// Simple single-issue delete path (preserves custom reason)
	// DryRun mode: just return what would be deleted
	if deleteArgs.DryRun {
		data, _ := json.Marshal(map[string]interface{}{
			"dry_run":     true,
			"issue_count": len(deleteArgs.IDs),
			"issues":      deleteArgs.IDs,
		})
		return Response{
			Success: true,
			Data:    data,
		}
	}

	deletedCount := 0
	errors := make([]string, 0)

	// Delete each issue
	for _, issueID := range deleteArgs.IDs {
		// Verify issue exists before deleting
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", issueID, err))
			continue
		}
		if issue == nil {
			errors = append(errors, fmt.Sprintf("%s: not found", issueID))
			continue
		}

		// Check if issue is a template (beads-1ra): templates are read-only
		if issue.IsTemplate {
			errors = append(errors, fmt.Sprintf("%s: cannot delete template (templates are read-only)", issueID))
			continue
		}

		// Create tombstone instead of hard delete (unless HardDelete is requested)
		// This preserves deletion history and prevents resurrection during sync
		if deleteArgs.HardDelete {
			// Hard delete requested - skip tombstones, do permanent delete
			// WARNING: This bypasses sync safety. Use only when certain issues won't resurrect.
			if err := store.DeleteIssue(ctx, issueID); err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", issueID, err))
				continue
			}
		} else {
			type tombstoner interface {
				CreateTombstone(ctx context.Context, id string, actor string, reason string) error
			}
			if t, ok := store.(tombstoner); ok {
				reason := deleteArgs.Reason
				if reason == "" {
					reason = "deleted via daemon"
				}
				if err := t.CreateTombstone(ctx, issueID, "daemon", reason); err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", issueID, err))
					continue
				}
			} else {
				// Fallback to hard delete if CreateTombstone not available
				if err := store.DeleteIssue(ctx, issueID); err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", issueID, err))
					continue
				}
			}
		}

		// Emit mutation event for event-driven daemon
		s.emitMutation(MutationDelete, issueID, issue.Title, issue.Assignee)

		// Emit advice.deleted bus event if this is an advice bead (bd-z4cu.2)
		if issue.IssueType == types.TypeAdvice {
			labels, _ := store.GetLabels(ctx, issueID)
			s.emitAdviceEvent(eventbus.EventAdviceDeleted, AdviceEventPayload{
				ID:                  issue.ID,
				Title:               issue.Title,
				Labels:              labels,
				AdviceHookCommand:   issue.AdviceHookCommand,
				AdviceHookTrigger:   issue.AdviceHookTrigger,
				AdviceHookTimeout:   issue.AdviceHookTimeout,
				AdviceHookOnFailure: issue.AdviceHookOnFailure,
			})
		}

		deletedCount++
	}

	// Build response
	result := map[string]interface{}{
		"deleted_count": deletedCount,
		"total_count":   len(deleteArgs.IDs),
	}

	if len(errors) > 0 {
		result["errors"] = errors
		if deletedCount == 0 {
			// All deletes failed
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to delete all issues: %v", errors),
			}
		}
		// Partial success
		result["partial_success"] = true
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleRename(req *Request) Response {
	var renameArgs RenameArgs
	if err := json.Unmarshal(req.Args, &renameArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid rename args: %v", err),
		}
	}

	// Validate IDs
	if renameArgs.OldID == "" || renameArgs.NewID == "" {
		return Response{
			Success: false,
			Error:   "old_id and new_id are required",
		}
	}
	if renameArgs.OldID == renameArgs.NewID {
		return Response{
			Success: false,
			Error:   "old and new IDs are the same",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	// Check if old issue exists
	oldIssue, err := store.GetIssue(ctx, renameArgs.OldID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get issue %s: %v", renameArgs.OldID, err),
		}
	}
	if oldIssue == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("issue %s not found", renameArgs.OldID),
		}
	}

	// Check if new ID already exists
	existing, err := store.GetIssue(ctx, renameArgs.NewID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to check for existing issue: %v", err),
		}
	}
	if existing != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("issue %s already exists", renameArgs.NewID),
		}
	}

	// Update the issue ID
	oldIssue.ID = renameArgs.NewID
	actor := req.Actor
	if actor == "" {
		actor = "daemon"
	}
	if err := store.UpdateIssueID(ctx, renameArgs.OldID, renameArgs.NewID, oldIssue, actor); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to rename issue: %v", err),
		}
	}

	// Update text references in other issues
	referencesUpdated := s.updateReferencesInAllIssues(ctx, store, renameArgs.OldID, renameArgs.NewID, actor)

	// Emit mutation event
	s.emitMutation(MutationUpdate, renameArgs.NewID, oldIssue.Title, oldIssue.Assignee)

	result := RenameResult{
		OldID:            renameArgs.OldID,
		NewID:            renameArgs.NewID,
		ReferencesUpdated: referencesUpdated,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

// updateReferencesInAllIssues updates text references to the old ID in all issues
// Returns the number of issues that were updated
func (s *Server) updateReferencesInAllIssues(ctx context.Context, store storage.Storage, oldID, newID, actor string) int {
	// Get all issues
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return 0
	}

	// Pattern to match the old ID as a word boundary
	oldPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(oldID) + `\b`)
	updatedCount := 0

	for _, issue := range issues {
		if issue.ID == newID {
			continue // Skip the renamed issue itself
		}

		updated := false
		updates := make(map[string]interface{})

		// Check and update each text field
		if oldPattern.MatchString(issue.Title) {
			updates["title"] = oldPattern.ReplaceAllString(issue.Title, newID)
			updated = true
		}
		if oldPattern.MatchString(issue.Description) {
			updates["description"] = oldPattern.ReplaceAllString(issue.Description, newID)
			updated = true
		}
		if oldPattern.MatchString(issue.Design) {
			updates["design"] = oldPattern.ReplaceAllString(issue.Design, newID)
			updated = true
		}
		if oldPattern.MatchString(issue.Notes) {
			updates["notes"] = oldPattern.ReplaceAllString(issue.Notes, newID)
			updated = true
		}
		if oldPattern.MatchString(issue.AcceptanceCriteria) {
			updates["acceptance_criteria"] = oldPattern.ReplaceAllString(issue.AcceptanceCriteria, newID)
			updated = true
		}

		if updated {
			if err := store.UpdateIssue(ctx, issue.ID, updates, actor); err == nil {
				updatedCount++
			}
		}
	}

	return updatedCount
}

func (s *Server) handleList(req *Request) Response {
	var listArgs ListArgs
	if err := json.Unmarshal(req.Args, &listArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid list args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	// Cross-rig listing: open target rig's storage (bd-rl6y)
	var useTargetStore bool
	if listArgs.TargetRig != "" {
		targetBeadsDir, _, err := resolveTargetRig(req, listArgs.TargetRig)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to resolve target rig: %v", err),
			}
		}
		targetStore, err := factory.NewFromConfig(context.Background(), targetBeadsDir)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to open rig %q database: %v", listArgs.TargetRig, err),
			}
		}
		defer targetStore.Close()
		store = targetStore
		useTargetStore = true
	}

	filter := types.IssueFilter{
		Limit: listArgs.Limit,
	}
	
	// Normalize status: treat "" or "all" as unset (no filter)
	if listArgs.Status != "" && listArgs.Status != "all" {
		status := types.Status(listArgs.Status)
		filter.Status = &status
	}
	
	if listArgs.IssueType != "" {
		issueType := types.IssueType(listArgs.IssueType)
		filter.IssueType = &issueType
	}
	if listArgs.Assignee != "" {
		filter.Assignee = &listArgs.Assignee
	}
	if listArgs.Priority != nil {
		filter.Priority = listArgs.Priority
	}
	
	// Normalize and apply label filters
	labels := util.NormalizeLabels(listArgs.Labels)
	labelsAny := util.NormalizeLabels(listArgs.LabelsAny)
	// Support both old single Label and new Labels array (backward compat)
	if len(labels) > 0 {
		filter.Labels = labels
	} else if listArgs.Label != "" {
		filter.Labels = []string{strings.TrimSpace(listArgs.Label)}
	}
	if len(labelsAny) > 0 {
		filter.LabelsAny = labelsAny
	}
	if len(listArgs.IDs) > 0 {
		ids := util.NormalizeLabels(listArgs.IDs)
		if len(ids) > 0 {
			filter.IDs = ids
		}
	}
	
	// Pattern matching
	filter.TitleContains = listArgs.TitleContains
	filter.DescriptionContains = listArgs.DescriptionContains
	filter.NotesContains = listArgs.NotesContains
	
	// Date ranges - use parseTimeRPC helper for flexible formats
	if listArgs.CreatedAfter != "" {
		t, err := parseTimeRPC(listArgs.CreatedAfter)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --created-after date: %v", err),
			}
		}
		filter.CreatedAfter = &t
	}
	if listArgs.CreatedBefore != "" {
		t, err := parseTimeRPC(listArgs.CreatedBefore)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --created-before date: %v", err),
			}
		}
		filter.CreatedBefore = &t
	}
	if listArgs.UpdatedAfter != "" {
		t, err := parseTimeRPC(listArgs.UpdatedAfter)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --updated-after date: %v", err),
			}
		}
		filter.UpdatedAfter = &t
	}
	if listArgs.UpdatedBefore != "" {
		t, err := parseTimeRPC(listArgs.UpdatedBefore)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --updated-before date: %v", err),
			}
		}
		filter.UpdatedBefore = &t
	}
	if listArgs.ClosedAfter != "" {
		t, err := parseTimeRPC(listArgs.ClosedAfter)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --closed-after date: %v", err),
			}
		}
		filter.ClosedAfter = &t
	}
	if listArgs.ClosedBefore != "" {
		t, err := parseTimeRPC(listArgs.ClosedBefore)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --closed-before date: %v", err),
			}
		}
		filter.ClosedBefore = &t
	}
	
	// Empty/null checks
	filter.EmptyDescription = listArgs.EmptyDescription
	filter.NoAssignee = listArgs.NoAssignee
	filter.NoLabels = listArgs.NoLabels
	
	// Priority range
	filter.PriorityMin = listArgs.PriorityMin
	filter.PriorityMax = listArgs.PriorityMax

	// Pinned filtering
	filter.Pinned = listArgs.Pinned

	// Template filtering: exclude templates by default
	if !listArgs.IncludeTemplates {
		isTemplate := false
		filter.IsTemplate = &isTemplate
	}

	// Parent filtering
	if listArgs.ParentID != "" {
		filter.ParentID = &listArgs.ParentID
	}

	// Ephemeral filtering
	filter.Ephemeral = listArgs.Ephemeral

	// Molecule type filtering
	if listArgs.MolType != "" {
		molType := types.MolType(listArgs.MolType)
		filter.MolType = &molType
	}

	// Status exclusion (for default non-closed behavior, GH#788)
	if len(listArgs.ExcludeStatus) > 0 {
		for _, s := range listArgs.ExcludeStatus {
			filter.ExcludeStatus = append(filter.ExcludeStatus, types.Status(s))
		}
	}

	// Type exclusion (for hiding internal types like gates, bd-7zka.2)
	if len(listArgs.ExcludeTypes) > 0 {
		for _, t := range listArgs.ExcludeTypes {
			filter.ExcludeTypes = append(filter.ExcludeTypes, types.IssueType(t))
		}
	}

	// Time-based scheduling filters (GH#820)
	filter.Deferred = listArgs.Deferred
	if listArgs.DeferAfter != "" {
		t, err := parseTimeRPC(listArgs.DeferAfter)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --defer-after date: %v", err),
			}
		}
		filter.DeferAfter = &t
	}
	if listArgs.DeferBefore != "" {
		t, err := parseTimeRPC(listArgs.DeferBefore)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --defer-before date: %v", err),
			}
		}
		filter.DeferBefore = &t
	}
	if listArgs.DueAfter != "" {
		t, err := parseTimeRPC(listArgs.DueAfter)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --due-after date: %v", err),
			}
		}
		filter.DueAfter = &t
	}
	if listArgs.DueBefore != "" {
		t, err := parseTimeRPC(listArgs.DueBefore)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --due-before date: %v", err),
			}
		}
		filter.DueBefore = &t
	}
	filter.Overdue = listArgs.Overdue

	// Guard against excessive ID lists to avoid SQLite parameter limits
	const maxIDs = 1000
	if len(filter.IDs) > maxIDs {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("--id flag supports at most %d issue IDs, got %d", maxIDs, len(filter.IDs)),
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Collect wisps from WispStore if available and not explicitly filtering non-ephemeral
	// Skip wisps for cross-rig queries (wisps are rig-local) (bd-rl6y)
	var wisps []*types.Issue
	shouldIncludeWisps := !useTargetStore && (filter.Ephemeral == nil || (filter.Ephemeral != nil && *filter.Ephemeral))
	if s.wispStore != nil && shouldIncludeWisps {
		wispList, err := s.wispStore.List(ctx, filter)
		if err == nil {
			wisps = wispList
		}
	}

	// If explicitly filtering for ephemeral only, return just wisps
	if filter.Ephemeral != nil && *filter.Ephemeral && s.wispStore != nil {
		issuesWithCounts := make([]*types.IssueWithCounts, len(wisps))
		for i, wisp := range wisps {
			issuesWithCounts[i] = &types.IssueWithCounts{
				Issue:           wisp,
				DependencyCount: 0,
				DependentCount:  0,
			}
		}
		data, _ := json.Marshal(issuesWithCounts)
		return Response{
			Success: true,
			Data:    data,
		}
	}

	issues, err := store.SearchIssues(ctx, listArgs.Query, filter)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to list issues: %v", err),
		}
	}

	// Populate labels for each issue (batch query instead of N+1)
	issueIDs := make([]string, len(issues))
	var epicIDs []string
	for i, issue := range issues {
		issueIDs[i] = issue.ID
		if issue.IssueType == types.TypeEpic {
			epicIDs = append(epicIDs, issue.ID)
		}
	}
	// For cross-rig queries, use target store directly instead of label cache (bd-rl6y)
	var labelsMap map[string][]string
	if useTargetStore {
		labelsMap, _ = store.GetLabelsForIssues(ctx, issueIDs)
	} else {
		labelsMap, _ = s.labelCache.GetLabelsForIssues(ctx, issueIDs)
	}
	for _, issue := range issues {
		issue.Labels = labelsMap[issue.ID]
	}
	depCounts, _ := store.GetDependencyCounts(ctx, issueIDs)

	// Populate dependencies for listed issues only (targeted query instead of full table scan)
	allDeps, _ := store.GetDependencyRecordsForIssues(ctx, issueIDs)
	for _, issue := range issues {
		issue.Dependencies = allDeps[issue.ID]
	}

	// Get epic progress in bulk for epics
	epicProgress, _ := store.GetEpicProgress(ctx, epicIDs)
	if epicProgress == nil {
		epicProgress = make(map[string]*types.EpicProgress)
	}

	// Build response with counts
	totalCount := len(issues) + len(wisps)
	issuesWithCounts := make([]*types.IssueWithCounts, 0, totalCount)

	// Add wisps first (they don't have dependency counts)
	for _, wisp := range wisps {
		issuesWithCounts = append(issuesWithCounts, &types.IssueWithCounts{
			Issue:           wisp,
			DependencyCount: 0,
			DependentCount:  0,
		})
	}

	// Add regular issues with counts
	for _, issue := range issues {
		counts := depCounts[issue.ID]
		if counts == nil {
			counts = &types.DependencyCounts{DependencyCount: 0, DependentCount: 0}
		}
		iwc := &types.IssueWithCounts{
			Issue:           issue,
			DependencyCount: counts.DependencyCount,
			DependentCount:  counts.DependentCount,
		}
		// Add epic progress if this is an epic
		if progress, ok := epicProgress[issue.ID]; ok {
			iwc.EpicTotalChildren = progress.TotalChildren
			iwc.EpicClosedChildren = progress.ClosedChildren
		}
		issuesWithCounts = append(issuesWithCounts, iwc)
	}

	data, _ := json.Marshal(issuesWithCounts)
	return Response{
		Success: true,
		Data:    data,
	}
}

// handleListWatch implements long-polling watch mode for bd list --watch (bd-la75)
// It waits for mutations since the given timestamp and returns updated issue list.
func (s *Server) handleListWatch(req *Request) Response {
	var args ListWatchArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid list watch args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	// Set default and max timeout
	timeoutMs := args.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 30000 // 30 seconds default
	}
	if timeoutMs > 60000 {
		timeoutMs = 60000 // 60 seconds max
	}

	// If Since > 0, wait for mutations newer than Since
	if args.Since > 0 {
		deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
		pollInterval := 100 * time.Millisecond

		for time.Now().Before(deadline) {
			// Check for mutations newer than Since
			mutations := s.GetRecentMutations(args.Since)
			if len(mutations) > 0 {
				// Found mutations, return updated list
				break
			}

			// No mutations yet, wait a bit
			remaining := time.Until(deadline)
			if remaining < pollInterval {
				time.Sleep(remaining)
			} else {
				time.Sleep(pollInterval)
			}
		}
	}

	// Build filter from args (same as handleList)
	filter := types.IssueFilter{
		Limit: args.Limit,
	}

	if args.Status != "" && args.Status != "all" {
		status := types.Status(args.Status)
		filter.Status = &status
	}
	if args.IssueType != "" {
		issueType := types.IssueType(args.IssueType)
		filter.IssueType = &issueType
	}
	if args.Assignee != "" {
		filter.Assignee = &args.Assignee
	}
	if args.Priority != nil {
		filter.Priority = args.Priority
	}

	// Normalize and apply label filters
	labels := util.NormalizeLabels(args.Labels)
	labelsAny := util.NormalizeLabels(args.LabelsAny)
	if len(labels) > 0 {
		filter.Labels = labels
	} else if args.Label != "" {
		filter.Labels = []string{strings.TrimSpace(args.Label)}
	}
	if len(labelsAny) > 0 {
		filter.LabelsAny = labelsAny
	}
	if len(args.IDs) > 0 {
		ids := util.NormalizeLabels(args.IDs)
		if len(ids) > 0 {
			filter.IDs = ids
		}
	}

	// Pattern matching
	filter.TitleContains = args.TitleContains
	filter.DescriptionContains = args.DescriptionContains
	filter.NotesContains = args.NotesContains

	// Date ranges
	if args.CreatedAfter != "" {
		if t, err := parseTimeRPC(args.CreatedAfter); err == nil {
			filter.CreatedAfter = &t
		}
	}
	if args.CreatedBefore != "" {
		if t, err := parseTimeRPC(args.CreatedBefore); err == nil {
			filter.CreatedBefore = &t
		}
	}
	if args.UpdatedAfter != "" {
		if t, err := parseTimeRPC(args.UpdatedAfter); err == nil {
			filter.UpdatedAfter = &t
		}
	}
	if args.UpdatedBefore != "" {
		if t, err := parseTimeRPC(args.UpdatedBefore); err == nil {
			filter.UpdatedBefore = &t
		}
	}
	if args.ClosedAfter != "" {
		if t, err := parseTimeRPC(args.ClosedAfter); err == nil {
			filter.ClosedAfter = &t
		}
	}
	if args.ClosedBefore != "" {
		if t, err := parseTimeRPC(args.ClosedBefore); err == nil {
			filter.ClosedBefore = &t
		}
	}

	// Empty/null checks
	filter.EmptyDescription = args.EmptyDescription
	filter.NoAssignee = args.NoAssignee
	filter.NoLabels = args.NoLabels

	// Priority range
	filter.PriorityMin = args.PriorityMin
	filter.PriorityMax = args.PriorityMax

	// Pinned
	filter.Pinned = args.Pinned

	// Templates
	if !args.IncludeTemplates {
		isTemplate := false
		filter.IsTemplate = &isTemplate
	}

	// Parent
	if args.ParentID != "" {
		filter.ParentID = &args.ParentID
	}

	// Ephemeral
	filter.Ephemeral = args.Ephemeral

	// MolType
	if args.MolType != "" {
		molType := types.MolType(args.MolType)
		filter.MolType = &molType
	}

	// Status exclusion
	if len(args.ExcludeStatus) > 0 {
		for _, s := range args.ExcludeStatus {
			filter.ExcludeStatus = append(filter.ExcludeStatus, types.Status(s))
		}
	}

	// Type exclusion
	if len(args.ExcludeTypes) > 0 {
		for _, t := range args.ExcludeTypes {
			filter.ExcludeTypes = append(filter.ExcludeTypes, types.IssueType(t))
		}
	}

	// Time-based scheduling
	filter.Deferred = args.Deferred
	if args.DeferAfter != "" {
		if t, err := parseTimeRPC(args.DeferAfter); err == nil {
			filter.DeferAfter = &t
		}
	}
	if args.DeferBefore != "" {
		if t, err := parseTimeRPC(args.DeferBefore); err == nil {
			filter.DeferBefore = &t
		}
	}
	if args.DueAfter != "" {
		if t, err := parseTimeRPC(args.DueAfter); err == nil {
			filter.DueAfter = &t
		}
	}
	if args.DueBefore != "" {
		if t, err := parseTimeRPC(args.DueBefore); err == nil {
			filter.DueBefore = &t
		}
	}
	filter.Overdue = args.Overdue

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Query issues
	issues, err := store.SearchIssues(ctx, args.Query, filter)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to list issues: %v", err),
		}
	}

	// Populate labels for each issue (batch query instead of N+1)
	if len(issues) > 0 {
		watchIssueIDs := make([]string, len(issues))
		for i, issue := range issues {
			watchIssueIDs[i] = issue.ID
		}
		labelsMap, _ := s.labelCache.GetLabelsForIssues(ctx, watchIssueIDs)
		for _, issue := range issues {
			issue.Labels = labelsMap[issue.ID]
		}
	}

	// Get current timestamp for LastMutationMs
	var lastMutationMs int64
	s.recentMutationsMu.RLock()
	if len(s.recentMutations) > 0 {
		lastMutationMs = s.recentMutations[len(s.recentMutations)-1].Timestamp.UnixMilli()
	} else {
		lastMutationMs = time.Now().UnixMilli()
	}
	s.recentMutationsMu.RUnlock()

	result := ListWatchResult{
		Issues:         issues,
		LastMutationMs: lastMutationMs,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleCount(req *Request) Response {
	var countArgs CountArgs
	if err := json.Unmarshal(req.Args, &countArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid count args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	filter := types.IssueFilter{}

	// Normalize status: treat "" or "all" as unset (no filter)
	if countArgs.Status != "" && countArgs.Status != "all" {
		status := types.Status(countArgs.Status)
		filter.Status = &status
	}

	if countArgs.IssueType != "" {
		issueType := types.IssueType(countArgs.IssueType)
		filter.IssueType = &issueType
	}
	if countArgs.Assignee != "" {
		filter.Assignee = &countArgs.Assignee
	}
	if countArgs.Priority != nil {
		filter.Priority = countArgs.Priority
	}

	// Normalize and apply label filters
	labels := util.NormalizeLabels(countArgs.Labels)
	labelsAny := util.NormalizeLabels(countArgs.LabelsAny)
	if len(labels) > 0 {
		filter.Labels = labels
	}
	if len(labelsAny) > 0 {
		filter.LabelsAny = labelsAny
	}
	if len(countArgs.IDs) > 0 {
		ids := util.NormalizeLabels(countArgs.IDs)
		if len(ids) > 0 {
			filter.IDs = ids
		}
	}

	// Pattern matching
	filter.TitleContains = countArgs.TitleContains
	filter.DescriptionContains = countArgs.DescriptionContains
	filter.NotesContains = countArgs.NotesContains

	// Date ranges - use parseTimeRPC helper for flexible formats
	if countArgs.CreatedAfter != "" {
		t, err := parseTimeRPC(countArgs.CreatedAfter)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --created-after date: %v", err),
			}
		}
		filter.CreatedAfter = &t
	}
	if countArgs.CreatedBefore != "" {
		t, err := parseTimeRPC(countArgs.CreatedBefore)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --created-before date: %v", err),
			}
		}
		filter.CreatedBefore = &t
	}
	if countArgs.UpdatedAfter != "" {
		t, err := parseTimeRPC(countArgs.UpdatedAfter)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --updated-after date: %v", err),
			}
		}
		filter.UpdatedAfter = &t
	}
	if countArgs.UpdatedBefore != "" {
		t, err := parseTimeRPC(countArgs.UpdatedBefore)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --updated-before date: %v", err),
			}
		}
		filter.UpdatedBefore = &t
	}
	if countArgs.ClosedAfter != "" {
		t, err := parseTimeRPC(countArgs.ClosedAfter)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --closed-after date: %v", err),
			}
		}
		filter.ClosedAfter = &t
	}
	if countArgs.ClosedBefore != "" {
		t, err := parseTimeRPC(countArgs.ClosedBefore)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid --closed-before date: %v", err),
			}
		}
		filter.ClosedBefore = &t
	}

	// Empty/null checks
	filter.EmptyDescription = countArgs.EmptyDescription
	filter.NoAssignee = countArgs.NoAssignee
	filter.NoLabels = countArgs.NoLabels

	// Priority range
	filter.PriorityMin = countArgs.PriorityMin
	filter.PriorityMax = countArgs.PriorityMax

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	issues, err := store.SearchIssues(ctx, countArgs.Query, filter)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to count issues: %v", err),
		}
	}

	// If no grouping, just return the count
	if countArgs.GroupBy == "" {
		type CountResult struct {
			Count int `json:"count"`
		}
		data, _ := json.Marshal(CountResult{Count: len(issues)})
		return Response{
			Success: true,
			Data:    data,
		}
	}

	// Group by the specified field
	type GroupCount struct {
		Group string `json:"group"`
		Count int    `json:"count"`
	}

	counts := make(map[string]int)

	// For label grouping, fetch all labels in one query to avoid N+1
	var labelsMap map[string][]string
	if countArgs.GroupBy == "label" {
		issueIDs := make([]string, len(issues))
		for i, issue := range issues {
			issueIDs[i] = issue.ID
		}
		var err error
		labelsMap, err = s.labelCache.GetLabelsForIssues(ctx, issueIDs)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to get labels: %v", err),
			}
		}
	}

	for _, issue := range issues {
		var groupKey string
		switch countArgs.GroupBy {
		case "status":
			groupKey = string(issue.Status)
		case "priority":
			groupKey = fmt.Sprintf("P%d", issue.Priority)
		case "type":
			groupKey = string(issue.IssueType)
		case "assignee":
			if issue.Assignee == "" {
				groupKey = "(unassigned)"
			} else {
				groupKey = issue.Assignee
			}
		case "label":
			// For labels, count each label separately
			labels := labelsMap[issue.ID]
			if len(labels) > 0 {
				for _, label := range labels {
					counts[label]++
				}
				continue
			} else {
				groupKey = "(no labels)"
			}
		default:
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid group_by value: %s (must be one of: status, priority, type, assignee, label)", countArgs.GroupBy),
			}
		}
		counts[groupKey]++
	}

	// Convert map to sorted slice
	groups := make([]GroupCount, 0, len(counts))
	for group, count := range counts {
		groups = append(groups, GroupCount{Group: group, Count: count})
	}

	type GroupedCountResult struct {
		Total  int          `json:"total"`
		Groups []GroupCount `json:"groups"`
	}

	result := GroupedCountResult{
		Total:  len(issues),
		Groups: groups,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleResolveID(req *Request) Response {
	var args ResolveIDArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid resolve_id args: %v", err),
		}
	}

	if s.storage == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Check WispStore first for wisp IDs (bd-k59w)
	if s.wispStore != nil && isWispID(args.ID) {
		issue, err := s.wispStore.Get(ctx, args.ID)
		if err == nil && issue != nil {
			data, _ := json.Marshal(issue.ID)
			return Response{
				Success: true,
				Data:    data,
			}
		}
		// Fall through to regular storage if not found in wispStore
	}

	resolvedID, err := utils.ResolvePartialID(ctx, s.storage, args.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to resolve ID: %v", err),
		}
	}

	data, _ := json.Marshal(resolvedID)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleShow(req *Request) Response {
	var showArgs ShowArgs
	if err := json.Unmarshal(req.Args, &showArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid show args: %v", err),
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Check WispStore first for wisp IDs or if storage is not available
	if s.wispStore != nil && isWispID(showArgs.ID) {
		issue, err := s.wispStore.Get(ctx, showArgs.ID)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to get wisp: %v", err),
			}
		}
		if issue != nil {
			// Found in WispStore - return simplified details (no deps/labels/comments)
			details := &types.IssueDetails{
				Issue:  *issue,
				Labels: issue.Labels, // Labels are stored directly on wisp
			}
			data, _ := json.Marshal(details)
			return Response{
				Success: true,
				Data:    data,
			}
		}
		// Not found in WispStore, fall through to regular storage
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	issue, err := store.GetIssue(ctx, showArgs.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get issue: %v", err),
		}
	}
	if issue == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("issue not found: %s", showArgs.ID),
		}
	}

	// Populate labels, dependencies (with metadata), and dependents (with metadata)
	labels, _ := store.GetLabels(ctx, issue.ID)

	// Get dependencies and dependents with metadata (including dependency type)
	// These methods are on the Storage interface, no type assertion needed
	deps, _ := store.GetDependenciesWithMetadata(ctx, issue.ID)
	dependents, _ := store.GetDependentsWithMetadata(ctx, issue.ID)

	// Fetch comments
	comments, _ := store.GetIssueComments(ctx, issue.ID)

	// Create detailed response with related data
	details := &types.IssueDetails{
		Issue:        *issue,
		Labels:       labels,
		Dependencies: deps,
		Dependents:   dependents,
		Comments:     comments,
	}

	data, _ := json.Marshal(details)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleReady(req *Request) Response {
	var readyArgs ReadyArgs
	if err := json.Unmarshal(req.Args, &readyArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid ready args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	wf := types.WorkFilter{
		// Leave Status empty to get both 'open' and 'in_progress' (GH#5aml)
		Type:            readyArgs.Type,
		Priority:        readyArgs.Priority,
		Unassigned:      readyArgs.Unassigned,
		Limit:           readyArgs.Limit,
		SortPolicy:      types.SortPolicy(readyArgs.SortPolicy),
		Labels:          util.NormalizeLabels(readyArgs.Labels),
		LabelsAny:       util.NormalizeLabels(readyArgs.LabelsAny),
		IncludeDeferred: readyArgs.IncludeDeferred, // GH#820
	}
	if readyArgs.Assignee != "" && !readyArgs.Unassigned {
		wf.Assignee = &readyArgs.Assignee
	}
	if readyArgs.ParentID != "" {
		wf.ParentID = &readyArgs.ParentID
	}
	if readyArgs.MolType != "" {
		molType := types.MolType(readyArgs.MolType)
		wf.MolType = &molType
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	issues, err := store.GetReadyWork(ctx, wf)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get ready work: %v", err),
		}
	}

	data, _ := json.Marshal(issues)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleBlocked(req *Request) Response {
	var blockedArgs BlockedArgs
	if err := json.Unmarshal(req.Args, &blockedArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid blocked args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	var wf types.WorkFilter
	if blockedArgs.ParentID != "" {
		wf.ParentID = &blockedArgs.ParentID
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	blocked, err := store.GetBlockedIssues(ctx, wf)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get blocked issues: %v", err),
		}
	}

	data, _ := json.Marshal(blocked)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleStale(req *Request) Response {
	var staleArgs StaleArgs
	if err := json.Unmarshal(req.Args, &staleArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid stale args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	filter := types.StaleFilter{
		Days:   staleArgs.Days,
		Status: staleArgs.Status,
		Limit:  staleArgs.Limit,
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	issues, err := store.GetStaleIssues(ctx, filter)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get stale issues: %v", err),
		}
	}

	data, _ := json.Marshal(issues)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleStats(req *Request) Response {
	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	stats, err := store.GetStatistics(ctx)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get statistics: %v", err),
		}
	}

	data, _ := json.Marshal(stats)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleEpicStatus(req *Request) Response {
	var epicArgs EpicStatusArgs
	if err := json.Unmarshal(req.Args, &epicArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid epic status args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	epics, err := store.GetEpicsEligibleForClosure(ctx)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get epic status: %v", err),
		}
	}

	if epicArgs.EligibleOnly {
		filtered := []*types.EpicStatus{}
		for _, epic := range epics {
			if epic.EligibleForClose {
				filtered = append(filtered, epic)
			}
		}
		epics = filtered
	}

	data, err := json.Marshal(epics)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to marshal epics: %v", err),
		}
	}

	return Response{
		Success: true,
		Data:    data,
	}
}

// handleGetConfig retrieves a config value from the database
func (s *Server) handleGetConfig(req *Request) Response {
	var args GetConfigArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid get_config args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Get config value from database
	value, err := store.GetConfig(ctx, args.Key)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get config %q: %v", args.Key, err),
		}
	}

	result := GetConfigResponse{
		Key:   args.Key,
		Value: value,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

// handleConfigSet sets a config value in the database (bd-wmil)
func (s *Server) handleConfigSet(req *Request) Response {
	var args ConfigSetArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid config_set args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Set config value in database
	if err := store.SetConfig(ctx, args.Key, args.Value); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to set config %q: %v", args.Key, err),
		}
	}

	result := ConfigSetResponse{
		Key:   args.Key,
		Value: args.Value,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

// handleConfigList lists all config values from the database (bd-wmil)
func (s *Server) handleConfigList(req *Request) Response {
	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Get all config values from database
	config, err := store.GetAllConfig(ctx)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to list config: %v", err),
		}
	}

	result := ConfigListResponse{
		Config: config,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

// handleConfigUnset deletes a config value from the database (bd-wmil)
func (s *Server) handleConfigUnset(req *Request) Response {
	var args ConfigUnsetArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid config_unset args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Delete config value from database
	if err := store.DeleteConfig(ctx, args.Key); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to unset config %q: %v", args.Key, err),
		}
	}

	result := ConfigUnsetResponse{
		Key: args.Key,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

// Gate handlers

func (s *Server) handleGateCreate(req *Request) Response {
	var args GateCreateArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid gate create args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	now := time.Now()

	// Create gate issue
	gate := &types.Issue{
		Title:     args.Title,
		IssueType: "gate",
		Status:    types.StatusOpen,
		Priority:  1, // Gates are typically high priority
		Assignee:  "deacon/",
		Ephemeral:      true, // Gates are wisps (ephemeral)
		AwaitType: args.AwaitType,
		AwaitID:   args.AwaitID,
		Timeout:   args.Timeout,
		Waiters:   args.Waiters,
		CreatedAt: now,
		UpdatedAt: now,
	}
	gate.ContentHash = gate.ComputeContentHash()

	if err := store.CreateIssue(ctx, gate, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to create gate: %v", err),
		}
	}

	// Emit mutation event
	s.emitMutation(MutationCreate, gate.ID, gate.Title, gate.Assignee)

	data, _ := json.Marshal(GateCreateResult{ID: gate.ID})
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleGateList(req *Request) Response {
	var args GateListArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid gate list args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Build filter for gates
	gateType := types.IssueType("gate")
	filter := types.IssueFilter{
		IssueType: &gateType,
	}
	// By default, exclude closed gates (consistent with CLI behavior)
	if !args.All {
		filter.ExcludeStatus = []types.Status{types.StatusClosed}
	}

	gates, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to list gates: %v", err),
		}
	}

	data, _ := json.Marshal(gates)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleGateShow(req *Request) Response {
	var args GateShowArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid gate show args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Resolve partial ID
	gateID, err := utils.ResolvePartialID(ctx, store, args.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to resolve gate ID: %v", err),
		}
	}

	gate, err := store.GetIssue(ctx, gateID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get gate: %v", err),
		}
	}
	if gate == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("gate %s not found", gateID),
		}
	}
	if gate.IssueType != "gate" {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("%s is not a gate (type: %s)", gateID, gate.IssueType),
		}
	}

	data, _ := json.Marshal(gate)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleGateClose(req *Request) Response {
	var args GateCloseArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid gate close args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Resolve partial ID
	gateID, err := utils.ResolvePartialID(ctx, store, args.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to resolve gate ID: %v", err),
		}
	}

	// Verify it's a gate
	gate, err := store.GetIssue(ctx, gateID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get gate: %v", err),
		}
	}
	if gate == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("gate %s not found", gateID),
		}
	}
	if gate.IssueType != "gate" {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("%s is not a gate (type: %s)", gateID, gate.IssueType),
		}
	}

	reason := args.Reason
	if reason == "" {
		reason = "Gate closed"
	}

	oldStatus := string(gate.Status)

	if err := store.CloseIssue(ctx, gateID, reason, s.reqActor(req), ""); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to close gate: %v", err),
		}
	}

	// Emit rich status change event
	s.emitRichMutation(MutationEvent{
		Type:      MutationStatus,
		IssueID:   gateID,
		OldStatus: oldStatus,
		NewStatus: "closed",
	})

	closedGate, _ := store.GetIssue(ctx, gateID)
	data, _ := json.Marshal(closedGate)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleGateWait(req *Request) Response {
	var args GateWaitArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid gate wait args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Resolve partial ID
	gateID, err := utils.ResolvePartialID(ctx, store, args.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to resolve gate ID: %v", err),
		}
	}

	// Get existing gate
	gate, err := store.GetIssue(ctx, gateID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get gate: %v", err),
		}
	}
	if gate == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("gate %s not found", gateID),
		}
	}
	if gate.IssueType != "gate" {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("%s is not a gate (type: %s)", gateID, gate.IssueType),
		}
	}
	if gate.Status == types.StatusClosed {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("gate %s is already closed", gateID),
		}
	}

	// Add new waiters (avoiding duplicates)
	waiterSet := make(map[string]bool)
	for _, w := range gate.Waiters {
		waiterSet[w] = true
	}
	newWaiters := []string{}
	for _, addr := range args.Waiters {
		if !waiterSet[addr] {
			newWaiters = append(newWaiters, addr)
			waiterSet[addr] = true
		}
	}

	addedCount := len(newWaiters)

	if addedCount > 0 {
		// Update waiters using SQLite directly
		sqliteStore, ok := store.(*sqlite.SQLiteStorage)
		if !ok {
			return Response{
				Success: false,
				Error:   "gate wait requires SQLite storage",
			}
		}

		allWaiters := append(gate.Waiters, newWaiters...)
		waitersJSON, _ := json.Marshal(allWaiters)

		// Use raw SQL to update the waiters field
		_, err = sqliteStore.UnderlyingDB().ExecContext(ctx, `UPDATE issues SET waiters = ?, updated_at = ? WHERE id = ?`,
			string(waitersJSON), time.Now(), gateID)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to add waiters: %v", err),
			}
		}

		// Emit mutation event
		s.emitMutation(MutationUpdate, gateID, gate.Title, gate.Assignee)
	}

	data, _ := json.Marshal(GateWaitResult{AddedCount: addedCount})
	return Response{
		Success: true,
		Data:    data,
	}
}

// Decision point handlers

func (s *Server) handleDecisionCreate(req *Request) Response {
	var args DecisionCreateArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid decision create args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	var issue *types.Issue

	// If IssueID is provided, validate it exists; otherwise create a gate issue
	if args.IssueID != "" {
		var err error
		issue, err = store.GetIssue(ctx, args.IssueID)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to get issue: %v", err),
			}
		}
		if issue == nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("issue %s not found", args.IssueID),
			}
		}
	} else {
		// Create a new gate issue for the decision (gt-w3u2o9)
		gateIssue := &types.Issue{
			Title:       fmt.Sprintf("[DECISION] %s", args.Prompt),
			Description: fmt.Sprintf("Decision ID: pending\nQuestion: %s", args.Prompt),
			Status:      "open",
			Priority:    2,
			IssueType:   "gate",
			AwaitType:   "decision",
			CreatedBy:   actor,
			Labels:      []string{"gt:decision", "decision:pending"},
		}
		if err := store.CreateIssue(ctx, gateIssue, actor); err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to create gate issue: %v", err),
			}
		}
		issue = gateIssue
		args.IssueID = gateIssue.ID
	}

	// Convert options to JSON
	optionsJSON, err := json.Marshal(args.Options)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to marshal options: %v", err),
		}
	}

	// Set defaults
	maxIterations := args.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 3
	}

	dp := &types.DecisionPoint{
		IssueID:       args.IssueID,
		Prompt:        args.Prompt,
		Options:       string(optionsJSON),
		DefaultOption: args.DefaultOption,
		MaxIterations: maxIterations,
		Iteration:     1,
		RequestedBy:   args.RequestedBy,
		CreatedAt:     time.Now(),
	}

	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to create decision point: %v", err),
		}
	}

	// Return the decision with its associated issue
	resp := DecisionResponse{
		Decision: dp,
		Issue:    issue,
	}

	data, _ := json.Marshal(resp)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleDecisionGet(req *Request) Response {
	var args DecisionGetArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid decision get args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	dp, err := store.GetDecisionPoint(ctx, args.IssueID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get decision point: %v", err),
		}
	}
	if dp == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("no decision point for issue %s", args.IssueID),
		}
	}

	// Get associated issue
	issue, _ := store.GetIssue(ctx, args.IssueID)

	resp := DecisionResponse{
		Decision: dp,
		Issue:    issue,
	}

	data, _ := json.Marshal(resp)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleDecisionResolve(req *Request) Response {
	var args DecisionResolveArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid decision resolve args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Get existing decision point
	dp, err := store.GetDecisionPoint(ctx, args.IssueID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get decision point: %v", err),
		}
	}
	if dp == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("no decision point for issue %s", args.IssueID),
		}
	}

	// Update the decision point
	now := time.Now()
	dp.SelectedOption = args.SelectedOption
	dp.ResponseText = args.ResponseText
	dp.RespondedBy = args.RespondedBy
	dp.RespondedAt = &now
	dp.Guidance = args.Guidance

	if err := store.UpdateDecisionPoint(ctx, dp); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to update decision point: %v", err),
		}
	}

	// Get associated issue
	issue, _ := store.GetIssue(ctx, args.IssueID)

	resp := DecisionResponse{
		Decision: dp,
		Issue:    issue,
	}

	data, _ := json.Marshal(resp)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleDecisionList(req *Request) Response {
	var args DecisionListArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid decision list args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Get pending decisions (storage method already filters to pending)
	decisions, err := store.ListPendingDecisions(ctx)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to list decisions: %v", err),
		}
	}

	// Build response with associated issues
	var respDecisions []*DecisionResponse
	for _, dp := range decisions {
		issue, _ := store.GetIssue(ctx, dp.IssueID)
		respDecisions = append(respDecisions, &DecisionResponse{
			Decision: dp,
			Issue:    issue,
		})
	}

	resp := DecisionListResponse{
		Decisions: respDecisions,
		Count:     len(respDecisions),
	}

	data, _ := json.Marshal(resp)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleDecisionRemind(req *Request) Response {
	var args DecisionRemindArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid decision remind args: %v", err),
		}
	}

	if args.IssueID == "" {
		return Response{
			Success: false,
			Error:   "issue_id is required",
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Verify issue is a decision gate
	issue, err := store.GetIssue(ctx, args.IssueID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get issue: %v", err),
		}
	}
	if issue == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("issue %s not found", args.IssueID),
		}
	}
	if issue.IssueType != "gate" || issue.AwaitType != "decision" {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("%s is not a decision point", args.IssueID),
		}
	}

	// Get decision point
	dp, err := store.GetDecisionPoint(ctx, args.IssueID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get decision point: %v", err),
		}
	}
	if dp == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("no decision point data for %s", args.IssueID),
		}
	}

	// Check if already responded
	if dp.RespondedAt != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("decision %s already responded", args.IssueID),
		}
	}

	// Check reminder limit
	maxReminders := 3
	if dp.ReminderCount >= maxReminders && !args.Force {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("decision %s has reached max reminders (%d/%d)", args.IssueID, dp.ReminderCount, maxReminders),
		}
	}

	// Increment reminder count
	dp.ReminderCount++
	if err := store.UpdateDecisionPoint(ctx, dp); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to update decision point: %v", err),
		}
	}

	s.emitMutation(MutationUpdate, args.IssueID, issue.Title, issue.Assignee)

	result := DecisionRemindResult{
		IssueID:       args.IssueID,
		ReminderCount: dp.ReminderCount,
		MaxReminders:  maxReminders,
		Prompt:        dp.Prompt,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleDecisionCancel(req *Request) Response {
	var args DecisionCancelArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid decision cancel args: %v", err),
		}
	}

	if args.IssueID == "" {
		return Response{
			Success: false,
			Error:   "issue_id is required",
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Verify issue is a decision gate
	issue, err := store.GetIssue(ctx, args.IssueID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get issue: %v", err),
		}
	}
	if issue == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("issue %s not found", args.IssueID),
		}
	}
	if issue.IssueType != "gate" || issue.AwaitType != "decision" {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("%s is not a decision point", args.IssueID),
		}
	}

	// Get decision point
	dp, err := store.GetDecisionPoint(ctx, args.IssueID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get decision point: %v", err),
		}
	}
	if dp == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("no decision point data for %s", args.IssueID),
		}
	}

	// Check if already responded
	if dp.RespondedAt != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("decision %s already responded", args.IssueID),
		}
	}

	// Mark as canceled
	now := time.Now()
	dp.RespondedAt = &now
	dp.RespondedBy = args.CanceledBy
	dp.SelectedOption = "_canceled"
	if args.Reason != "" {
		dp.ResponseText = args.Reason
	}

	if err := store.UpdateDecisionPoint(ctx, dp); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to update decision point: %v", err),
		}
	}

	// Close the gate issue
	closeReason := "Decision canceled"
	if args.Reason != "" {
		closeReason = fmt.Sprintf("Decision canceled: %s", args.Reason)
	}
	actor := req.Actor
	if actor == "" {
		actor = "daemon"
	}
	if err := store.CloseIssue(ctx, args.IssueID, closeReason, actor, ""); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to close gate: %v", err),
		}
	}

	s.emitMutation(MutationUpdate, args.IssueID, issue.Title, issue.Assignee)

	result := DecisionCancelResult{
		IssueID:    args.IssueID,
		CanceledAt: now.Format(time.RFC3339),
		Reason:     args.Reason,
		CanceledBy: args.CanceledBy,
		Prompt:     dp.Prompt,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

// handleCreateWithDeps handles the create_with_deps operation for atomic issue creation.
// This creates multiple issues with their labels and dependencies in a single transaction.
func (s *Server) handleCreateWithDeps(req *Request) Response {
	var args CreateWithDepsArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid create_with_deps args: %v", err),
		}
	}

	if len(args.Issues) == 0 {
		return Response{
			Success: false,
			Error:   "no issues to create",
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	// idMapping maps old IDs (from args) to new IDs (generated by storage)
	idMapping := make(map[string]string)

	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// First pass: create all issues and build ID mapping
		for i, issueArg := range args.Issues {
			// Save the reference ID for mapping before creating issue
			// The passed ID is used as a reference for dependency mapping,
			// NOT as the actual issue ID (to avoid duplicate key errors)
			refID := issueArg.ID
			if refID == "" {
				refID = fmt.Sprintf("%d", i)
			}

			issue := &types.Issue{
				ID:                 "", // Let storage generate unique ID (gt-yklx63 fix)
				Title:              issueArg.Title,
				Description:        issueArg.Description,
				IssueType:          types.IssueType(issueArg.IssueType),
				Priority:           issueArg.Priority,
				Design:             issueArg.Design,
				AcceptanceCriteria: issueArg.AcceptanceCriteria,
				Assignee:           issueArg.Assignee,
				EstimatedMinutes:   issueArg.EstimatedMinutes,
				Ephemeral:          issueArg.Ephemeral,
				IDPrefix:           issueArg.IDPrefix,
				Status:             types.StatusOpen,
			}

			// Create the issue (storage generates unique ID)
			if err := tx.CreateIssue(ctx, issue, actor); err != nil {
				return fmt.Errorf("failed to create issue %d (%s): %w", i, issueArg.Title, err)
			}

			// Map reference ID to new generated ID
			idMapping[refID] = issue.ID

			// Add labels for this issue
			for _, label := range issueArg.Labels {
				if err := tx.AddLabel(ctx, issue.ID, label, actor); err != nil {
					return fmt.Errorf("failed to add label %s to issue %s: %w", label, issue.ID, err)
				}
			}
		}

		// Second pass: create all dependencies using the ID mapping
		for _, depArg := range args.Dependencies {
			// Resolve FromID - might be an old ID that needs mapping
			fromID := depArg.FromID
			if mappedID, ok := idMapping[fromID]; ok {
				fromID = mappedID
			}

			// Resolve ToID - might be an old ID that needs mapping, or an existing issue ID
			toID := depArg.ToID
			if mappedID, ok := idMapping[toID]; ok {
				toID = mappedID
			}

			dep := &types.Dependency{
				IssueID:     fromID,
				DependsOnID: toID,
				Type:        types.DependencyType(depArg.DepType),
			}

			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("failed to add dependency %s -> %s (%s): %w",
					fromID, toID, depArg.DepType, err)
			}
		}

		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("transaction failed: %v", err),
		}
	}

	// Emit mutation events for all created issues
	for _, newID := range idMapping {
		s.emitMutation(MutationCreate, newID, "", "")
	}

	result := CreateWithDepsResult{
		IDMapping: idMapping,
		Created:   len(args.Issues),
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

// handleCreateMolecule creates multiple issues and their dependencies atomically.
// This fixes FK violations that occur when creating issues and dependencies
// in separate operations, as dependencies may reference issues that don't exist yet.
// (bd-jjbl)
func (s *Server) handleCreateMolecule(req *Request) Response {
	var args CreateMoleculeArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid create_molecule args: %v", err),
		}
	}

	// Validate we have at least one issue
	if len(args.Issues) == 0 {
		return Response{
			Success: false,
			Error:   "at least one issue is required",
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}
	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	// Build ID mapping: template ID → new ID
	idMapping := make(map[string]string)

	// Use transaction for atomicity - this ensures all issues and dependencies
	// are created together, avoiding FK violations when dependencies reference
	// issues that haven't been created yet
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// First pass: create all issues with new IDs
		for _, spec := range args.Issues {
			// Build the issue from CreateArgs
			createArgs := spec.CreateArgs

			// Apply molecule-level defaults
			if args.Prefix != "" && createArgs.IDPrefix == "" {
				createArgs.IDPrefix = args.Prefix
			}
			if args.Ephemeral {
				createArgs.Ephemeral = true
			}

			// Parse optional fields
			var design, acceptance, notes, assignee, externalRef *string
			if createArgs.Design != "" {
				design = &createArgs.Design
			}
			if createArgs.AcceptanceCriteria != "" {
				acceptance = &createArgs.AcceptanceCriteria
			}
			if createArgs.Notes != "" {
				notes = &createArgs.Notes
			}
			if createArgs.Assignee != "" {
				assignee = &createArgs.Assignee
			}
			if createArgs.ExternalRef != "" {
				externalRef = &createArgs.ExternalRef
			}

			// Parse DueAt if provided
			var dueAt *time.Time
			if createArgs.DueAt != "" {
				if t, err := time.ParseInLocation("2006-01-02", createArgs.DueAt, time.Local); err == nil {
					dueAt = &t
				} else if t, err := time.Parse(time.RFC3339, createArgs.DueAt); err == nil {
					dueAt = &t
				} else {
					return fmt.Errorf("invalid due_at format %q for template %s", createArgs.DueAt, spec.TemplateID)
				}
			}

			// Parse DeferUntil if provided
			var deferUntil *time.Time
			if createArgs.DeferUntil != "" {
				if t, err := time.ParseInLocation("2006-01-02", createArgs.DeferUntil, time.Local); err == nil {
					deferUntil = &t
				} else if t, err := time.Parse(time.RFC3339, createArgs.DeferUntil); err == nil {
					deferUntil = &t
				} else {
					return fmt.Errorf("invalid defer_until format %q for template %s", createArgs.DeferUntil, spec.TemplateID)
				}
			}

			issue := &types.Issue{
				ID:                 createArgs.ID, // May be empty - storage will generate
				Title:              createArgs.Title,
				Description:        createArgs.Description,
				IssueType:          types.IssueType(createArgs.IssueType),
				Priority:           createArgs.Priority,
				Design:             strValue(design),
				AcceptanceCriteria: strValue(acceptance),
				Notes:              strValue(notes),
				Assignee:           strValue(assignee),
				ExternalRef:        externalRef,
				EstimatedMinutes:   createArgs.EstimatedMinutes,
				Status:             types.StatusOpen,
				Sender:             createArgs.Sender,
				Ephemeral:          createArgs.Ephemeral,
				Pinned:             createArgs.Pinned,
				AutoClose:          createArgs.AutoClose,
				IDPrefix:           createArgs.IDPrefix,
				CreatedBy:          createArgs.CreatedBy,
				Owner:              createArgs.Owner,
				MolType:            types.MolType(createArgs.MolType),
				RoleType:           createArgs.RoleType,
				Rig:                createArgs.Rig,
				EventKind:          createArgs.EventCategory,
				Actor:              createArgs.EventActor,
				Target:             createArgs.EventTarget,
				Payload:            createArgs.EventPayload,
				DueAt:              dueAt,
				DeferUntil:         deferUntil,
				AwaitType:          createArgs.AwaitType,
				AwaitID:            createArgs.AwaitID,
				Timeout:            createArgs.Timeout,
				Waiters:            createArgs.Waiters,
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
			}

			if err := tx.CreateIssue(ctx, issue, actor); err != nil {
				return fmt.Errorf("failed to create issue for template %s: %w", spec.TemplateID, err)
			}

			// Store the mapping from template ID to the actual generated ID
			idMapping[spec.TemplateID] = issue.ID
		}

		// Second pass: create all dependencies using mapped IDs
		for _, dep := range args.Dependencies {
			fromID, ok1 := idMapping[dep.FromTemplateID]
			toID, ok2 := idMapping[dep.ToTemplateID]
			if !ok1 {
				return fmt.Errorf("unknown from_template_id: %s", dep.FromTemplateID)
			}
			if !ok2 {
				return fmt.Errorf("unknown to_template_id: %s", dep.ToTemplateID)
			}

			newDep := &types.Dependency{
				IssueID:     fromID,
				DependsOnID: toID,
				Type:        types.DependencyType(dep.DepType),
			}
			if err := tx.AddDependency(ctx, newDep, actor); err != nil {
				return fmt.Errorf("failed to create dependency %s → %s: %w", fromID, toID, err)
			}
		}

		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to create molecule: %v", err),
		}
	}

	// Determine root ID if RootTemplate was specified
	rootID := ""
	if args.RootTemplate != "" {
		rootID = idMapping[args.RootTemplate]
	}

	result := CreateMoleculeResult{
		IDMapping: idMapping,
		RootID:    rootID,
		Created:   len(args.Issues),
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

// handleCreateConvoyWithTracking creates a convoy issue and tracking dependencies atomically.
// This ensures the convoy and all tracking relations are created in a single transaction,
// preventing partial state where the convoy exists but some tracking deps failed.
func (s *Server) handleCreateConvoyWithTracking(req *Request) Response {
	var args CreateConvoyWithTrackingArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid create_convoy_with_tracking args: %v", err),
		}
	}

	// Validate required fields
	if args.Name == "" {
		return Response{
			Success: false,
			Error:   "convoy name is required",
		}
	}

	if len(args.TrackedIssues) == 0 {
		return Response{
			Success: false,
			Error:   "at least one tracked issue is required",
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	var convoyID string
	trackedIDs := make([]string, 0, len(args.TrackedIssues))

	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Create the convoy issue
		convoy := &types.Issue{
			ID:        args.ConvoyID, // Will be auto-generated if empty
			Title:     args.Name,
			IssueType: types.IssueType("convoy"),
			Status:    types.StatusOpen,
			Owner:     args.Owner,
		}

		// Add notify address to description if provided
		if args.NotifyAddress != "" {
			convoy.Description = fmt.Sprintf("Notify: %s", args.NotifyAddress)
		}

		if err := tx.CreateIssue(ctx, convoy, actor); err != nil {
			return fmt.Errorf("failed to create convoy issue: %w", err)
		}
		convoyID = convoy.ID

		// Add tracking dependencies for each tracked issue
		for _, trackedID := range args.TrackedIssues {
			dep := &types.Dependency{
				IssueID:     convoyID,
				DependsOnID: trackedID,
				Type:        types.DepTracks,
			}

			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("failed to add tracking dependency for %s: %w", trackedID, err)
			}
			trackedIDs = append(trackedIDs, trackedID)
		}

		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("transaction failed: %v", err),
		}
	}

	// Emit mutation event for convoy creation
	s.emitMutation(MutationCreate, convoyID, "", "")

	result := CreateConvoyWithTrackingResult{
		ConvoyID:     convoyID,
		TrackedCount: len(trackedIDs),
		TrackedIDs:   trackedIDs,
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

// handleAtomicClosureChain closes multiple related issues and updates an agent atomically.
// This is used for MR completion where we need to:
// 1. Close the MR bead with a reason (e.g., "merged")
// 2. Close the source issue with a reason
// 3. Optionally update the agent bead (e.g., clear hook_bead)
// All operations happen in a single transaction for consistency.
func (s *Server) handleAtomicClosureChain(req *Request) Response {
	var args AtomicClosureChainArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid atomic_closure_chain args: %v", err),
		}
	}

	// Validate required fields
	if args.MRID == "" {
		return Response{
			Success: false,
			Error:   "mr_id is required",
		}
	}
	if args.SourceIssueID == "" {
		return Response{
			Success: false,
			Error:   "source_issue_id is required",
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	// Track results
	result := AtomicClosureChainResult{}

	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Step 1: Close the MR bead
		if err := tx.CloseIssue(ctx, args.MRID, args.MRCloseReason, actor, ""); err != nil {
			return fmt.Errorf("failed to close MR %s: %w", args.MRID, err)
		}
		result.MRClosed = true
		result.MRCloseTime = time.Now().Format(time.RFC3339)

		// Step 2: Close the source issue
		if err := tx.CloseIssue(ctx, args.SourceIssueID, args.SourceCloseReason, actor, ""); err != nil {
			return fmt.Errorf("failed to close source issue %s: %w", args.SourceIssueID, err)
		}
		result.SourceIssueClosed = true
		result.SourceCloseTime = time.Now().Format(time.RFC3339)

		// Step 3: Update agent bead if specified
		if args.AgentBeadID != "" && len(args.AgentUpdates) > 0 {
			if err := tx.UpdateIssue(ctx, args.AgentBeadID, args.AgentUpdates, actor); err != nil {
				return fmt.Errorf("failed to update agent %s: %w", args.AgentBeadID, err)
			}
			result.AgentUpdated = true
		}

		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("transaction failed: %v", err),
		}
	}

	// Emit mutation events for the closures
	s.emitMutation(MutationStatus, args.MRID, "", "")
	s.emitMutation(MutationStatus, args.SourceIssueID, "", "")
	if result.AgentUpdated {
		s.emitMutation(MutationUpdate, args.AgentBeadID, "", "")
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}
