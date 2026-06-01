// Stubs for Storage interface methods added after be-fqjs3v/be-ht5qm4 branched.
// Count methods delegate via RPC. Iter methods use SliceIter wrappers.
// Replace with full RPC implementations as the transport matures.

package rpc

import (
	"context"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// --- SearchIssuesWithCounts ---

func (c *daemonClient) SearchIssuesWithCounts(ctx context.Context, query string, filter types.IssueFilter) ([]*types.IssueWithCounts, error) {
	args := &SearchIssuesWithCountsArgs{Query: query, Filter: filter}
	var reply SearchIssuesWithCountsReply
	if err := c.client.Call("daemonServer.SearchIssuesWithCounts", args, &reply); err != nil {
		return nil, err
	}
	return reply.Issues, decodeRPCError(reply.RPCError)
}

// --- GetReadyWorkWithCounts ---

func (c *daemonClient) GetReadyWorkWithCounts(ctx context.Context, filter types.WorkFilter) ([]*types.IssueWithCounts, error) {
	args := &GetReadyWorkWithCountsArgs{Filter: filter}
	var reply GetReadyWorkWithCountsReply
	if err := c.client.Call("daemonServer.GetReadyWorkWithCounts", args, &reply); err != nil {
		return nil, err
	}
	return reply.Issues, decodeRPCError(reply.RPCError)
}

// --- Count methods ---

func (c *daemonClient) CountIssues(ctx context.Context, query string, filter types.IssueFilter) (int64, error) {
	args := &CountIssuesArgs{Query: query, Filter: filter}
	var reply CountIssuesReply
	if err := c.client.Call("daemonServer.CountIssues", args, &reply); err != nil {
		return 0, err
	}
	return reply.Count, decodeRPCError(reply.RPCError)
}

func (c *daemonClient) CountIssuesByGroup(ctx context.Context, filter types.IssueFilter, groupBy string) (map[string]int, error) {
	args := &CountIssuesByGroupArgs{Filter: filter, GroupBy: groupBy}
	var reply CountIssuesByGroupReply
	if err := c.client.Call("daemonServer.CountIssuesByGroup", args, &reply); err != nil {
		return nil, err
	}
	return reply.Counts, decodeRPCError(reply.RPCError)
}

func (c *daemonClient) CountDependents(ctx context.Context, issueID string) (int64, error) {
	args := &CountDependentsArgs{IssueID: issueID}
	var reply CountDependentsReply
	if err := c.client.Call("daemonServer.CountDependents", args, &reply); err != nil {
		return 0, err
	}
	return reply.Count, decodeRPCError(reply.RPCError)
}

func (c *daemonClient) CountDependencies(ctx context.Context, issueID string) (int64, error) {
	args := &CountDependenciesArgs{IssueID: issueID}
	var reply CountDependenciesReply
	if err := c.client.Call("daemonServer.CountDependencies", args, &reply); err != nil {
		return 0, err
	}
	return reply.Count, decodeRPCError(reply.RPCError)
}

func (c *daemonClient) CountIssueComments(ctx context.Context, issueID string) (int64, error) {
	args := &CountIssueCommentsArgs{IssueID: issueID}
	var reply CountIssueCommentsReply
	if err := c.client.Call("daemonServer.CountIssueComments", args, &reply); err != nil {
		return 0, err
	}
	return reply.Count, decodeRPCError(reply.RPCError)
}

func (c *daemonClient) CountEvents(ctx context.Context, issueID string, limit int) (int64, error) {
	args := &CountEventsArgs{IssueID: issueID, Limit: limit}
	var reply CountEventsReply
	if err := c.client.Call("daemonServer.CountEvents", args, &reply); err != nil {
		return 0, err
	}
	return reply.Count, decodeRPCError(reply.RPCError)
}

// --- Iter methods (SliceIter wrappers until full streaming lands) ---

func (c *daemonClient) IterIssues(ctx context.Context, query string, filter types.IssueFilter) (storage.Iter[types.Issue], error) {
	issues, err := c.SearchIssues(ctx, query, filter)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(issues), nil
}

func (c *daemonClient) IterDependentsWithMetadata(ctx context.Context, issueID string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	items, err := c.GetDependentsWithMetadata(ctx, issueID)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(items), nil
}

func (c *daemonClient) IterDependenciesWithMetadata(ctx context.Context, issueID string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	items, err := c.GetDependenciesWithMetadata(ctx, issueID)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(items), nil
}

func (c *daemonClient) IterIssueComments(ctx context.Context, issueID string) (storage.Iter[types.Comment], error) {
	items, err := c.GetIssueComments(ctx, issueID)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(items), nil
}

func (c *daemonClient) IterEvents(ctx context.Context, issueID string, limit int) (storage.Iter[types.Event], error) {
	items, err := c.GetEvents(ctx, issueID, limit)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(items), nil
}

func (c *daemonClient) IterAllEventsSince(ctx context.Context, since time.Time) (storage.Iter[types.Event], error) {
	items, err := c.GetAllEventsSince(ctx, since)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(items), nil
}

func (c *daemonClient) IterReadyWork(ctx context.Context, filter types.WorkFilter) (storage.Iter[types.Issue], error) {
	items, err := c.GetReadyWork(ctx, filter)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(items), nil
}

func (c *daemonClient) IterBlockedIssues(ctx context.Context, filter types.WorkFilter) (storage.Iter[types.BlockedIssue], error) {
	items, err := c.GetBlockedIssues(ctx, filter)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(items), nil
}

func (c *daemonClient) IterWisps(ctx context.Context, filter types.WispFilter) (storage.Iter[types.Issue], error) {
	items, err := c.ListWisps(ctx, filter)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(items), nil
}
