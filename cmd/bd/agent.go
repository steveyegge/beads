package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// Valid agent states for state command
var validAgentStates = map[string]bool{
	"idle":     true, // Agent is waiting for work
	"spawning": true, // Agent is starting up
	"running":  true, // Agent is executing (general)
	"working":  true, // Agent is actively working on a task
	"stuck":    true, // Agent is blocked and needs help
	"done":     true, // Agent completed its current work
	"stopped":  true, // Agent has cleanly shut down
	"dead":     true, // Agent died without clean shutdown
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agent bead state",
	Long: `Manage state on agent beads for ZFC-compliant state reporting.

Agent beads (labeled gt:agent) can self-report their state using these commands.
This enables the Witness and other monitoring systems to track agent health.

States:
  idle      - Agent is waiting for work
  spawning  - Agent is starting up
  running   - Agent is executing (general)
  working   - Agent is actively working on a task
  stuck     - Agent is blocked and needs help
  done      - Agent completed its current work
  stopped   - Agent has cleanly shut down
  dead      - Agent died without clean shutdown (set by Witness via timeout)

Examples:
  bd agent state gt-emma running     # Set emma's state to running
  bd agent heartbeat gt-emma         # Update emma's last_activity timestamp
  bd agent show gt-emma              # Show emma's agent details`,
}

var agentStateCmd = &cobra.Command{
	Use:   "state <agent> <state>",
	Short: "Set agent state",
	Long: `Set the state of an agent bead.

This updates both the agent_state field and the last_activity timestamp.
Use this for ZFC-compliant state reporting.

Valid states: idle, spawning, running, working, stuck, done, stopped, dead

Examples:
  bd agent state gt-emma running   # Set state to running
  bd agent state gt-mayor idle     # Set state to idle`,
	Args: cobra.ExactArgs(2),
	RunE: runAgentState,
}

var agentHeartbeatCmd = &cobra.Command{
	Use:   "heartbeat <agent>",
	Short: "Update agent last_activity timestamp",
	Long: `Update the last_activity timestamp of an agent bead without changing state.

Use this for periodic heartbeats to indicate the agent is still alive.
The Witness can use this to detect dead agents via timeout.

Examples:
  bd agent heartbeat gt-emma   # Update emma's last_activity
  bd agent heartbeat gt-mayor  # Update mayor's last_activity`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentHeartbeat,
}

var agentShowCmd = &cobra.Command{
	Use:   "show <agent>",
	Short: "Show agent bead details",
	Long: `Show detailed information about an agent bead.

Displays agent-specific fields including state, last_activity, hook, and role.

Examples:
  bd agent show gt-emma   # Show emma's agent details
  bd agent show gt-mayor  # Show mayor's agent details`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentShow,
}

var agentBackfillLabelsCmd = &cobra.Command{
	Use:   "backfill-labels",
	Short: "Backfill role_type/rig labels on existing agent beads",
	Long: `Backfill role_type and rig labels on existing agent beads.

This command scans all agent beads and:
1. Extracts role_type and rig from description text if fields are empty
2. Sets the role_type and rig fields on the agent bead
3. Adds role_type:<value> and rig:<value> labels for filtering

This enables queries like:
  bd list --type=agent --label=role_type:witness
  bd list --type=agent --label=rig:gastown

Use --dry-run to see what would be changed without making changes.

Examples:
  bd agent backfill-labels           # Backfill all agent beads
  bd agent backfill-labels --dry-run # Preview changes without applying`,
	RunE: runAgentBackfillLabels,
}

var backfillDryRun bool

var agentPodRegisterCmd = &cobra.Command{
	Use:   "pod-register <agent>",
	Short: "Register a K8s pod for an agent",
	Long: `Register a Kubernetes pod for an agent bead.

Sets the pod fields (pod_name, pod_ip, pod_node, screen_session) and
updates last_activity. If pod_status is not specified, defaults to "running".

This command requires the daemon (BD_DAEMON_HOST).

Examples:
  bd agent pod-register gt-emma --pod-name=emma-pod-abc --pod-ip=10.0.1.5
  bd agent pod-register gt-emma --pod-name=emma-pod-abc --pod-node=node-1 --screen=emma-screen`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentPodRegister,
}

var agentPodDeregisterCmd = &cobra.Command{
	Use:   "pod-deregister <agent>",
	Short: "Deregister a K8s pod from an agent",
	Long: `Deregister (clear) pod fields from an agent bead.

Clears all pod fields (pod_name, pod_ip, pod_node, pod_status, screen_session)
and updates last_activity.

This command requires the daemon (BD_DAEMON_HOST).

Examples:
  bd agent pod-deregister gt-emma`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentPodDeregister,
}

