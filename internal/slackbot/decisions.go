// Package slackbot provides a Slack bot integration for beads issue tracking.
package slackbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// looksLikeSessionID returns true if the string appears to be a session UUID
// or hex identifier rather than a human-readable agent name.
// Matches patterns like "cea46632-a2c0-4ddf-95f3-c9bd79c0ccc3" or long hex strings.
var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]{8,}(-[0-9a-f]{4,}){0,4}$`)

func looksLikeSessionID(s string) bool {
	return len(s) > 16 && sessionIDPattern.MatchString(s)
}

// Decision is a unified decision struct used internally by the Slack bot.
// It bridges the beads DecisionPoint/Issue pair into a single view.
type Decision struct {
	ID              string
	Question        string
	Context         string
	Options         []DecisionOption
	ChosenIndex     int // 1-indexed; 0 means unresolved
	Rationale       string
	RequestedBy     string
	RequestedAt     time.Time
	ResolvedBy      string
	Urgency         string // "high", "medium", "low"
	Resolved        bool
	PredecessorID   string
	ParentBeadID    string
	ParentBeadTitle string
	Blockers        []string
	SemanticSlug    string
}

// DecisionOption represents a single choice within a decision.
type DecisionOption struct {
	ID          string
	Label       string
	Description string
	Recommended bool // Not natively stored in beads; kept for interface compat.
}

// DecisionClient adapts beads' rpc.Client to provide a higher-level
// decision-oriented API for the Slack bot. It reconnects automatically
// when the underlying TCP connection is broken.
type DecisionClient struct {
	client    *rpc.Client
	addr      string
	token     string
	mu        sync.Mutex
}

// NewDecisionClient wraps an existing RPC client for decision operations.
// It stores the connection parameters so it can reconnect if the connection breaks.
func NewDecisionClient(c *rpc.Client, addr, token string) *DecisionClient {
	return &DecisionClient{client: c, addr: addr, token: token}
}

// reconnect attempts to establish a new RPC connection, replacing the broken one.
// Uses HTTP when the address looks like a URL, TCP otherwise (legacy).
func (dc *DecisionClient) reconnect() error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.client != nil {
		dc.client.Close()
	}

	// Auto-prepend http:// if bare host:port
	addr := dc.addr
	if !rpc.IsHTTPURL(addr) {
		addr = "http://" + addr
	}
	httpClient, err := rpc.TryConnectHTTP(addr, dc.token)
	var newClient *rpc.Client
	if err == nil {
		newClient = rpc.WrapHTTPClient(httpClient)
	}

	if err != nil {
		return fmt.Errorf("reconnect to %s: %w", dc.addr, err)
	}
	dc.client = newClient
	log.Printf("slackbot: reconnected to daemon at %s", dc.addr)
	return nil
}

// getClient returns the current RPC client under lock.
func (dc *DecisionClient) getClient() *rpc.Client {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.client
}

// isBrokenPipe returns true if the error indicates a broken TCP connection.
func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "use of closed network connection")
}

// ListPending returns all unresolved decisions.
func (dc *DecisionClient) ListPending(ctx context.Context) ([]Decision, error) {
	resp, err := dc.getClient().DecisionList(&rpc.DecisionListArgs{All: false})
	if err != nil && isBrokenPipe(err) {
		if rerr := dc.reconnect(); rerr != nil {
			return nil, fmt.Errorf("decision list (reconnect failed): %w", rerr)
		}
		resp, err = dc.getClient().DecisionList(&rpc.DecisionListArgs{All: false})
	}
	if err != nil {
		return nil, fmt.Errorf("decision list: %w", err)
	}

	out := make([]Decision, 0, len(resp.Decisions))
	for _, dr := range resp.Decisions {
		d := convertDecisionResponse(dr)
		out = append(out, d)
	}
	return out, nil
}

// GetDecision retrieves a single decision by its issue ID.
func (dc *DecisionClient) GetDecision(ctx context.Context, issueID string) (*Decision, error) {
	log.Printf("slackbot/decisions: GetDecision(%s) calling daemon", issueID)
	resp, err := dc.getClient().DecisionGet(&rpc.DecisionGetArgs{IssueID: issueID})
	if err != nil && isBrokenPipe(err) {
		log.Printf("slackbot/decisions: GetDecision(%s) broken pipe, reconnecting", issueID)
		if rerr := dc.reconnect(); rerr != nil {
			return nil, fmt.Errorf("decision get %s (reconnect failed): %w", issueID, rerr)
		}
		resp, err = dc.getClient().DecisionGet(&rpc.DecisionGetArgs{IssueID: issueID})
	}
	if err != nil {
		log.Printf("slackbot/decisions: GetDecision(%s) error: %v", issueID, err)
		return nil, fmt.Errorf("decision get %s: %w", issueID, err)
	}
	log.Printf("slackbot/decisions: GetDecision(%s) success", issueID)

	d := convertDecisionResponse(resp)
	return &d, nil
}

// Resolve picks one of the predefined options by 1-based index.
// It fetches the decision first to map the index to the option ID that beads expects.
func (dc *DecisionClient) Resolve(ctx context.Context, issueID string, chosenIndex int, rationale, resolvedBy string) (*Decision, error) {
	log.Printf("slackbot/decisions: Resolve(%s, index=%d, by=%s) calling daemon", issueID, chosenIndex, resolvedBy)
	// Fetch the decision to get the option IDs.
	current, err := dc.getClient().DecisionGet(&rpc.DecisionGetArgs{IssueID: issueID})
	if err != nil && isBrokenPipe(err) {
		if rerr := dc.reconnect(); rerr != nil {
			return nil, fmt.Errorf("decision get for resolve %s (reconnect failed): %w", issueID, rerr)
		}
		current, err = dc.getClient().DecisionGet(&rpc.DecisionGetArgs{IssueID: issueID})
	}
	if err != nil {
		return nil, fmt.Errorf("decision get for resolve %s: %w", issueID, err)
	}

	var opts []types.DecisionOption
	if current.Decision != nil && current.Decision.Options != "" {
		if err := json.Unmarshal([]byte(current.Decision.Options), &opts); err != nil {
			return nil, fmt.Errorf("parse decision options for %s: %w", issueID, err)
		}
	}

	if chosenIndex < 1 || chosenIndex > len(opts) {
		return nil, fmt.Errorf("chosen index %d out of range [1..%d]", chosenIndex, len(opts))
	}
	optionID := opts[chosenIndex-1].ID

	resp, err := dc.getClient().DecisionResolve(&rpc.DecisionResolveArgs{
		IssueID:        issueID,
		SelectedOption: optionID,
		RespondedBy:    resolvedBy,
		ResponseText:   rationale,
	})
	if err != nil && isBrokenPipe(err) {
		if rerr := dc.reconnect(); rerr != nil {
			return nil, fmt.Errorf("decision resolve %s (reconnect failed): %w", issueID, rerr)
		}
		resp, err = dc.getClient().DecisionResolve(&rpc.DecisionResolveArgs{
			IssueID:        issueID,
			SelectedOption: optionID,
			RespondedBy:    resolvedBy,
			ResponseText:   rationale,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("decision resolve %s: %w", issueID, err)
	}

	d := convertDecisionResponse(resp)
	return &d, nil
}

// ResolveWithText resolves a decision with free-form text instead of a predefined option.
func (dc *DecisionClient) ResolveWithText(ctx context.Context, issueID, text, resolvedBy string) (*Decision, error) {
	resp, err := dc.getClient().DecisionResolve(&rpc.DecisionResolveArgs{
		IssueID:      issueID,
		ResponseText: text,
		RespondedBy:  resolvedBy,
	})
	if err != nil && isBrokenPipe(err) {
		if rerr := dc.reconnect(); rerr != nil {
			return nil, fmt.Errorf("decision resolve with text %s (reconnect failed): %w", issueID, rerr)
		}
		resp, err = dc.getClient().DecisionResolve(&rpc.DecisionResolveArgs{
			IssueID:      issueID,
			ResponseText: text,
			RespondedBy:  resolvedBy,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("decision resolve with text %s: %w", issueID, err)
	}

	d := convertDecisionResponse(resp)
	return &d, nil
}

// Cancel cancels a pending decision.
func (dc *DecisionClient) Cancel(ctx context.Context, issueID string) error {
	_, err := dc.getClient().DecisionCancel(&rpc.DecisionCancelArgs{IssueID: issueID})
	if err != nil && isBrokenPipe(err) {
		if rerr := dc.reconnect(); rerr != nil {
			return fmt.Errorf("decision cancel %s (reconnect failed): %w", issueID, rerr)
		}
		_, err = dc.getClient().DecisionCancel(&rpc.DecisionCancelArgs{IssueID: issueID})
	}
	if err != nil {
		return fmt.Errorf("decision cancel %s: %w", issueID, err)
	}
	return nil
}

// convertDecisionResponse maps an rpc.DecisionResponse to the unified Decision type.
func convertDecisionResponse(resp *rpc.DecisionResponse) Decision {
	d := Decision{}
	if resp == nil {
		return d
	}

	if resp.Issue != nil {
		d.ID = resp.Issue.ID
		d.SemanticSlug = resp.Issue.SemanticSlug
		d.RequestedAt = resp.Issue.CreatedAt
	}

	dp := resp.Decision
	if dp == nil {
		return d
	}

	d.Question = dp.Prompt
	d.Context = dp.Context
	// Sanitize: clear session IDs that leaked into RequestedBy from older versions
	if looksLikeSessionID(dp.RequestedBy) {
		d.RequestedBy = ""
	} else {
		d.RequestedBy = dp.RequestedBy
	}
	d.Urgency = dp.Urgency
	d.PredecessorID = dp.PriorID
	d.ParentBeadID = dp.ParentBeadID
	d.Resolved = dp.RespondedAt != nil
	d.ResolvedBy = dp.RespondedBy
	d.Rationale = dp.Guidance

	// Parse options JSON into our DecisionOption slice.
	var beadsOpts []types.DecisionOption
	if dp.Options != "" {
		_ = json.Unmarshal([]byte(dp.Options), &beadsOpts)
	}

	d.Options = make([]DecisionOption, len(beadsOpts))
	for i, o := range beadsOpts {
		d.Options[i] = DecisionOption{
			ID:          o.ID,
			Label:       o.Label,
			Description: o.Description,
		}
	}

	// Map SelectedOption back to a 1-indexed ChosenIndex.
	if dp.SelectedOption != "" {
		for i, o := range beadsOpts {
			if o.ID == dp.SelectedOption {
				d.ChosenIndex = i + 1
				break
			}
		}
	}

	// Populate parent bead title from the issue if available and the decision
	// references a parent.
	if dp.ParentBeadID != "" && resp.Issue != nil {
		d.ParentBeadTitle = resp.Issue.Title
	}

	return d
}
