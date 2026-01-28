package rpc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/spec"
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
	ctx := s.reqCtx(req)

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

	scanned, err := spec.Scan(root, scanPath)
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
	ctx := s.reqCtx(req)

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
	ctx := s.reqCtx(req)

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
	ctx := s.reqCtx(req)

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
	ctx := s.reqCtx(req)

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

	if err := store.UpdateSpecRegistry(ctx, args.SpecID, update); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("update spec registry: %v", err)}
	}

	entry, err := store.GetSpecRegistry(ctx, args.SpecID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("get spec: %v", err)}
	}
	data, _ := json.Marshal(entry)
	return Response{Success: true, Data: data}
}

func (s *Server) handleSpecRisk(req *Request) Response {
	ctx := s.reqCtx(req)

	var args SpecRiskArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid spec risk args: %v", err)}
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

	results := make([]spec.SpecRiskEntry, 0)
	for _, entry := range entries {
		if entry.MissingAt != nil {
			continue
		}
		events, err := store.ListSpecScanEvents(ctx, entry.SpecID, since)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("list spec scan events: %v", err)}
		}
		changeCount, lastChangedAt := spec.SummarizeScanEvents(events, time.Time{})
		if changeCount < minChanges {
			continue
		}
		results = append(results, spec.SpecRiskEntry{
			SpecID:        entry.SpecID,
			Title:         entry.Title,
			ChangeCount:   changeCount,
			LastChangedAt: lastChangedAt,
			OpenIssues:    openIssues[entry.SpecID],
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

func (s *Server) handleSpecSuggest(req *Request) Response {
	ctx := s.reqCtx(req)

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
	ctx := s.reqCtx(req)

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