var agentPodStatusCmd = &cobra.Command{
	Use:   "pod-status <agent> --status=<status>",
	Short: "Update pod status for an agent",
	Long: `Update the pod_status field on an agent bead.

Updates only the pod_status field and last_activity timestamp.

This command requires the daemon (BD_DAEMON_HOST).

Examples:
  bd agent pod-status gt-emma --status=running
  bd agent pod-status gt-emma --status=terminating`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentPodStatus,
}

var agentPodListCmd = &cobra.Command{
	Use:   "pod-list",
	Short: "List agents with active pods",
	Long: `List all agent beads that have active pods registered.

Optionally filter by rig name.

This command requires the daemon (BD_DAEMON_HOST).

Examples:
  bd agent pod-list
  bd agent pod-list --rig=beads`,
	RunE: runAgentPodList,
}

var podRegisterName string
var podRegisterIP string
var podRegisterNode string
var podRegisterStatus string
var podRegisterScreen string
var podStatusValue string
var podListRig string

var agentSubscriptionsCmd = &cobra.Command{
	Use:   "subscriptions <agent>",
	Short: "Show agent's effective advice subscriptions",
	Long: `Show the effective advice subscriptions for an agent bead.

This displays how advice is delivered to the agent by showing:
- Auto-subscriptions: Labels derived from agent identity (global, agent:X, rig:X, role:X)
- Custom: Additional labels from Issue.AdviceSubscriptions field
- Excluded: Labels opted out via Issue.AdviceSubscriptionsExclude field
- Final: The merged list of effective subscriptions (auto + custom - excluded)

Examples:
  bd agent subscriptions gt-emma          # Show emma's subscriptions
  bd agent subscriptions gt-gastown-witness  # Show witness subscriptions`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentSubscriptions,
}

func init() {
	agentBackfillLabelsCmd.Flags().BoolVar(&backfillDryRun, "dry-run", false, "Preview changes without applying them")

	agentPodRegisterCmd.Flags().StringVar(&podRegisterName, "pod-name", "", "K8s pod name (required)")
	agentPodRegisterCmd.Flags().StringVar(&podRegisterIP, "pod-ip", "", "Pod IP address")
	agentPodRegisterCmd.Flags().StringVar(&podRegisterNode, "pod-node", "", "K8s node name")
	agentPodRegisterCmd.Flags().StringVar(&podRegisterStatus, "status", "", "Pod status (default: running)")
	agentPodRegisterCmd.Flags().StringVar(&podRegisterScreen, "screen", "", "Screen/tmux session name")
	_ = agentPodRegisterCmd.MarkFlagRequired("pod-name")

	agentPodStatusCmd.Flags().StringVar(&podStatusValue, "status", "", "Pod status value (required)")
	_ = agentPodStatusCmd.MarkFlagRequired("status")

	agentPodListCmd.Flags().StringVar(&podListRig, "rig", "", "Filter by rig name")

	agentCmd.AddCommand(agentStateCmd)
	agentCmd.AddCommand(agentHeartbeatCmd)
	agentCmd.AddCommand(agentShowCmd)
	agentCmd.AddCommand(agentBackfillLabelsCmd)
	agentCmd.AddCommand(agentSubscriptionsCmd)
	agentCmd.AddCommand(agentPodRegisterCmd)
	agentCmd.AddCommand(agentPodDeregisterCmd)
	agentCmd.AddCommand(agentPodStatusCmd)
	agentCmd.AddCommand(agentPodListCmd)
	rootCmd.AddCommand(agentCmd)
}

