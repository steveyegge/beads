package rpc

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/specarchive"
	"github.com/steveyegge/beads/internal/types"
)

func (s *Server) specStore() (spec.SpecRegistryStore, error) {
	store, ok := s.storage.(spec.SpecRegistryStore)
	if !ok {
		return nil, fmt.Errorf("storage backend does not support spec registry")
	}
	return store, nil
}

func (s *Server) handleSpecScan(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecScanArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid spec scan args: %v", err)}
		}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	root := s.workspacePath
	if root == "" {
		root = req.Cwd
	}
	if root == "" {
		root = "."
	}

	scanPath := args.Path
	if scanPath == "" {
		scanPath = "specs"
	}

	existingEntries, err := store.ListSpecRegistry(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list spec registry: %v", err)}
	}
	existingByID := make(map[string]spec.SpecRegistryEntry, len(existingEntries))
	for _, entry := range existingEntries {
		existingByID[entry.SpecID] = entry
	}

	scanned, err := spec.ScanWithOptions(root, scanPath, &spec.ScanOptions{
		ExistingByID: existingByID,
	})
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("scan specs: %v", err)}
	}

	now := time.Now().UTC().Truncate(time.Second)
	result, err := spec.UpdateRegistry(ctx, store, scanned, now)
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecList(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecListArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid spec list args: %v", err)}
		}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	entries, err := store.ListSpecRegistryWithCounts(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list spec registry: %v", err)}
	}

	filtered := make([]spec.SpecRegistryCount, 0, len(entries))
	for _, entry := range entries {
		if !args.IncludeMissing && entry.Spec.MissingAt != nil {
			continue
		}
		if args.Prefix != "" && !strings.HasPrefix(entry.Spec.SpecID, args.Prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}

	data, _ := json.Marshal(filtered)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecShow(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecShowArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid spec show args: %v", err)}
	}
	if strings.TrimSpace(args.SpecID) == "" {
		return Response{Success: false, Error: "spec_id is required"}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	entry, err := store.GetSpecRegistry(ctx, args.SpecID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("get spec: %v", err)}
	}
	if entry == nil {
		return Response{Success: false, Error: fmt.Sprintf("spec not found: %s", args.SpecID)}
	}

	filter := types.IssueFilter{SpecID: &args.SpecID}
	beads, err := s.storage.SearchIssues(ctx, "", filter)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list beads for spec: %v", err)}
	}

	resp := SpecShowResult{
		Spec:  entry,
		Beads: beads,
	}
	data, _ := json.Marshal(resp)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecCoverage(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecCoverageArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid spec coverage args: %v", err)}
		}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	entries, err := store.ListSpecRegistryWithCounts(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list spec registry: %v", err)}
	}

	result := SpecCoverageResult{}
	for _, entry := range entries {
		if !args.IncludeMissing && entry.Spec.MissingAt != nil {
			continue
		}
		if args.Prefix != "" && !strings.HasPrefix(entry.Spec.SpecID, args.Prefix) {
			continue
		}

		result.Total++
		if entry.Spec.MissingAt != nil {
			result.Missing++
		}
		if entry.BeadCount > 0 {
			result.WithBeads++
		} else {
			result.WithoutBeads++
		}
		if entry.ChangedBeadCount > 0 {
			result.WithChangedBeads++
		}
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecCompact(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecCompactArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid spec compact args: %v", err)}
	}
	if strings.TrimSpace(args.SpecID) == "" {
		return Response{Success: false, Error: "spec_id is required"}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	update := spec.SpecRegistryUpdate{}
	if args.Lifecycle != "" {
		update.Lifecycle = &args.Lifecycle
	}
	if args.CompletedAt != nil {
		update.CompletedAt = args.CompletedAt
	}
	if args.Summary != "" {
		update.Summary = &args.Summary
		update.SummaryTokens = &args.SummaryTokens
	}
	if args.ArchivedAt != nil {
		update.ArchivedAt = args.ArchivedAt
	}

	specID := args.SpecID
	if args.MoveToArchive {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			return Response{Success: false, Error: "no .beads directory found"}
		}
		repoRoot := filepath.Dir(beadsDir)
		newSpecID, moved, err := specarchive.MoveSpecFile(repoRoot, specID)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("move spec file: %v", err)}
		}
		if moved {
			if err := specarchive.MoveSpecReferences(ctx, s.storage, store, specID, newSpecID); err != nil {
				return Response{Success: false, Error: fmt.Sprintf("move spec references: %v", err)}
			}
		}
		specID = newSpecID
	}

	if err := store.UpdateSpecRegistry(ctx, specID, update); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("update spec registry: %v", err)}
	}

	entry, err := store.GetSpecRegistry(ctx, specID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("get spec: %v", err)}
	}
	data, _ := json.Marshal(entry)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecRisk(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecRiskArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid spec volatility args: %v", err)}
		}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	var since time.Time
	if strings.TrimSpace(args.Since) != "" {
		parsed, err := time.Parse(time.RFC3339, args.Since)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid since timestamp: %v", err)}
		}
		since = parsed
	}

	minChanges := args.MinChanges
	if minChanges <= 0 {
		minChanges = 1
	}

	entries, err := store.ListSpecRegistry(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list spec registry: %v", err)}
	}

	openIssues := make(map[string]int)
	openFilter := types.IssueFilter{
		ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
	}
	issues, err := s.storage.SearchIssues(ctx, "", openFilter)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list open issues: %v", err)}
	}
	for _, issue := range issues {
		if issue.SpecID == "" {
			continue
		}
		openIssues[issue.SpecID]++
	}

	halfLife := config.GetString("volatility.decay.half_life")
	halfLifeDuration := time.Duration(0)
	if strings.TrimSpace(halfLife) != "" {
		if parsed, err := parseDurationString(halfLife); err == nil {
			halfLifeDuration = parsed
		}
	}
	now := time.Now().UTC()

	results := make([]spec.SpecRiskEntry, 0)
	for _, entry := range entries {
		if entry.MissingAt != nil {
			continue
		}
		events, err := store.ListSpecScanEvents(ctx, entry.SpecID, since)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("list spec scan events: %v", err)}
		}
		rawChangeCount, lastChangedAt := spec.SummarizeScanEvents(events, time.Time{})
		weighted := 0.0
		if halfLifeDuration > 0 {
			weighted, lastChangedAt = spec.SummarizeScanEventsWeighted(events, time.Time{}, now, halfLifeDuration)
		}
		effectiveChanges := rawChangeCount
		if weighted > 0 {
			effectiveChanges = int(weighted + 0.5)
		}
		if effectiveChanges < minChanges {
			continue
		}
		results = append(results, spec.SpecRiskEntry{
			SpecID:              entry.SpecID,
			Title:               entry.Title,
			ChangeCount:         rawChangeCount,
			WeightedChangeCount: weighted,
			LastChangedAt:       lastChangedAt,
			OpenIssues:          openIssues[entry.SpecID],
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].ChangeCount != results[j].ChangeCount {
			return results[i].ChangeCount > results[j].ChangeCount
		}
		if results[i].OpenIssues != results[j].OpenIssues {
			return results[i].OpenIssues > results[j].OpenIssues
		}
		return results[i].SpecID < results[j].SpecID
	})

	if args.Limit > 0 && len(results) > args.Limit {
		results = results[:args.Limit]
	}

	data, _ := json.Marshal(results)
	return Response{Success: true, Data: data}
}

