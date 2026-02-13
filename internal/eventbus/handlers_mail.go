package eventbus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// MailNudgeHandler nudges the recipient agent via their Coop HTTP API when
// a MailSent event is dispatched. This provides instant mail delivery instead
// of waiting for the agent's next polling cycle. (bd-cdp8)
//
// Resolution: parses the "to" field from MailEventPayload, converts the
// address to an agent bead ID, looks up the bead's notes for coop_url,
// and POSTs to <coop_url>/api/v1/agent/nudge.
//
// Priority 50 (runs after standard handlers; nudging is supplementary).
type MailNudgeHandler struct {
	httpClient *http.Client
}

func (h *MailNudgeHandler) ID() string          { return "mail-nudge" }
func (h *MailNudgeHandler) Handles() []EventType { return []EventType{EventMailSent} }
func (h *MailNudgeHandler) Priority() int        { return 50 }

func (h *MailNudgeHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	var payload MailEventPayload
	if err := unmarshalEventPayload(event, &payload); err != nil {
		return fmt.Errorf("mail-nudge: %w", err)
	}

	to := payload.To
	if to == "" {
		return nil
	}

	// Convert mail address to agent bead ID.
	agentID := mailAddressToAgentID(to)
	if agentID == "" {
		log.Printf("mail-nudge: cannot resolve agent ID for address %q, skipping", to)
		return nil
	}

	// Look up agent bead notes for coop_url.
	coopURL, err := resolveCoopURLFromBead(ctx, event.CWD, agentID)
	if err != nil {
		log.Printf("mail-nudge: no coop_url for agent %q: %v", agentID, err)
		return nil // Not a coop agent or not reachable; skip silently.
	}

	// POST nudge to coop sidecar.
	message := fmt.Sprintf("You have new mail from %s: %s", payload.From, payload.Subject)
	delivered, reason, err := postNudge(ctx, h.httpClient, coopURL, message)
	if err != nil {
		log.Printf("mail-nudge: nudge to %s (%s) failed: %v", agentID, coopURL, err)
		return nil // Best-effort; don't fail the event chain.
	}

	if delivered {
		log.Printf("mail-nudge: nudged %s successfully", agentID)
	} else {
		log.Printf("mail-nudge: nudge to %s not delivered (reason: %s)", agentID, reason)
	}

	return nil
}

// mailAddressToAgentID converts a mail address to a beads agent bead ID.
//
// Address normalization (matching gastown's AddressToIdentity):
//   - "mayor" or "mayor/" -> "gt-mayor" (town-level)
//   - "deacon" or "deacon/" -> "gt-deacon" (town-level)
//   - "gastown/polecats/Toast" -> "gt-gastown-polecat-Toast" (strip middle)
//   - "gastown/crew/max" -> "gt-gastown-crew-max"
//   - "gastown/Toast" -> "gt-gastown-polecat-Toast" (canonical)
//   - "gastown/witness" -> "gt-gastown-witness"
//   - "gastown/refinery" -> "gt-gastown-refinery"
func mailAddressToAgentID(address string) string {
	// Trim trailing slash
	address = strings.TrimSuffix(address, "/")

	// Town-level agents
	switch address {
	case "mayor":
		return "gt-mayor"
	case "deacon":
		return "gt-deacon"
	}

	parts := strings.Split(address, "/")

	switch len(parts) {
	case 2:
		rig := parts[0]
		role := parts[1]
		switch role {
		case "witness":
			return fmt.Sprintf("gt-%s-witness", rig)
		case "refinery":
			return fmt.Sprintf("gt-%s-refinery", rig)
		default:
			if strings.HasPrefix(role, "crew/") {
				crewName := strings.TrimPrefix(role, "crew/")
				return fmt.Sprintf("gt-%s-crew-%s", rig, crewName)
			}
			// Default: assume polecat
			return fmt.Sprintf("gt-%s-polecat-%s", rig, role)
		}
	case 3:
		rig := parts[0]
		middle := parts[1]
		name := parts[2]
		switch middle {
		case "polecats":
			return fmt.Sprintf("gt-%s-polecat-%s", rig, name)
		case "crew":
			return fmt.Sprintf("gt-%s-crew-%s", rig, name)
		default:
			return ""
		}
	}

	return ""
}

