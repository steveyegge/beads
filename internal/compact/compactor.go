package compact

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/steveyegge/beads/internal/storage/sqlite"
)

const (
	defaultConcurrency = 5
)

type CompactConfig struct {
	APIKey      string
	Concurrency int
	DryRun      bool
}

type Compactor struct {
	store  *sqlite.SQLiteStorage
	haiku  *HaikuClient
	config *CompactConfig
}

func New(store *sqlite.SQLiteStorage, apiKey string, config *CompactConfig) (*Compactor, error) {
	if config == nil {
		config = &CompactConfig{
			Concurrency: defaultConcurrency,
		}
	}
	if config.Concurrency <= 0 {
		config.Concurrency = defaultConcurrency
	}
	if apiKey != "" {
		config.APIKey = apiKey
	}

	var haikuClient *HaikuClient
	var err error
	if !config.DryRun {
		haikuClient, err = NewHaikuClient(config.APIKey)
		if err != nil {
			if errors.Is(err, ErrAPIKeyRequired) {
				config.DryRun = true
			} else {
				return nil, fmt.Errorf("failed to create Haiku client: %w", err)
			}
		}
	}

	return &Compactor{
		store:  store,
		haiku:  haikuClient,
		config: config,
	}, nil
}

type CompactResult struct {
	IssueID       string
	OriginalSize  int
	CompactedSize int
	Err           error
}

func (c *Compactor) CompactTier1(ctx context.Context, issueID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	eligible, reason, err := c.store.CheckEligibility(ctx, issueID, 1)
	if err != nil {
		return fmt.Errorf("failed to verify eligibility: %w", err)
	}

	if !eligible {
		if reason != "" {
			return fmt.Errorf("issue %s is not eligible for Tier 1 compaction: %s", issueID, reason)
		}
		return fmt.Errorf("issue %s is not eligible for Tier 1 compaction", issueID)
	}

	issue, err := c.store.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get issue: %w", err)
	}

	originalSize := len(issue.Description) + len(issue.Design) + len(issue.Notes) + len(issue.AcceptanceCriteria)

	if c.config.DryRun {
		return fmt.Errorf("dry-run: would compact %s (original size: %d bytes)", issueID, originalSize)
	}

	summary, err := c.haiku.SummarizeTier1(ctx, issue)
	if err != nil {
		return fmt.Errorf("failed to summarize with Haiku: %w", err)
	}

	compactedSize := len(summary)

	if compactedSize >= originalSize {
		warningMsg := fmt.Sprintf("Tier 1 compaction skipped: summary (%d bytes) not shorter than original (%d bytes)", compactedSize, originalSize)
		if err := c.store.AddComment(ctx, issueID, "compactor", warningMsg); err != nil {
			return fmt.Errorf("failed to record warning: %w", err)
		}
		return fmt.Errorf("compaction would increase size (%d → %d bytes), keeping original", originalSize, compactedSize)
	}

	updates := map[string]interface{}{
		"description":         summary,
		"design":              "",
		"notes":               "",
		"acceptance_criteria": "",
	}

	if err := c.store.UpdateIssue(ctx, issueID, updates, "compactor"); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	commitHash := GetCurrentCommitHash()
	if err := c.store.ApplyCompaction(ctx, issueID, 1, originalSize, compactedSize, commitHash); err != nil {
		return fmt.Errorf("failed to set compaction level: %w", err)
	}

	savingBytes := originalSize - compactedSize
	eventData := fmt.Sprintf("Tier 1 compaction: %d → %d bytes (saved %d)", originalSize, compactedSize, savingBytes)
	if err := c.store.AddComment(ctx, issueID, "compactor", eventData); err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	if err := c.store.MarkIssueDirty(ctx, issueID); err != nil {
		return fmt.Errorf("failed to mark dirty: %w", err)
	}

	return nil
}