func runAgentState(cmd *cobra.Command, args []string) error {
	CheckReadonly("agent state")

	agentArg := args[0]
	state := strings.ToLower(args[1])

	// Validate state
	if !validAgentStates[state] {
		validList := []string{}
		for s := range validAgentStates {
			validList = append(validList, s)
		}
		return fmt.Errorf("invalid state %q; valid states: %s", state, strings.Join(validList, ", "))
	}

	ctx := rootCtx

	// Resolve agent ID with centralized routing (bd-z344)
	res, notFound, err := resolveAgentID(ctx, agentArg)
	if err != nil {
		return err
	}
	defer res.Close()
	agentID := res.AgentID

	var agent *types.Issue

	// If agent not found, auto-create it
	if notFound {
		roleType, rig := parseAgentIDFields(agentID)
		agent = &types.Issue{
			ID:        agentID,
			Title:     fmt.Sprintf("Agent: %s", agentID),
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			RoleType:  roleType,
			Rig:       rig,
			CreatedBy: actor,
		}

		if res.useDaemon(agentArg) {
			labels := []string{"gt:agent"}
			if roleType != "" {
				labels = append(labels, "role_type:"+roleType)
			}
			if rig != "" {
				labels = append(labels, "rig:"+rig)
			}
			createArgs := &rpc.CreateArgs{
				ID:        agentID,
				Title:     agent.Title,
				IssueType: string(types.TypeTask),
				RoleType:  roleType,
				Rig:       rig,
				CreatedBy: actor,
				Labels:    labels,
			}
			resp, err := daemonClient.Create(createArgs)
			if err != nil {
				return fmt.Errorf("failed to auto-create agent bead %s: %w", agentID, err)
			}
			if err := json.Unmarshal(resp.Data, &agent); err != nil {
				return fmt.Errorf("parsing create response: %w", err)
			}
		} else {
			if err := res.ActiveStore.CreateIssue(ctx, agent, actor); err != nil {
				return fmt.Errorf("failed to auto-create agent bead %s: %w", agentID, err)
			}
			if err := res.ActiveStore.AddLabel(ctx, agent.ID, "gt:agent", actor); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add gt:agent label: %v\n", err)
			}
			if roleType != "" {
				if err := res.ActiveStore.AddLabel(ctx, agent.ID, "role_type:"+roleType, actor); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to add role_type label: %v\n", err)
				}
			}
			if rig != "" {
				if err := res.ActiveStore.AddLabel(ctx, agent.ID, "rig:"+rig, actor); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to add rig label: %v\n", err)
				}
			}
		}
	} else {
		var labels []string
		agent, labels, err = res.getAgentWithLabels(ctx, agentArg)
		if err != nil {
			return err
		}
		if !isAgentBead(labels) {
			return fmt.Errorf("%s is not an agent bead (missing gt:agent label)", agentID)
		}
	}

	// Update state and last_activity
	updateLastActivity := true
	if res.useDaemon(agentArg) {
		_, err := daemonClient.Update(&rpc.UpdateArgs{
			ID:           agentID,
			AgentState:   &state,
			LastActivity: &updateLastActivity,
		})
		if err != nil {
			return fmt.Errorf("failed to update agent state: %w", err)
		}
	} else {
		updates := map[string]interface{}{
			"agent_state":   state,
			"last_activity": time.Now(),
		}
		if err := res.ActiveStore.UpdateIssue(ctx, agentID, updates, actor); err != nil {
			return fmt.Errorf("failed to update agent state: %w", err)
		}
	}

	// Trigger auto-flush
	if flushManager != nil {
		flushManager.MarkDirty(false)
	}

	if jsonOutput {
		result := map[string]interface{}{
			"agent":         agentID,
			"agent_state":   state,
			"last_activity": time.Now().Format(time.RFC3339),
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	fmt.Printf("%s %s state=%s\n", ui.RenderPass("✓"), agentID, state)
	return nil
}

func runAgentHeartbeat(cmd *cobra.Command, args []string) error {
	CheckReadonly("agent heartbeat")

	agentArg := args[0]

	ctx := rootCtx

	// Resolve agent ID with centralized routing (bd-z344)
	res, notFound, err := resolveAgentID(ctx, agentArg)
	if err != nil {
		return err
	}
	defer res.Close()
	if notFound {
		return fmt.Errorf("agent bead not found: %s", agentArg)
	}
	agentID := res.AgentID

	// Get agent bead to verify it's an agent
	_, labels, err := res.getAgentWithLabels(ctx, agentArg)
	if err != nil {
		return err
	}
	if !isAgentBead(labels) {
		return fmt.Errorf("%s is not an agent bead (missing gt:agent label)", agentID)
	}

	// Update only last_activity
	updateLastActivity := true
	if res.useDaemon(agentArg) {
		_, err := daemonClient.Update(&rpc.UpdateArgs{
			ID:           agentID,
			LastActivity: &updateLastActivity,
		})
		if err != nil {
			return fmt.Errorf("failed to update agent heartbeat: %w", err)
		}
	} else {
		updates := map[string]interface{}{
			"last_activity": time.Now(),
		}
		if err := res.ActiveStore.UpdateIssue(ctx, agentID, updates, actor); err != nil {
			return fmt.Errorf("failed to update agent heartbeat: %w", err)
		}
	}

	// Trigger auto-flush
	if flushManager != nil {
		flushManager.MarkDirty(false)
	}

	if jsonOutput {
		result := map[string]interface{}{
			"agent":         agentID,
			"last_activity": time.Now().Format(time.RFC3339),
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	fmt.Printf("%s %s heartbeat\n", ui.RenderPass("✓"), agentID)
	return nil
}

func runAgentShow(cmd *cobra.Command, args []string) error {
	agentArg := args[0]
	ctx := rootCtx

	// Resolve agent ID with centralized routing (bd-z344)
	res, notFound, err := resolveAgentID(ctx, agentArg)
	if err != nil {
		return err
	}
	defer res.Close()
	if notFound {
		return fmt.Errorf("agent bead not found: %s", agentArg)
	}
	agentID := res.AgentID

	agent, labels, err := res.getAgentWithLabels(ctx, agentArg)
	if err != nil {
		return err
	}
	if !isAgentBead(labels) {
		return fmt.Errorf("%s is not an agent bead (missing gt:agent label)", agentID)
	}

	if jsonOutput {
		result := map[string]interface{}{
			"id":            agentID,
			"title":         agent.Title,
			"agent_state":   emptyToNil(string(agent.AgentState)),
			"last_activity": formatTimeOrNil(agent.LastActivity),
			"hook_bead":     emptyToNil(agent.HookBead),
			"role_bead":     emptyToNil(agent.RoleBead),
			"role_type":     emptyToNil(agent.RoleType),
			"rig":           emptyToNil(agent.Rig),
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	// Human-readable output
	fmt.Printf("Agent: %s\n", agentID)
	fmt.Printf("Title: %s\n", agent.Title)
	fmt.Println()
	fmt.Println("State:")
	if agent.AgentState != "" {
		fmt.Printf("  agent_state: %s\n", agent.AgentState)
	} else {
		fmt.Println("  agent_state: (not set)")
	}
	if agent.LastActivity != nil {
		fmt.Printf("  last_activity: %s (%s ago)\n",
			agent.LastActivity.Format(time.RFC3339),
			time.Since(*agent.LastActivity).Round(time.Second))
	} else {
		fmt.Println("  last_activity: (not set)")
	}
	fmt.Println()
	fmt.Println("Identity:")
	if agent.RoleType != "" {
		fmt.Printf("  role_type: %s\n", agent.RoleType)
	} else {
		fmt.Println("  role_type: (not set)")
	}
	if agent.Rig != "" {
		fmt.Printf("  rig: %s\n", agent.Rig)
	} else {
		fmt.Println("  rig: (not set)")
	}
	fmt.Println()
	fmt.Println("Slots:")
	if agent.HookBead != "" {
		fmt.Printf("  hook: %s\n", agent.HookBead)
	} else {
		fmt.Println("  hook: (empty)")
	}
	if agent.RoleBead != "" {
		fmt.Printf("  role: %s\n", agent.RoleBead)
	} else {
		fmt.Println("  role: (empty)")
	}

	return nil
}

func runAgentSubscriptions(cmd *cobra.Command, args []string) error {
	agentArg := args[0]

	ctx := rootCtx

	// Resolve agent ID with centralized routing (bd-z344)
	res, notFound, err := resolveAgentID(ctx, agentArg)
	if err != nil {
		return err
	}
	defer res.Close()

	if notFound {
		return fmt.Errorf("agent bead not found: %s", agentArg)
	}

	agentID := res.AgentID

	// Get agent bead with labels
	agent, labels, err := res.getAgentWithLabels(ctx, agentArg)
	if err != nil {
		return err
	}

	// Verify agent bead is actually an agent (check for gt:agent label)
	if !isAgentBead(labels) {
		return fmt.Errorf("%s is not an agent bead (missing gt:agent label)", agentID)
	}

	// Compute auto-subscriptions based on agent identity
	autoSubs := []string{"global"}
	autoSubs = append(autoSubs, "agent:"+agentID)
	if agent.Rig != "" {
		autoSubs = append(autoSubs, "rig:"+agent.Rig)
	}
	if agent.RoleType != "" {
		autoSubs = append(autoSubs, "role:"+agent.RoleType)
	}

	// Get custom subscriptions from native fields
	customSubs := agent.AdviceSubscriptions
	excludedSubs := agent.AdviceSubscriptionsExclude

	// Compute final merged list: (auto + custom) - excluded
	excludeSet := make(map[string]bool)
	for _, exc := range excludedSubs {
		excludeSet[exc] = true
	}

	finalSubs := []string{}
	seen := make(map[string]bool)

	// Add auto-subscriptions (if not excluded)
	for _, sub := range autoSubs {
		if !excludeSet[sub] && !seen[sub] {
			finalSubs = append(finalSubs, sub)
			seen[sub] = true
		}
	}

	// Add custom subscriptions (if not excluded)
	for _, sub := range customSubs {
		if !excludeSet[sub] && !seen[sub] {
			finalSubs = append(finalSubs, sub)
			seen[sub] = true
		}
	}

	if jsonOutput {
		result := map[string]interface{}{
			"agent":    agentID,
			"auto":     autoSubs,
			"custom":   customSubs,
			"excluded": excludedSubs,
			"final":    finalSubs,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	// Human-readable output
	fmt.Printf("Agent: %s\n\n", agentID)

	fmt.Printf("Auto-subscriptions: %s\n", formatSubscriptionList(autoSubs))
	fmt.Printf("Custom: %s\n", formatSubscriptionList(customSubs))
	fmt.Printf("Excluded: %s\n", formatSubscriptionList(excludedSubs))
	fmt.Printf("Final: %s\n", formatSubscriptionList(finalSubs))

	return nil
}

// formatSubscriptionList formats a subscription list for display
func formatSubscriptionList(subs []string) string {
	if len(subs) == 0 {
		return "(none)"
	}
	return strings.Join(subs, ", ")
}

// formatTimeOrNil returns the time formatted as RFC3339 or nil if nil
func formatTimeOrNil(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}

func runAgentPodRegister(cmd *cobra.Command, args []string) error {
	CheckReadonly("agent pod-register")

	if daemonClient == nil {
		return fmt.Errorf("agent pod-register requires the daemon (set BD_DAEMON_HOST)")
	}

	agentArg := args[0]

	// Resolve agent ID
	resp, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: agentArg})
	if err != nil {
		return fmt.Errorf("failed to resolve agent %s: %w", agentArg, err)
	}
	var agentID string
	if err := json.Unmarshal(resp.Data, &agentID); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	result, err := daemonClient.AgentPodRegister(&rpc.AgentPodRegisterArgs{
		AgentID:       agentID,
		PodName:       podRegisterName,
		PodIP:         podRegisterIP,
		PodNode:       podRegisterNode,
		PodStatus:     podRegisterStatus,
		ScreenSession: podRegisterScreen,
	})
	if err != nil {
		return fmt.Errorf("failed to register pod: %w", err)
	}

	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	fmt.Printf("%s %s pod=%s status=%s\n", ui.RenderPass("✓"), result.AgentID, result.PodName, result.PodStatus)
	return nil
}

func runAgentPodDeregister(cmd *cobra.Command, args []string) error {
	CheckReadonly("agent pod-deregister")

	if daemonClient == nil {
		return fmt.Errorf("agent pod-deregister requires the daemon (set BD_DAEMON_HOST)")
	}

	agentArg := args[0]

	// Resolve agent ID
	resp, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: agentArg})
	if err != nil {
		return fmt.Errorf("failed to resolve agent %s: %w", agentArg, err)
	}
	var agentID string
	if err := json.Unmarshal(resp.Data, &agentID); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	result, err := daemonClient.AgentPodDeregister(&rpc.AgentPodDeregisterArgs{
		AgentID: agentID,
	})
	if err != nil {
		return fmt.Errorf("failed to deregister pod: %w", err)
	}

	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	fmt.Printf("%s %s pod deregistered\n", ui.RenderPass("✓"), result.AgentID)
	return nil
}

func runAgentPodStatus(cmd *cobra.Command, args []string) error {
	CheckReadonly("agent pod-status")

	if daemonClient == nil {
		return fmt.Errorf("agent pod-status requires the daemon (set BD_DAEMON_HOST)")
	}

	agentArg := args[0]

	// Resolve agent ID
	resp, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: agentArg})
	if err != nil {
		return fmt.Errorf("failed to resolve agent %s: %w", agentArg, err)
	}
	var agentID string
	if err := json.Unmarshal(resp.Data, &agentID); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	result, err := daemonClient.AgentPodStatus(&rpc.AgentPodStatusArgs{
		AgentID:   agentID,
		PodStatus: podStatusValue,
	})
	if err != nil {
		return fmt.Errorf("failed to update pod status: %w", err)
	}

	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	fmt.Printf("%s %s pod_status=%s\n", ui.RenderPass("✓"), result.AgentID, result.PodStatus)
	return nil
}

func runAgentPodList(cmd *cobra.Command, args []string) error {
	if daemonClient == nil {
		return fmt.Errorf("agent pod-list requires the daemon (set BD_DAEMON_HOST)")
	}

	result, err := daemonClient.AgentPodList(&rpc.AgentPodListArgs{
		Rig: podListRig,
	})
	if err != nil {
		return fmt.Errorf("failed to list agent pods: %w", err)
	}

	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	if len(result.Agents) == 0 {
		fmt.Println("No agents with active pods")
		return nil
	}

	for _, a := range result.Agents {
		line := fmt.Sprintf("%-30s pod=%-30s status=%-12s", a.AgentID, a.PodName, a.PodStatus)
		if a.PodIP != "" {
			line += fmt.Sprintf(" ip=%s", a.PodIP)
		}
		if a.PodNode != "" {
			line += fmt.Sprintf(" node=%s", a.PodNode)
		}
		if a.Rig != "" {
			line += fmt.Sprintf(" rig=%s", a.Rig)
		}
		fmt.Println(line)
	}

	return nil
}

// runAgentBackfillLabels scans all agent beads and adds role_type/rig labels
func runAgentBackfillLabels(cmd *cobra.Command, args []string) error {
	if !backfillDryRun {
		CheckReadonly("agent backfill-labels")
	}

	// List all agent beads (by gt:agent label) via daemon RPC
	var agents []*types.Issue
	resp, err := daemonClient.List(&rpc.ListArgs{
		Labels: []string{"gt:agent"},
	})
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}
	if err := json.Unmarshal(resp.Data, &agents); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	if len(agents) == 0 {
		fmt.Println("No agent beads found")
		return nil
	}

	updated := 0
	skipped := 0

	for _, agent := range agents {
		// Skip tombstoned agents
		if agent.Status == types.StatusTombstone {
			continue
		}

		// Extract role_type and rig from description if not set in fields
		roleType := agent.RoleType
		rig := agent.Rig

		if roleType == "" || rig == "" {
			// Parse from description
			lines := strings.Split(agent.Description, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "role_type:") && roleType == "" {
					roleType = strings.TrimSpace(strings.TrimPrefix(line, "role_type:"))
				}
				if strings.HasPrefix(line, "rig:") && rig == "" {
					rig = strings.TrimSpace(strings.TrimPrefix(line, "rig:"))
				}
			}
		}

		// Skip if no role_type or rig found
		if roleType == "" && rig == "" {
			skipped++
			continue
		}

		// Check if labels already exist
		var existingLabels []string
		showResp, err := daemonClient.Show(&rpc.ShowArgs{ID: agent.ID})
		if err == nil {
			var fullAgent types.Issue
			if err := json.Unmarshal(showResp.Data, &fullAgent); err == nil {
				existingLabels = fullAgent.Labels
			}
		}

		// Determine which labels need to be added
		needsRoleTypeLabel := roleType != "" && !containsLabel(existingLabels, "role_type:"+roleType)
		needsRigLabel := rig != "" && !containsLabel(existingLabels, "rig:"+rig)
		needsFieldUpdate := (roleType != "" && agent.RoleType == "") || (rig != "" && agent.Rig == "")

		if !needsRoleTypeLabel && !needsRigLabel && !needsFieldUpdate {
			skipped++
			continue
		}

		if backfillDryRun {
			fmt.Printf("Would update %s:\n", agent.ID)
			if needsFieldUpdate {
				if roleType != "" && agent.RoleType == "" {
					fmt.Printf("  Set role_type: %s\n", roleType)
				}
				if rig != "" && agent.Rig == "" {
					fmt.Printf("  Set rig: %s\n", rig)
				}
			}
			if needsRoleTypeLabel {
				fmt.Printf("  Add label: role_type:%s\n", roleType)
			}
			if needsRigLabel {
				fmt.Printf("  Add label: rig:%s\n", rig)
			}
			updated++
			continue
		}

		// Update fields if needed
		if needsFieldUpdate {
			updateArgs := &rpc.UpdateArgs{ID: agent.ID}
			if roleType != "" && agent.RoleType == "" {
				rt := roleType
				updateArgs.RoleType = &rt
			}
			if rig != "" && agent.Rig == "" {
				r := rig
				updateArgs.Rig = &r
			}
			if _, err := daemonClient.Update(updateArgs); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update fields for %s: %v\n", agent.ID, err)
			}
		}

		// Add labels - use BatchAddLabels for atomicity when using daemon
		var labelsToAdd []string
		if needsRoleTypeLabel {
			labelsToAdd = append(labelsToAdd, "role_type:"+roleType)
		}
		if needsRigLabel {
			labelsToAdd = append(labelsToAdd, "rig:"+rig)
		}

		if len(labelsToAdd) > 0 {
			// Use BatchAddLabels for atomic multi-label add
			if _, err := daemonClient.BatchAddLabels(&rpc.BatchAddLabelsArgs{
				IssueID: agent.ID,
				Labels:  labelsToAdd,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to add labels to %s: %v\n", agent.ID, err)
			}
		}

		fmt.Printf("%s Updated %s (role_type:%s, rig:%s)\n", ui.RenderPass("✓"), agent.ID, roleType, rig)
		updated++
	}

	// Trigger auto-flush
	if flushManager != nil && !backfillDryRun {
		flushManager.MarkDirty(false)
	}

	if backfillDryRun {
		fmt.Printf("\nDry run complete: %d would be updated, %d skipped\n", updated, skipped)
	} else {
		fmt.Printf("\nBackfill complete: %d updated, %d skipped\n", updated, skipped)
	}

	return nil
}

