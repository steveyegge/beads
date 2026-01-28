package rpc

import (
	"encoding/json"
	"fmt"
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