func (c *Compactor) CompactTier1Batch(ctx context.Context, issueIDs []string) ([]*CompactResult, error) {
	if len(issueIDs) == 0 {
		return nil, nil
	}

	eligibleIDs := make([]string, 0, len(issueIDs))
	results := make([]*CompactResult, 0, len(issueIDs))

	for _, id := range issueIDs {
		eligible, reason, err := c.store.CheckEligibility(ctx, id, 1)
		if err != nil {
			results = append(results, &CompactResult{
				IssueID: id,
				Err:     fmt.Errorf("failed to verify eligibility: %w", err),
			})
			continue
		}
		if !eligible {
			results = append(results, &CompactResult{
				IssueID: id,
				Err:     fmt.Errorf("not eligible for Tier 1 compaction: %s", reason),
			})
		} else {
			eligibleIDs = append(eligibleIDs, id)
		}
	}

	if len(eligibleIDs) == 0 {
		return results, nil
	}

	if c.config.DryRun {
		for _, id := range eligibleIDs {
			issue, err := c.store.GetIssue(ctx, id)
			if err != nil {
				results = append(results, &CompactResult{
					IssueID: id,
					Err:     fmt.Errorf("failed to get issue: %w", err),
				})
				continue
			}
			originalSize := len(issue.Description) + len(issue.Design) + len(issue.Notes) + len(issue.AcceptanceCriteria)
			results = append(results, &CompactResult{
				IssueID:      id,
				OriginalSize: originalSize,
				Err:          nil,
			})
		}
		return results, nil
	}

	workCh := make(chan string, len(eligibleIDs))
	resultCh := make(chan *CompactResult, len(eligibleIDs))

	var wg sync.WaitGroup
	for i := 0; i < c.config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for issueID := range workCh {
				result := &CompactResult{IssueID: issueID}

				if err := c.compactSingleWithResult(ctx, issueID, result); err != nil {
					result.Err = err
				}

				resultCh <- result
			}
		}()
	}

	for _, id := range eligibleIDs {
		workCh <- id
	}
	close(workCh)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for result := range resultCh {
		results = append(results, result)
	}

	return results, nil
}

func (c *Compactor) compactSingleWithResult(ctx context.Context, issueID string, result *CompactResult) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	issue, err := c.store.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get issue: %w", err)
	}

	result.OriginalSize = len(issue.Description) + len(issue.Design) + len(issue.Notes) + len(issue.AcceptanceCriteria)

	summary, err := c.haiku.SummarizeTier1(ctx, issue)
	if err != nil {
		return fmt.Errorf("failed to summarize with Haiku: %w", err)
	}

	result.CompactedSize = len(summary)

	if result.CompactedSize >= result.OriginalSize {
		warningMsg := fmt.Sprintf("Tier 1 compaction skipped: summary (%d bytes) not shorter than original (%d bytes)", result.CompactedSize, result.OriginalSize)
		if err := c.store.AddComment(ctx, issueID, "compactor", warningMsg); err != nil {
			return fmt.Errorf("failed to record warning: %w", err)
		}
		return fmt.Errorf("compaction would increase size (%d → %d bytes), keeping original", result.OriginalSize, result.CompactedSize)
	}

	updates := map[string]interface{}{
		"description":         summary,
		"design":              "",
		"notes":               "",
		"acceptance_criteria": "",
	}

	if err := c.store.UpdateIssue(ctx, issueID, updates, "compactor"); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	commitHash := GetCurrentCommitHash()
	if err := c.store.ApplyCompaction(ctx, issueID, 1, result.OriginalSize, result.CompactedSize, commitHash); err != nil {
		return fmt.Errorf("failed to set compaction level: %w", err)
	}

	savingBytes := result.OriginalSize - result.CompactedSize
	eventData := fmt.Sprintf("Tier 1 compaction: %d → %d bytes (saved %d)", result.OriginalSize, result.CompactedSize, savingBytes)
	if err := c.store.AddComment(ctx, issueID, "compactor", eventData); err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	if err := c.store.MarkIssueDirty(ctx, issueID); err != nil {
		return fmt.Errorf("failed to mark dirty: %w", err)
	}

	return nil
}