// agentResolution holds the result of resolving a single agent ID.
// It encapsulates the routing decision and provides a unified interface
// for getting the resolved ID, issue, labels, and the correct store.
type agentResolution struct {
	AgentID        string
	RoutedResult   *RoutedResult // non-nil if resolved via routing
	SkipRouting    bool          // true if remote daemon handles all IDs
	ActiveStore    storage.Storage
	routingChecked bool // true if routing was checked (for daemon path decisions)
}

// resolveAgentID resolves a single agent arg, handling routing vs daemon vs direct.
// Returns an agentResolution with the resolved ID and routing info.
// On "not found" errors, sets NotFound=true and returns the input arg as AgentID.
// Caller must call Close() on the result when done.
func resolveAgentID(ctx context.Context, agentArg string) (*agentResolution, bool, error) {
	res := &agentResolution{
		SkipRouting: isRemoteDaemon(),
		ActiveStore: store,
	}
	notFound := false

	if !res.SkipRouting && needsRouting(agentArg) {
		var err error
		res.RoutedResult, err = resolveAndGetIssueWithRouting(ctx, store, agentArg)
		if err != nil {
			if res.RoutedResult != nil {
				res.RoutedResult.Close()
				res.RoutedResult = nil
			}
			if strings.Contains(err.Error(), "no issue found matching") {
				notFound = true
				res.AgentID = agentArg
			} else {
				return nil, false, fmt.Errorf("failed to resolve agent %s: %w", agentArg, err)
			}
		} else if res.RoutedResult != nil && res.RoutedResult.Issue != nil {
			res.AgentID = res.RoutedResult.ResolvedID
			if res.RoutedResult.Routed {
				res.ActiveStore = res.RoutedResult.Store
			}
		} else {
			if res.RoutedResult != nil {
				res.RoutedResult.Close()
				res.RoutedResult = nil
			}
			notFound = true
			res.AgentID = agentArg
		}
	} else {
		resp, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: agentArg})
		if err != nil {
			if strings.Contains(err.Error(), "no issue found matching") {
				notFound = true
				res.AgentID = agentArg
			} else {
				return nil, false, fmt.Errorf("failed to resolve agent %s: %w", agentArg, err)
			}
		} else {
			if err := json.Unmarshal(resp.Data, &res.AgentID); err != nil {
				return nil, false, fmt.Errorf("parsing response: %w", err)
			}
		}
		res.routingChecked = true
	}

	return res, notFound, nil
}