func parseDurationString(input string) (time.Duration, error) {
	if d, err := time.ParseDuration(input); err == nil {
		return d, nil
	}
	re := regexp.MustCompile(`^(\d+)([dhms])$`)
	matches := re.FindStringSubmatch(strings.ToLower(input))
	if len(matches) != 3 {
		return 0, fmt.Errorf("invalid duration format: %s", input)
	}
	value, _ := strconv.Atoi(matches[1])
	unit := matches[2]
	switch unit {
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	case "s":
		return time.Duration(value) * time.Second, nil
	default:
		return 0, fmt.Errorf("unknown duration unit: %s", unit)
	}
}

func (s *Server) handleSpecSuggest(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecSuggestArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid spec suggest args: %v", err)}
	}
	if strings.TrimSpace(args.IssueID) == "" {
		return Response{Success: false, Error: "issue_id is required"}
	}

	issue, err := s.storage.GetIssue(ctx, args.IssueID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("get issue: %v", err)}
	}
	if issue == nil {
		return Response{Success: false, Error: fmt.Sprintf("issue not found: %s", args.IssueID)}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	entries, err := store.ListSpecRegistry(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list spec registry: %v", err)}
	}

	specs := make([]spec.SpecRegistryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.MissingAt != nil {
			continue
		}
		specs = append(specs, entry)
	}

	limit := args.Limit
	if limit == 0 {
		limit = 3
	}
	threshold := args.Threshold
	if threshold == 0 {
		threshold = 40
	}
	minScore := float64(threshold) / 100.0

	result := SpecSuggestResult{
		IssueID:     issue.ID,
		IssueTitle:  issue.Title,
		CurrentSpec: issue.SpecID,
	}
	if issue.SpecID == "" {
		result.Suggestions = spec.SuggestSpecs(issue.Title, specs, limit, minScore)
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecLinkAuto(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecLinkAutoArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid spec link auto args: %v", err)}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	entries, err := store.ListSpecRegistry(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list spec registry: %v", err)}
	}

	specs := make([]spec.SpecRegistryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.MissingAt != nil {
			continue
		}
		specs = append(specs, entry)
	}

	filter := types.IssueFilter{NoSpec: true}
	if !args.IncludeClosed {
		filter.ExcludeStatus = []types.Status{types.StatusClosed}
	}
	if args.MaxIssues > 0 {
		filter.Limit = args.MaxIssues
	}

	issues, err := s.storage.SearchIssues(ctx, "", filter)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list issues: %v", err)}
	}

	threshold := args.Threshold
	if threshold == 0 {
		threshold = 80
	}
	minScore := float64(threshold) / 100.0

	result := SpecLinkAutoResult{
		TotalIssues: len(issues),
	}
	actor := s.reqActor(req)

	for _, issue := range issues {
		if strings.TrimSpace(issue.Title) == "" {
			result.SkippedNoTitle++
			continue
		}
		match, ok := spec.BestSpecMatch(issue.Title, specs, minScore)
		if !ok {
			result.SkippedLowScore++
			continue
		}
		result.Matched++
		suggestion := SpecLinkAutoSuggestion{
			IssueID:    issue.ID,
			IssueTitle: issue.Title,
			SpecID:     match.SpecID,
			SpecTitle:  match.Title,
			Score:      match.Score,
		}
		if args.Confirm {
			updates := map[string]interface{}{
				"spec_id": match.SpecID,
			}
			if err := s.storage.UpdateIssue(ctx, issue.ID, updates, actor); err != nil {
				suggestion.Error = err.Error()
			} else {
				suggestion.Applied = true
				result.Applied++
			}
		}
		result.Suggestions = append(result.Suggestions, suggestion)
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecCandidates(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecCandidatesArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid spec candidates args: %v", err)}
		}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	entries, err := store.ListSpecRegistryWithCounts(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list spec registry: %v", err)}
	}

	now := time.Now()
	candidates := make([]SpecCandidateEntry, 0)

	for _, entry := range entries {
		// Skip missing, complete, or archived specs
		if entry.Spec.MissingAt != nil {
			continue
		}
		if entry.Spec.Lifecycle == "complete" || entry.Spec.Lifecycle == "archived" {
			continue
		}

		// Get issues linked to this spec
		specID := entry.Spec.SpecID
		filter := types.IssueFilter{SpecID: &specID}
		issues, err := s.storage.SearchIssues(ctx, "", filter)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("search issues for spec %s: %v", specID, err)}
		}

		// Count open vs closed
		openCount := 0
		closedCount := 0
		for _, issue := range issues {
			if issue.Status == types.StatusClosed || issue.Status == types.StatusTombstone {
				closedCount++
			} else {
				openCount++
			}
		}

		// Calculate score
		score := 0.0
		reasons := make([]string, 0)

		// +0.4 - All linked issues are closed
		if len(issues) > 0 && openCount == 0 {
			score += 0.4
			reasons = append(reasons, fmt.Sprintf("All %d issues closed", closedCount))
		}

		// +0.3 - Spec unchanged for 30+ days
		daysOld := 0
		if !entry.Spec.Mtime.IsZero() {
			daysOld = int(now.Sub(entry.Spec.Mtime).Hours() / 24)
			if daysOld >= 30 {
				score += 0.3
				reasons = append(reasons, fmt.Sprintf("%d days old", daysOld))
			}
		}

		// +0.2 - Has at least one linked issue
		if len(issues) > 0 {
			score += 0.2
		}

		// +0.1 - Title suggests completion
		titleLower := strings.ToLower(entry.Spec.Title)
		if strings.Contains(titleLower, "complete") ||
			strings.Contains(titleLower, "done") ||
			strings.Contains(titleLower, "finished") {
			score += 0.1
			reasons = append(reasons, "Title suggests completion")
		}

		// Only include if score >= 0.6
		if score >= 0.6 {
			action := "SUGGEST"
			if score >= 0.8 {
				action = "MARK"
			}

			reason := strings.Join(reasons, ", ")
			if reason == "" {
				reason = "No issues linked"
				if daysOld > 0 {
					reason = fmt.Sprintf("No issues linked, %d days old", daysOld)
				}
			}

			candidates = append(candidates, SpecCandidateEntry{
				SpecID:      entry.Spec.SpecID,
				Title:       entry.Spec.Title,
				Score:       score,
				Action:      action,
				Reason:      reason,
				OpenIssues:  openCount,
				ClosedCount: closedCount,
				DaysOld:     daysOld,
			})
		}
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	result := SpecCandidatesResult{
		Candidates: candidates,
	}

	// Auto-mark if requested
	if args.Auto {
		for i := range result.Candidates {
			c := &result.Candidates[i]
			if c.Score >= 0.8 {
				lifecycle := "complete"
				completedAt := time.Now().UTC().Truncate(time.Second)
				update := spec.SpecRegistryUpdate{
					Lifecycle:   &lifecycle,
					CompletedAt: &completedAt,
				}
				if err := store.UpdateSpecRegistry(ctx, c.SpecID, update); err != nil {
					c.Error = fmt.Sprintf("failed to mark: %v", err)
				} else {
					c.Marked = true
					result.Marked++
				}
			}
		}
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecAudit(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecAuditArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid spec audit args: %v", err)}
		}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	entries, err := store.ListSpecRegistry(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list spec registry: %v", err)}
	}

	// Get all issues to count open/closed per spec
	allIssues, err := s.storage.SearchIssues(ctx, "", types.IssueFilter{
		IncludeTombstones: false,
	})
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("list issues: %v", err)}
	}

	// Build issue counts per spec
	openCounts := make(map[string]int)
	closedCounts := make(map[string]int)
	for _, issue := range allIssues {
		if issue.SpecID == "" {
			continue
		}
		if issue.Status == types.StatusClosed {
			closedCounts[issue.SpecID]++
		} else {
			openCounts[issue.SpecID]++
		}
	}

	now := time.Now()
	staleDays := 30

	result := SpecAuditResult{}

	for _, entry := range entries {
		if !args.IncludeMissing && entry.MissingAt != nil {
			continue
		}
		if args.Prefix != "" && !strings.HasPrefix(entry.SpecID, args.Prefix) {
			continue
		}

		openCount := openCounts[entry.SpecID]
		closedCount := closedCounts[entry.SpecID]
		totalCount := openCount + closedCount

		// Calculate completion percentage
		var completion float64
		if totalCount > 0 {
			completion = float64(closedCount) / float64(totalCount) * 100
		}

		// Determine status
		var status string
		isStale := false

		// Check for stale: not modified in 30+ days
		lastMod := entry.Mtime
		if !entry.LastScannedAt.IsZero() && entry.LastScannedAt.After(lastMod) {
			lastMod = entry.LastScannedAt
		}
		if now.Sub(lastMod) > time.Duration(staleDays)*24*time.Hour {
			isStale = true
		}

		// Determine status based on lifecycle or issue counts
		if entry.Lifecycle == "complete" || entry.Lifecycle == "archived" {
			status = "complete"
		} else if isStale && totalCount > 0 && openCount > 0 {
			status = "stale"
		} else if totalCount == 0 {
			status = "pending"
		} else if openCount > 0 {
			status = "in-progress"
		} else {
			status = "complete"
		}

		auditEntry := SpecAuditEntry{
			SpecID:       entry.SpecID,
			Title:        entry.Title,
			Path:         entry.Path,
			Lifecycle:    entry.Lifecycle,
			OpenIssues:   openCount,
			ClosedIssues: closedCount,
			TotalIssues:  totalCount,
			Completion:   completion,
			Status:       status,
			Stale:        isStale,
		}
		if !lastMod.IsZero() {
			auditEntry.LastModified = lastMod.Format(time.RFC3339)
		}

		result.Entries = append(result.Entries, auditEntry)

		// Update summary
		result.Summary.TotalSpecs++
		switch status {
		case "pending":
			result.Summary.PendingSpecs++
		case "in-progress":
			result.Summary.InProgressSpecs++
		case "complete":
			result.Summary.CompleteSpecs++
		case "stale":
			result.Summary.StaleSpecs++
		}
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecMarkDone(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	var args SpecMarkDoneArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid spec mark-done args: %v", err)}
	}
	if strings.TrimSpace(args.SpecID) == "" {
		return Response{Success: false, Error: "spec_id is required"}
	}

	store, err := s.specStore()
	if err != nil {
		return Response{Success: false, Error: err.Error()}
	}

	// Verify spec exists
	entry, err := store.GetSpecRegistry(ctx, args.SpecID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("get spec: %v", err)}
	}
	if entry == nil {
		return Response{Success: false, Error: fmt.Sprintf("spec not found: %s", args.SpecID)}
	}

	now := time.Now().UTC().Truncate(time.Second)
	lifecycle := "complete"

	update := spec.SpecRegistryUpdate{
		Lifecycle:   &lifecycle,
		CompletedAt: &now,
	}

	if err := store.UpdateSpecRegistry(ctx, args.SpecID, update); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("update spec registry: %v", err)}
	}

	// Return updated entry
	entry, err = store.GetSpecRegistry(ctx, args.SpecID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("get spec: %v", err)}
	}

	data, _ := json.Marshal(entry)
	return Response{Success: true, Data: data}
}
