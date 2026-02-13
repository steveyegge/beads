package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/compact"
	"github.com/steveyegge/beads/internal/types"
)

// compactionCandidate holds information about a compaction candidate.
type compactionCandidate struct {
	IssueID string
}

// compactableStore is an optional interface that storage backends can implement
// to support compaction operations. Backends that don't implement this will
// get a "compaction not supported" error.
type compactableStore interface {
	CheckEligibility(ctx context.Context, issueID string, tier int) (bool, string, error)
	GetIssue(ctx context.Context, issueID string) (*types.Issue, error)
	UpdateIssue(ctx context.Context, issueID string, updates map[string]interface{}, actor string) error
	ApplyCompaction(ctx context.Context, issueID string, tier int, originalSize int, compactedSize int, commitHash string) error
	AddComment(ctx context.Context, issueID, actor, comment string) error
	MarkIssueDirty(ctx context.Context, issueID string) error
	GetTier1Candidates(ctx context.Context) ([]*compactionCandidate, error)
	GetTier2Candidates(ctx context.Context) ([]*compactionCandidate, error)
}

func (s *Server) handleCompact(req *Request) Response {
	var args CompactArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid compact args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	cs, ok := store.(compactableStore)
	if !ok {
		return Response{
			Success: false,
			Error:   "compact not supported by the current storage backend",
		}
	}

	config := &compact.Config{
		APIKey:      args.APIKey,
		Concurrency: args.Workers,
		DryRun:      args.DryRun,
	}
	if config.Concurrency <= 0 {
		config.Concurrency = 5
	}

	compactor, err := compact.New(cs, args.APIKey, config)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to create compactor: %v", err),
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	startTime := time.Now()

	if args.IssueID != "" {
		if !args.Force {
			eligible, reason, err := cs.CheckEligibility(ctx, args.IssueID, args.Tier)
			if err != nil {
				return Response{
					Success: false,
					Error:   fmt.Sprintf("failed to check eligibility: %v", err),
				}
			}
			if !eligible {
				return Response{
					Success: false,
					Error:   fmt.Sprintf("%s is not eligible for Tier %d compaction: %s", args.IssueID, args.Tier, reason),
				}
			}
		}

		issue, err := cs.GetIssue(ctx, args.IssueID)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to get issue: %v", err),
			}
		}

		originalSize := len(issue.Description) + len(issue.Design) + len(issue.Notes) + len(issue.AcceptanceCriteria)

		if args.DryRun {
			result := CompactResponse{
				Success:      true,
				IssueID:      args.IssueID,
				OriginalSize: originalSize,
				Reduction:    "70-80%",
				DryRun:       true,
			}
			data, _ := json.Marshal(result)
			return Response{
				Success: true,
				Data:    data,
			}
		}

		if args.Tier == 1 {
			err = compactor.CompactTier1(ctx, args.IssueID)
		} else {
			return Response{
				Success: false,
				Error:   "Tier 2 compaction not yet implemented",
			}
		}

		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("compaction failed: %v", err),
			}
		}

		issueAfter, _ := cs.GetIssue(ctx, args.IssueID)
		compactedSize := 0
		if issueAfter != nil {
			compactedSize = len(issueAfter.Description)
		}

		duration := time.Since(startTime)
		result := CompactResponse{
			Success:       true,
			IssueID:       args.IssueID,
			OriginalSize:  originalSize,
			CompactedSize: compactedSize,
			Reduction:     fmt.Sprintf("%.1f%%", float64(originalSize-compactedSize)/float64(originalSize)*100),
			Duration:      duration.String(),
		}
		data, _ := json.Marshal(result)
		return Response{
			Success: true,
			Data:    data,
		}
	}

	if args.All {
		var candidates []*compactionCandidate

		switch args.Tier {
		case 1:
			tier1, err := cs.GetTier1Candidates(ctx)
			if err != nil {
				return Response{
					Success: false,
					Error:   fmt.Sprintf("failed to get Tier 1 candidates: %v", err),
				}
			}
			candidates = tier1
		case 2:
			tier2, err := cs.GetTier2Candidates(ctx)
			if err != nil {
				return Response{
					Success: false,
					Error:   fmt.Sprintf("failed to get Tier 2 candidates: %v", err),
				}
			}
			candidates = tier2
		default:
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid tier: %d (must be 1 or 2)", args.Tier),
			}
		}

		if len(candidates) == 0 {
			result := CompactResponse{
				Success: true,
				Results: []CompactResult{},
			}
			data, _ := json.Marshal(result)
			return Response{
				Success: true,
				Data:    data,
			}
		}

		issueIDs := make([]string, len(candidates))
		for i, c := range candidates {
			issueIDs[i] = c.IssueID
		}

		batchResults, err := compactor.CompactTier1Batch(ctx, issueIDs)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("batch compaction failed: %v", err),
			}
		}

		results := make([]CompactResult, 0, len(batchResults))
		for _, r := range batchResults {
			result := CompactResult{
				IssueID:       r.IssueID,
				Success:       r.Err == nil,
				OriginalSize:  r.OriginalSize,
				CompactedSize: r.CompactedSize,
			}
			if r.Err != nil {
				result.Error = r.Err.Error()
			} else if r.OriginalSize > 0 && r.CompactedSize > 0 {
				result.Reduction = fmt.Sprintf("%.1f%%", float64(r.OriginalSize-r.CompactedSize)/float64(r.OriginalSize)*100)
			}
			results = append(results, result)
		}

		duration := time.Since(startTime)
		response := CompactResponse{
			Success:  true,
			Results:  results,
			Duration: duration.String(),
			DryRun:   args.DryRun,
		}
		data, _ := json.Marshal(response)
		return Response{
			Success: true,
			Data:    data,
		}
	}

	return Response{
		Success: false,
		Error:   "must specify --all or --id",
	}
}

func (s *Server) handleCompactStats(req *Request) Response {
	var args CompactStatsArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid compact stats args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	cs, ok := store.(compactableStore)
	if !ok {
		return Response{
			Success: false,
			Error:   "compact stats not supported by the current storage backend",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	tier1, err := cs.GetTier1Candidates(ctx)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get Tier 1 candidates: %v", err),
		}
	}

	tier2, err := cs.GetTier2Candidates(ctx)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get Tier 2 candidates: %v", err),
		}
	}

	stats := CompactStatsData{
		Tier1Candidates: len(tier1),
		Tier2Candidates: len(tier2),
		Tier1MinAge:     "30 days",
		Tier2MinAge:     "90 days",
		TotalClosed:     0, // Could query for this but not critical
	}

	result := CompactResponse{
		Success: true,
		Stats:   &stats,
	}
	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}