// Close releases any routed storage held by this resolution.
func (r *agentResolution) Close() {
	if r.RoutedResult != nil {
		r.RoutedResult.Close()
	}
}

// useDaemon returns true if the daemon should be used for RPC operations
// (i.e., the ID doesn't need cross-rig routing). Daemon is always connected.
func (r *agentResolution) useDaemon(agentArg string) bool {
	return r.SkipRouting || !needsRouting(agentArg)
}

// getAgentWithLabels fetches the agent issue and its labels from the appropriate source.
// If the agent was already loaded via routing, uses that. Otherwise uses daemon or direct store.
func (r *agentResolution) getAgentWithLabels(ctx context.Context, agentArg string) (*types.Issue, []string, error) {
	if r.RoutedResult != nil && r.RoutedResult.Issue != nil {
		labels, _ := r.RoutedResult.Store.GetLabels(ctx, r.AgentID)
		return r.RoutedResult.Issue, labels, nil
	}
	if r.useDaemon(agentArg) {
		resp, err := daemonClient.Show(&rpc.ShowArgs{ID: r.AgentID})
		if err != nil {
			return nil, nil, fmt.Errorf("agent bead not found: %s", r.AgentID)
		}
		var agent types.Issue
		if err := json.Unmarshal(resp.Data, &agent); err != nil {
			return nil, nil, fmt.Errorf("parsing response: %w", err)
		}
		return &agent, agent.Labels, nil
	}
	agent, err := r.ActiveStore.GetIssue(ctx, r.AgentID)
	if err != nil || agent == nil {
		return nil, nil, fmt.Errorf("agent bead not found: %s", r.AgentID)
	}
	labels, _ := r.ActiveStore.GetLabels(ctx, r.AgentID)
	return agent, labels, nil
}

