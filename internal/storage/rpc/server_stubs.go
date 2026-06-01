// Server-side stubs for Storage interface methods added after be-fqjs3v/be-ht5qm4 branched.

package rpc

func (s *daemonServer) SearchIssuesWithCounts(args *SearchIssuesWithCountsArgs, reply *SearchIssuesWithCountsReply) error {
	ctx, cancel := daemonCallContext(s.root)
	defer cancel()
	r0, err := s.store.SearchIssuesWithCounts(ctx, args.Query, args.Filter)
	reply.Issues = r0
	reply.RPCError = encodeRPCError(err)
	return nil
}

func (s *daemonServer) GetReadyWorkWithCounts(args *GetReadyWorkWithCountsArgs, reply *GetReadyWorkWithCountsReply) error {
	ctx, cancel := daemonCallContext(s.root)
	defer cancel()
	r0, err := s.store.GetReadyWorkWithCounts(ctx, args.Filter)
	reply.Issues = r0
	reply.RPCError = encodeRPCError(err)
	return nil
}

func (s *daemonServer) CountIssues(args *CountIssuesArgs, reply *CountIssuesReply) error {
	ctx, cancel := daemonCallContext(s.root)
	defer cancel()
	r0, err := s.store.CountIssues(ctx, args.Query, args.Filter)
	reply.Count = r0
	reply.RPCError = encodeRPCError(err)
	return nil
}

func (s *daemonServer) CountIssuesByGroup(args *CountIssuesByGroupArgs, reply *CountIssuesByGroupReply) error {
	ctx, cancel := daemonCallContext(s.root)
	defer cancel()
	r0, err := s.store.CountIssuesByGroup(ctx, args.Filter, args.GroupBy)
	reply.Counts = r0
	reply.RPCError = encodeRPCError(err)
	return nil
}

func (s *daemonServer) CountDependents(args *CountDependentsArgs, reply *CountDependentsReply) error {
	ctx, cancel := daemonCallContext(s.root)
	defer cancel()
	r0, err := s.store.CountDependents(ctx, args.IssueID)
	reply.Count = r0
	reply.RPCError = encodeRPCError(err)
	return nil
}

func (s *daemonServer) CountDependencies(args *CountDependenciesArgs, reply *CountDependenciesReply) error {
	ctx, cancel := daemonCallContext(s.root)
	defer cancel()
	r0, err := s.store.CountDependencies(ctx, args.IssueID)
	reply.Count = r0
	reply.RPCError = encodeRPCError(err)
	return nil
}

func (s *daemonServer) CountIssueComments(args *CountIssueCommentsArgs, reply *CountIssueCommentsReply) error {
	ctx, cancel := daemonCallContext(s.root)
	defer cancel()
	r0, err := s.store.CountIssueComments(ctx, args.IssueID)
	reply.Count = r0
	reply.RPCError = encodeRPCError(err)
	return nil
}

func (s *daemonServer) CountEvents(args *CountEventsArgs, reply *CountEventsReply) error {
	ctx, cancel := daemonCallContext(s.root)
	defer cancel()
	r0, err := s.store.CountEvents(ctx, args.IssueID, args.Limit)
	reply.Count = r0
	reply.RPCError = encodeRPCError(err)
	return nil
}
