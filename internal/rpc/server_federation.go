// Package rpc provides RPC server handlers for federation operations (bd-ma0s.4).
// These handlers enable federation commands to work in daemon mode via RPC.
package rpc

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
)

// getFederatedStorage attempts to cast the server's storage to FederatedStorage.
// Returns an error response if federation is not supported.
func (s *Server) getFederatedStorage() (storage.FederatedStorage, *Response) {
	fs, ok := storage.AsFederated(s.storage)
	if !ok {
		resp := Response{
			Success: false,
			Error:   "federation requires Dolt backend (current backend does not support federation)",
		}
		return nil, &resp
	}
	return fs, nil
}

// getFederationDoltStore attempts to get the concrete DoltStore from storage via federation interface.
// Some operations (like Sync) require the concrete type for complex orchestration.
func (s *Server) getFederationDoltStore() (*dolt.DoltStore, *Response) {
	fs, errResp := s.getFederatedStorage()
	if errResp != nil {
		return nil, errResp
	}
	ds, ok := fs.(*dolt.DoltStore)
	if !ok {
		resp := Response{
			Success: false,
			Error:   "internal error: federated storage is not DoltStore",
		}
		return nil, &resp
	}
	return ds, nil
}

// handleFedListRemotes handles the fed_list_remotes RPC operation.
func (s *Server) handleFedListRemotes(req *Request) Response {
	fs, errResp := s.getFederatedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	remotes, err := fs.ListRemotes(ctx)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to list remotes: %v", err)}
	}

	result := FedListRemotesResult{
		Remotes: make([]FedRemoteInfo, len(remotes)),
	}
	for i, r := range remotes {
		result.Remotes[i] = FedRemoteInfo{Name: r.Name, URL: r.URL}
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFedSync handles the fed_sync RPC operation.
// This is the most complex federation operation: fetch → merge → conflict resolution → push.
func (s *Server) handleFedSync(req *Request) Response {
	var args FedSyncArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}
	if args.Peer == "" {
		return Response{Success: false, Error: "peer is required"}
	}

	// Validate strategy if provided
	if args.Strategy != "" && args.Strategy != "ours" && args.Strategy != "theirs" {
		return Response{Success: false, Error: fmt.Sprintf("invalid strategy %q: must be 'ours' or 'theirs'", args.Strategy)}
	}

	ds, errResp := s.getFederationDoltStore()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	start := time.Now()
	syncResult, err := ds.Sync(ctx, args.Peer, args.Strategy)

	result := FedSyncResult{
		Peer:       args.Peer,
		DurationMs: time.Since(start).Milliseconds(),
	}

	if syncResult != nil {
		result.Fetched = syncResult.Fetched
		result.Merged = syncResult.Merged
		result.Pushed = syncResult.Pushed
		result.PulledCommits = syncResult.PulledCommits
		result.ConflictsResolved = syncResult.ConflictsResolved

		if len(syncResult.Conflicts) > 0 {
			result.Conflicts = make([]FedConflictInfo, len(syncResult.Conflicts))
			for i, c := range syncResult.Conflicts {
				result.Conflicts[i] = FedConflictInfo{
					IssueID: c.IssueID,
					Field:   c.Field,
				}
			}
		}

		if syncResult.PushError != nil {
			result.PushError = syncResult.PushError.Error()
		}
	}

	if err != nil {
		// Return partial result with error
		data, _ := json.Marshal(result)
		return Response{Success: false, Error: err.Error(), Data: data}
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFedSyncStatus handles the fed_sync_status RPC operation.
func (s *Server) handleFedSyncStatus(req *Request) Response {
	var args FedSyncStatusArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}
	if args.Peer == "" {
		return Response{Success: false, Error: "peer is required"}
	}

	fs, errResp := s.getFederatedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	status, err := fs.SyncStatus(ctx, args.Peer)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to get sync status: %v", err)}
	}

	result := FedSyncStatusResult{
		Peer:         status.Peer,
		LocalAhead:   status.LocalAhead,
		LocalBehind:  status.LocalBehind,
		HasConflicts: status.HasConflicts,
	}
	if !status.LastSync.IsZero() {
		result.LastSync = status.LastSync.Format(time.RFC3339)
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFedFetch handles the fed_fetch RPC operation.
func (s *Server) handleFedFetch(req *Request) Response {
	var args FedFetchArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}
	if args.Peer == "" {
		return Response{Success: false, Error: "peer is required"}
	}

	fs, errResp := s.getFederatedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := fs.Fetch(ctx, args.Peer); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to fetch from %s: %v", args.Peer, err)}
	}

	result := FedFetchResult{Peer: args.Peer}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFedPushTo handles the fed_push_to RPC operation.
func (s *Server) handleFedPushTo(req *Request) Response {
	var args FedPushToArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}
	if args.Peer == "" {
		return Response{Success: false, Error: "peer is required"}
	}

	fs, errResp := s.getFederatedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := fs.PushTo(ctx, args.Peer); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to push to %s: %v", args.Peer, err)}
	}

	result := FedPushToResult{Peer: args.Peer}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFedPullFrom handles the fed_pull_from RPC operation.