// containsLabel checks if a label exists in the list
func containsLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

// isAgentBead checks if an issue is an agent bead by looking for the gt:agent label.
// This replaces the previous type-based check (issue_type='agent') for Gas Town separation.
func isAgentBead(labels []string) bool {
	for _, l := range labels {
		if l == "gt:agent" {
			return true
		}
	}
	return false
}

// parseAgentIDFields extracts role_type and rig from an agent bead ID.
// Agent ID patterns:
//   - Town-level: <prefix>-<role> (e.g., gt-mayor) → role="mayor", rig=""
//   - Per-rig singleton: <prefix>-<rig>-<role> (e.g., gt-gastown-witness) → role="witness", rig="gastown"
//   - Per-rig named: <prefix>-<rig>-<role>-<name> (e.g., gt-gastown-polecat-nux) → role="polecat", rig="gastown"
func parseAgentIDFields(agentID string) (roleType, rig string) {
	// Must contain a hyphen to have a prefix
	hyphenIdx := strings.Index(agentID, "-")
	if hyphenIdx <= 0 {
		return "", ""
	}

	// Split into parts after the prefix
	rest := agentID[hyphenIdx+1:] // Skip "<prefix>-"
	parts := strings.Split(rest, "-")

	if len(parts) < 1 {
		return "", ""
	}

	// Known roles for classification
	townLevelRoles := map[string]bool{"mayor": true, "deacon": true}
	rigLevelRoles := map[string]bool{"witness": true, "refinery": true}
	namedRoles := map[string]bool{"crew": true, "polecat": true}

	// Case 1: Town-level roles (gt-mayor, gt-deacon) - single part after prefix
	if len(parts) == 1 {
		role := parts[0]
		if townLevelRoles[role] {
			return role, ""
		}
		return "", "" // Unknown format
	}

	// For 2+ parts, scan from the right to find a known role.
	// This allows rig names to contain hyphens (e.g., "my-project").
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]

		// Check for rig-level role (witness, refinery) - must be at end
		if rigLevelRoles[part] && i == len(parts)-1 {
			// rig is everything before role
			rig = strings.Join(parts[:i], "-")
			return part, rig
		}

		// Check for named role (crew, polecat) - must have something after (the name)
		if namedRoles[part] && i < len(parts)-1 {
			// rig is everything before role
			rig = strings.Join(parts[:i], "-")
			return part, rig
		}
	}

	return "", "" // Unknown format
}

