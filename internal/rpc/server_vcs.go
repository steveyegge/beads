package rpc

import (
	"encoding/json"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
)

// getDoltStore extracts the DoltStore from storage, returning an error response if not available.
func (s *Server) getDoltStore() (*dolt.DoltStore, *Response) {
	store := s.storage
	if store == nil {
		return nil, &Response{Success: false, Error: "storage not available"}
	}

	ds, ok := store.(*dolt.DoltStore)
	if !ok {
		return nil, &Response{Success: false, Error: "VCS operations require Dolt storage backend"}
	}
	return ds, nil
}

// getVersionedStorage extracts VersionedStorage from storage, returning an error response if not available.
func (s *Server) getVersionedStorage() (storage.VersionedStorage, *Response) {
	store := s.storage
	if store == nil {
		return nil, &Response{Success: false, Error: "storage not available"}
	}

	vs, ok := storage.AsVersioned(store)
	if !ok {
		return nil, &Response{Success: false, Error: "VCS operations require versioned storage backend"}
	}
	return vs, nil
}

func (s *Server) handleVcsCommit(req *Request) Response {
	var args VcsCommitArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid vcs_commit args: %v", err)}
	}
	if args.Message == "" {
		return Response{Success: false, Error: "message is required"}
	}

	vs, errResp := s.getVersionedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := vs.Commit(ctx, args.Message); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs commit failed: %v", err)}
	}

	data, _ := json.Marshal(VcsCommitResult{Success: true})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsPush(req *Request) Response {
	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	rs, ok := storage.AsRemote(store)
	if !ok {
		return Response{Success: false, Error: "vcs push requires remote storage backend"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := rs.Push(ctx); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs push failed: %v", err)}
	}

	data, _ := json.Marshal(VcsPushResult{Success: true})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsPull(req *Request) Response {
	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	rs, ok := storage.AsRemote(store)
	if !ok {
		return Response{Success: false, Error: "vcs pull requires remote storage backend"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := rs.Pull(ctx); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs pull failed: %v", err)}
	}

	data, _ := json.Marshal(VcsPullResult{Success: true})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsMerge(req *Request) Response {
	var args VcsMergeArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid vcs_merge args: %v", err)}
	}
	if args.Branch == "" {
		return Response{Success: false, Error: "branch is required"}
	}

	vs, errResp := s.getVersionedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	conflicts, err := vs.Merge(ctx, args.Branch)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs merge failed: %v", err)}
	}

	result := VcsMergeResult{Success: true}
	for _, c := range conflicts {
		result.Conflicts = append(result.Conflicts, VcsConflict{
			IssueID:     c.IssueID,
			Field:       c.Field,
			OursValue:   fmt.Sprintf("%v", c.OursValue),
			TheirsValue: fmt.Sprintf("%v", c.TheirsValue),
		})
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsBranchCreate(req *Request) Response {
	var args VcsBranchCreateArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid vcs_branch_create args: %v", err)}
	}
	if args.Name == "" {
		return Response{Success: false, Error: "name is required"}
	}

	vs, errResp := s.getVersionedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := vs.Branch(ctx, args.Name); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs branch create failed: %v", err)}
	}

	data, _ := json.Marshal(VcsBranchCreateResult{Name: args.Name})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsBranchDelete(req *Request) Response {
	var args VcsBranchDeleteArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid vcs_branch_delete args: %v", err)}
	}
	if args.Name == "" {
		return Response{Success: false, Error: "name is required"}
	}

	// DeleteBranch is only on DoltStore, not on VersionedStorage interface
	ds, errResp := s.getDoltStore()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := ds.DeleteBranch(ctx, args.Name); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs branch delete failed: %v", err)}
	}

	data, _ := json.Marshal(VcsBranchDeleteResult{Name: args.Name})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsCheckout(req *Request) Response {
	var args VcsCheckoutArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid vcs_checkout args: %v", err)}
	}
	if args.Branch == "" {
		return Response{Success: false, Error: "branch is required"}
	}

	vs, errResp := s.getVersionedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := vs.Checkout(ctx, args.Branch); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs checkout failed: %v", err)}
	}

	data, _ := json.Marshal(VcsCheckoutResult{Branch: args.Branch})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsActiveBranch(req *Request) Response {
	vs, errResp := s.getVersionedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	branch, err := vs.CurrentBranch(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs active branch failed: %v", err)}
	}

	data, _ := json.Marshal(VcsActiveBranchResult{Branch: branch})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsStatus(req *Request) Response {
	// Status() is only on DoltStore, not on VersionedStorage interface
	ds, errResp := s.getDoltStore()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	status, err := ds.Status(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs status failed: %v", err)}
	}

	result := VcsStatusResult{
		Staged:   make([]VcsStatusEntry, 0, len(status.Staged)),
		Unstaged: make([]VcsStatusEntry, 0, len(status.Unstaged)),
	}
	for _, e := range status.Staged {
		result.Staged = append(result.Staged, VcsStatusEntry{Table: e.Table, Status: e.Status})
	}
	for _, e := range status.Unstaged {
		result.Unstaged = append(result.Unstaged, VcsStatusEntry{Table: e.Table, Status: e.Status})
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsHasUncommitted(req *Request) Response {
	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	sc, ok := storage.AsStatusChecker(store)
	if !ok {
		return Response{Success: false, Error: "vcs has_uncommitted requires status checker storage backend"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	hasChanges, err := sc.HasUncommittedChanges(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs has_uncommitted failed: %v", err)}
	}

	data, _ := json.Marshal(VcsHasUncommittedResult{HasUncommitted: hasChanges})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsBranches(req *Request) Response {
	vs, errResp := s.getVersionedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	branches, err := vs.ListBranches(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs branches failed: %v", err)}
	}

	data, _ := json.Marshal(VcsBranchesResult{Branches: branches})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsCurrentCommit(req *Request) Response {
	vs, errResp := s.getVersionedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	hash, err := vs.GetCurrentCommit(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs current commit failed: %v", err)}
	}

	data, _ := json.Marshal(VcsCurrentCommitResult{Hash: hash})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsCommitExists(req *Request) Response {
	var args VcsCommitExistsArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid vcs_commit_exists args: %v", err)}
	}
	if args.Hash == "" {
		return Response{Success: false, Error: "hash is required"}
	}

	// CommitExists is only on DoltStore
	ds, errResp := s.getDoltStore()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	exists, err := ds.CommitExists(ctx, args.Hash)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs commit exists failed: %v", err)}
	}

	data, _ := json.Marshal(VcsCommitExistsResult{Exists: exists})
	return Response{Success: true, Data: data}
}

func (s *Server) handleVcsLog(req *Request) Response {
	var args VcsLogArgs
	if req.Args != nil {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid vcs_log args: %v", err)}
		}
	}
	if args.Limit <= 0 {
		args.Limit = 10
	}

	// Log is only on DoltStore
	ds, errResp := s.getDoltStore()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	commits, err := ds.Log(ctx, args.Limit)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("vcs log failed: %v", err)}
	}

	result := VcsLogResult{
		Commits: make([]VcsLogEntry, 0, len(commits)),
	}
	for _, c := range commits {
		result.Commits = append(result.Commits, VcsLogEntry{
			Hash:    c.Hash,
			Author:  c.Author,
			Email:   c.Email,
			Date:    c.Date,
			Message: c.Message,
		})
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}
