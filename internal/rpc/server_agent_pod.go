package rpc

import (
	"encoding/json"
	"fmt"
	"time"
)

// handleAgentPodRegister sets pod fields on an agent bead.
func (s *Server) handleAgentPodRegister(req *Request) Response {
	var args AgentPodRegisterArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid agent_pod_register args: %v", err),
		}
	}

	if args.AgentID == "" {
		return Response{Success: false, Error: "agent_id is required"}
	}
	if args.PodName == "" {
		return Response{Success: false, Error: "pod_name is required"}
	}

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Default pod_status to "running" if not specified
	podStatus := args.PodStatus
	if podStatus == "" {
		podStatus = "running"
	}

	updates := map[string]interface{}{
		"pod_name":       args.PodName,
		"pod_ip":         args.PodIP,
		"pod_node":       args.PodNode,
		"pod_status":     podStatus,
		"screen_session": args.ScreenSession,
		"last_activity":  time.Now(),
	}

	if err := store.UpdateIssue(ctx, args.AgentID, updates, req.Actor); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to register pod for agent %s: %v", args.AgentID, err),
		}
	}

	s.emitRichMutation(MutationEvent{
		Type:    MutationUpdate,
		IssueID: args.AgentID,
		Actor:   req.Actor,
	})

	result := AgentPodRegisterResult{
		AgentID:   args.AgentID,
		PodName:   args.PodName,
		PodStatus: podStatus,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleAgentPodDeregister clears all pod fields on an agent bead.
func (s *Server) handleAgentPodDeregister(req *Request) Response {
	var args AgentPodDeregisterArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid agent_pod_deregister args: %v", err),
		}
	}

	if args.AgentID == "" {
		return Response{Success: false, Error: "agent_id is required"}
	}

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	updates := map[string]interface{}{
		"pod_name":       "",
		"pod_ip":         "",
		"pod_node":       "",
		"pod_status":     "",
		"screen_session": "",
		"last_activity":  time.Now(),
	}

	if err := store.UpdateIssue(ctx, args.AgentID, updates, req.Actor); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to deregister pod for agent %s: %v", args.AgentID, err),
		}
	}

	s.emitRichMutation(MutationEvent{
		Type:    MutationUpdate,
		IssueID: args.AgentID,
		Actor:   req.Actor,
	})

	result := AgentPodDeregisterResult{
		AgentID: args.AgentID,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleAgentPodStatus updates only the pod_status field on an agent bead.
func (s *Server) handleAgentPodStatus(req *Request) Response {
	var args AgentPodStatusArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid agent_pod_status args: %v", err),
		}
	}

	if args.AgentID == "" {
		return Response{Success: false, Error: "agent_id is required"}
	}
	if args.PodStatus == "" {
		return Response{Success: false, Error: "pod_status is required"}
	}

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	updates := map[string]interface{}{
		"pod_status":    args.PodStatus,
		"last_activity": time.Now(),
	}

	if err := store.UpdateIssue(ctx, args.AgentID, updates, req.Actor); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to update pod status for agent %s: %v", args.AgentID, err),
		}
	}

	s.emitRichMutation(MutationEvent{
		Type:    MutationUpdate,
		IssueID: args.AgentID,
		Actor:   req.Actor,
	})

	result := AgentPodStatusResult{
		AgentID:   args.AgentID,
		PodStatus: args.PodStatus,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleAgentPodList returns agents with active pods (pod_name != '').
func (s *Server) handleAgentPodList(req *Request) Response {
	var args AgentPodListArgs
	if req.Args != nil {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("invalid agent_pod_list args: %v", err),
			}
		}
	}

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// List all agent beads by label
	issues, err := store.GetIssuesByLabel(ctx, "gt:agent")
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to list agents: %v", err),
		}
	}

	// Filter to agents with active pods
	var agents []AgentPodInfo
	for _, issue := range issues {
		if issue.PodName == "" {
			continue
		}
		// Filter by rig if specified
		if args.Rig != "" && issue.Rig != args.Rig {
			continue
		}
		agents = append(agents, AgentPodInfo{
			AgentID:       issue.ID,
			PodName:       issue.PodName,
			PodIP:         issue.PodIP,
			PodNode:       issue.PodNode,
			PodStatus:     issue.PodStatus,
			ScreenSession: issue.ScreenSession,
			AgentState:    string(issue.AgentState),
			Rig:           issue.Rig,
			RoleType:      issue.RoleType,
		})
	}

	result := AgentPodListResult{Agents: agents}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}