// AgentFields holds agent-specific configuration that can be stored in a bead's description.
// These fields extend the core agent identity stored in Issue fields (RoleType, Rig, etc.).
type AgentFields struct {
	// RoleType is the agent role: polecat, crew, witness, refinery, mayor, deacon
	RoleType string

	// Rig is the rig name (empty for town-level agents)
	Rig string

	// AdviceSubscriptions are additional labels the agent subscribes to for advice delivery.
	// These are in addition to the auto-subscribed context labels (global, rig:X, role:Y, agent:Z).
	// Example: ["security", "testing", "performance"]
	AdviceSubscriptions []string

	// AdviceSubscriptionsExclude are labels the agent opts out of receiving advice for.
	// Use to suppress advice that would otherwise match via auto-subscriptions.
	// Example: ["deprecated", "wip"]
	AdviceSubscriptionsExclude []string
}

// FormatAgentDescription formats AgentFields into a description string.
// The format uses key: value lines for simple fields and key: value1,value2 for lists.
// This allows agent configuration to be stored in a bead's description field.
func FormatAgentDescription(fields AgentFields) string {
	var lines []string

	if fields.RoleType != "" {
		lines = append(lines, "role_type: "+fields.RoleType)
	}

	if fields.Rig != "" {
		lines = append(lines, "rig: "+fields.Rig)
	}

	if len(fields.AdviceSubscriptions) > 0 {
		lines = append(lines, "advice_subscriptions: "+strings.Join(fields.AdviceSubscriptions, ","))
	}

	if len(fields.AdviceSubscriptionsExclude) > 0 {
		lines = append(lines, "advice_subscriptions_exclude: "+strings.Join(fields.AdviceSubscriptionsExclude, ","))
	}

	return strings.Join(lines, "\n")
}

// ParseAgentFields parses a description string into AgentFields.
// The description is expected to have key: value lines.
// Unrecognized lines are ignored to allow for additional content in descriptions.
func ParseAgentFields(description string) AgentFields {
	var fields AgentFields

	lines := strings.Split(description, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Split on first colon
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])

		switch key {
		case "role_type":
			fields.RoleType = value
		case "rig":
			fields.Rig = value
		case "advice_subscriptions":
			if value != "" {
				fields.AdviceSubscriptions = parseCommaSeparatedList(value)
			}
		case "advice_subscriptions_exclude":
			if value != "" {
				fields.AdviceSubscriptionsExclude = parseCommaSeparatedList(value)
			}
		}
	}

	return fields
}

// parseCommaSeparatedList splits a comma-separated string into a slice,
// trimming whitespace from each element.
func parseCommaSeparatedList(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