func (s *Server) handleFedPullFrom(req *Request) Response {
	var args FedPullFromArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}
	if args.Peer == "" {
		return Response{Success: false, Error: "peer is required"}
	}

	fs, errResp := s.getFederatedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	conflicts, err := fs.PullFrom(ctx, args.Peer)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to pull from %s: %v", args.Peer, err)}
	}

	result := FedPullFromResult{Peer: args.Peer}
	if len(conflicts) > 0 {
		result.Conflicts = make([]FedConflictInfo, len(conflicts))
		for i, c := range conflicts {
			result.Conflicts[i] = FedConflictInfo{
				IssueID: c.IssueID,
				Field:   c.Field,
			}
		}
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFedAddRemote handles the fed_add_remote RPC operation.
func (s *Server) handleFedAddRemote(req *Request) Response {
	var args FedAddRemoteArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}
	if args.Name == "" {
		return Response{Success: false, Error: "name is required"}
	}
	if args.URL == "" {
		return Response{Success: false, Error: "url is required"}
	}

	fs, errResp := s.getFederatedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := fs.AddRemote(ctx, args.Name, args.URL); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to add remote: %v", err)}
	}

	result := FedAddRemoteResult{Name: args.Name, URL: args.URL}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFedRemoveRemote handles the fed_remove_remote RPC operation.
func (s *Server) handleFedRemoveRemote(req *Request) Response {
	var args FedRemoveRemoteArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}
	if args.Name == "" {
		return Response{Success: false, Error: "name is required"}
	}

	fs, errResp := s.getFederatedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	if err := fs.RemoveRemote(ctx, args.Name); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to remove remote: %v", err)}
	}

	result := FedRemoveRemoteResult{Name: args.Name}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFedAddPeer handles the fed_add_peer RPC operation.
// This is security-sensitive: creates SQL users and sets up remotes with credentials.
func (s *Server) handleFedAddPeer(req *Request) Response {
	var args FedAddPeerArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}
	if args.Name == "" {
		return Response{Success: false, Error: "name is required"}
	}
	if args.URL == "" {
		return Response{Success: false, Error: "url is required"}
	}

	fs, errResp := s.getFederatedStorage()
	if errResp != nil {
		return *errResp
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	peer := &storage.FederationPeer{
		Name:        args.Name,
		RemoteURL:   args.URL,
		Username:    args.Username,
		Password:    args.Password,
		Sovereignty: args.Sovereignty,
	}

	if err := fs.AddFederationPeer(ctx, peer); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to add federation peer: %v", err)}
	}

	result := FedAddPeerResult{
		Name:        args.Name,
		URL:         args.URL,
		HasAuth:     args.Username != "",
		Sovereignty: args.Sovereignty,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}