// resolveCoopURLFromBead looks up an agent bead via `bd show --json` and
// extracts coop_url from the notes field. Returns empty string if the agent
// doesn't have a coop_url (e.g., local tmux agent).
func resolveCoopURLFromBead(ctx context.Context, cwd, agentID string) (string, error) {
	stdout, _, err := runBDCommand(ctx, cwd, "show", agentID, "--json")
	if err != nil {
		return "", fmt.Errorf("bd show %s: %w", agentID, err)
	}

	// bd show --json returns an array of issues
	var issues []struct {
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(stdout), &issues); err != nil {
		return "", fmt.Errorf("parse bd show output: %w", err)
	}
	if len(issues) == 0 {
		return "", fmt.Errorf("agent bead %q not found", agentID)
	}

	notes := issues[0].Notes
	if !strings.Contains(notes, "coop_url") {
		return "", fmt.Errorf("no coop_url in notes for %q", agentID)
	}

	for _, line := range strings.Split(notes, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "coop_url" {
			return strings.TrimSpace(parts[1]), nil
		}
	}

	return "", fmt.Errorf("coop_url not found in notes for %q", agentID)
}

// postNudge POSTs a nudge message to a Coop sidecar's nudge endpoint.
// Returns (delivered, reason, error). If client is nil, a default is used.
func postNudge(ctx context.Context, client *http.Client, coopURL, message string) (bool, string, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	body, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		return false, "", err
	}

	url := strings.TrimRight(coopURL, "/") + "/api/v1/agent/nudge"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return false, "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var nudgeResp struct {
		Delivered   bool   `json:"delivered"`
		StateBefore string `json:"state_before,omitempty"`
		Reason      string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(respBody, &nudgeResp); err != nil {
		return false, "", fmt.Errorf("parse nudge response: %w", err)
	}

	return nudgeResp.Delivered, nudgeResp.Reason, nil
}

// DecisionNudgeHandler nudges the requesting agent via their Coop HTTP API when
// a decision is resolved. This wakes idle agents so their PostToolUse hook can
// run `bd decision check --inject` and deliver the response. (bd-5mkhu)
//
// Priority 50 (runs after standard handlers; nudging is supplementary).
type DecisionNudgeHandler struct {
	httpClient *http.Client
}

func (h *DecisionNudgeHandler) ID() string          { return "decision-nudge" }
func (h *DecisionNudgeHandler) Handles() []EventType { return []EventType{EventDecisionResponded} }
func (h *DecisionNudgeHandler) Priority() int        { return 50 }

func (h *DecisionNudgeHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	var payload DecisionEventPayload
	if err := unmarshalEventPayload(event, &payload); err != nil {
		return fmt.Errorf("decision-nudge: %w", err)
	}

	agentID := payload.RequestedBy
	if agentID == "" {
		return nil // No agent to nudge
	}

	// Look up agent bead notes for coop_url.
	coopURL, err := resolveCoopURLFromBead(ctx, event.CWD, agentID)
	if err != nil {
		log.Printf("decision-nudge: no coop_url for agent %q: %v", agentID, err)
		return nil // Not a coop agent or not reachable; skip silently.
	}

	// POST nudge to coop sidecar.
	message := fmt.Sprintf("Decision %s resolved by %s (chose: %s)", payload.DecisionID, payload.ResolvedBy, payload.ChosenLabel)
	delivered, reason, err := postNudge(ctx, h.httpClient, coopURL, message)
	if err != nil {
		log.Printf("decision-nudge: nudge to %s (%s) failed: %v", agentID, coopURL, err)
		return nil // Best-effort; don't fail the event chain.
	}

	if delivered {
		log.Printf("decision-nudge: nudged %s for decision %s", agentID, payload.DecisionID)
	} else {
		log.Printf("decision-nudge: nudge to %s not delivered (reason: %s)", agentID, reason)
	}

	return nil
}

// DefaultMailHandlers returns the mail and decision nudge event bus handlers
// for daemon registration.
func DefaultMailHandlers() []Handler {
	return []Handler{
		&MailNudgeHandler{},
		&DecisionNudgeHandler{},
	}
}
