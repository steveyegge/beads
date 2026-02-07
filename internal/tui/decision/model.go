// Package decision provides a Bubbletea TUI for monitoring and responding to
// decision points. It polls for pending decisions using `bd decision list --json`,
// displays them with urgency sorting, and lets users select options, add rationale,
// peek at agent terminals, and resolve or dismiss decisions interactively.
package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/steveyegge/beads/internal/types"
)

const pollInterval = 5 * time.Second

// InputMode represents the current input mode.
type InputMode int

const (
	ModeNormal    InputMode = iota
	ModeRationale           // Entering rationale for selected option
	ModeText                // Custom text response (stub)
)

// DecisionItem is a display-friendly wrapper around a decision point and its issue.
type DecisionItem struct {
	ID            string
	Prompt        string
	Options       []types.DecisionOption
	Urgency       string
	RequestedBy   string
	RequestedAt   time.Time
	Context       string
	PredecessorID string
}

// Model is the Bubbletea model for the decision watch TUI.
type Model struct {
	// Dimensions
	width, height int

	// Data
	decisions      []DecisionItem
	selected       int
	selectedOption int // 0 = none, 1-4 = option number

	// Input
	inputMode InputMode
	textInput textarea.Model
	rationale string

	// UI state
	keys           KeyMap
	help           help.Model
	showHelp       bool
	detailViewport viewport.Model
	filter         string // "high", "all", etc.
	notify         bool   // desktop notifications
	err            error
	status         string

	// Peek state - for viewing agent terminal
	peeking         bool
	peekContent     string
	peekSessionName string
	peekViewport    viewport.Model

	// Polling
	done chan struct{}
}

// New creates a new decision TUI model.
func New() *Model {
	ta := textarea.New()
	ta.Placeholder = "Enter rationale..."
	ta.SetHeight(3)
	ta.SetWidth(60)

	h := help.New()
	h.ShowAll = false

	return &Model{
		keys:           DefaultKeyMap(),
		help:           h,
		textInput:      ta,
		detailViewport: viewport.New(0, 0),
		peekViewport:   viewport.New(0, 0),
		filter:         "all",
		done:           make(chan struct{}),
	}
}

// SetFilter sets the urgency filter.
func (m *Model) SetFilter(filter string) {
	m.filter = filter
}

// SetNotify enables desktop notifications.
func (m *Model) SetNotify(notify bool) {
	m.notify = notify
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchDecisions(),
		m.startPolling(),
		tea.SetWindowTitle("BD Decision Watch"),
	)
}

// fetchDecisionsMsg is sent when decisions are fetched.
type fetchDecisionsMsg struct {
	decisions []DecisionItem
	err       error
}

// tickMsg is sent on each poll interval.
type tickMsg time.Time

// resolvedMsg is sent when a decision is resolved.
type resolvedMsg struct {
	id  string
	err error
}

// dismissedMsg is sent when a decision is dismissed/canceled.
type dismissedMsg struct {
	id  string
	err error
}

// peekMsg is sent when terminal content is captured.
type peekMsg struct {
	sessionName string
	content     string
	err         error
}

// enrichedDecision mirrors the JSON output from `bd decision list --json`.
type enrichedDecision struct {
	IssueID       string                 `json:"issue_id"`
	Prompt        string                 `json:"prompt"`
	Context       string                 `json:"context,omitempty"`
	Options       string                 `json:"options"`
	Urgency       string                 `json:"urgency,omitempty"`
	RequestedBy   string                 `json:"requested_by,omitempty"`
	PriorID       string                 `json:"prior_id,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	OptionsParsed []types.DecisionOption `json:"options_parsed,omitempty"`
	Issue         *enrichedIssue         `json:"issue,omitempty"`
}

type enrichedIssue struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// fetchDecisions fetches pending decisions via the bd CLI.
func (m *Model) fetchDecisions() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "bd", "decision", "list", "--json")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			// Check if it's just "no decisions"
			if strings.Contains(stderr.String(), "No pending") ||
				strings.Contains(stdout.String(), "[]") ||
				strings.Contains(stdout.String(), "null") {
				return fetchDecisionsMsg{decisions: []DecisionItem{}}
			}
			return fetchDecisionsMsg{err: fmt.Errorf("failed to fetch decisions: %w", err)}
		}

		raw := stdout.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
			return fetchDecisionsMsg{decisions: []DecisionItem{}}
		}

		var enriched []enrichedDecision
		if err := json.Unmarshal(raw, &enriched); err != nil {
			return fetchDecisionsMsg{decisions: []DecisionItem{}}
		}

		var decisions []DecisionItem
		for _, ed := range enriched {
			item := DecisionItem{
				ID:            ed.IssueID,
				Prompt:        ed.Prompt,
				Urgency:       ed.Urgency,
				RequestedBy:   ed.RequestedBy,
				RequestedAt:   ed.CreatedAt,
				Context:       ed.Context,
				PredecessorID: ed.PriorID,
			}

			// Use parsed options if available, otherwise parse from JSON string
			if len(ed.OptionsParsed) > 0 {
				item.Options = ed.OptionsParsed
			} else if ed.Options != "" {
				var opts []types.DecisionOption
				if err := json.Unmarshal([]byte(ed.Options), &opts); err == nil {
					item.Options = opts
				}
			}

			decisions = append(decisions, item)
		}

		return fetchDecisionsMsg{decisions: decisions}
	}
}

// startPolling starts the poll ticker.
func (m *Model) startPolling() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// resolveDecision resolves a decision with the given option.
func (m *Model) resolveDecision(decisionID string, optionID string, rationale string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		args := []string{"decision", "respond", decisionID, "--select", optionID}
		if rationale != "" {
			args = append(args, "--text", rationale)
		}

		cmd := exec.CommandContext(ctx, "bd", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return resolvedMsg{id: decisionID, err: fmt.Errorf("%s", stderr.String())}
		}

		return resolvedMsg{id: decisionID}
	}
}

// dismissDecision cancels/dismisses a decision.
func (m *Model) dismissDecision(decisionID, reason string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		args := []string{"decision", "cancel", decisionID}
		if reason != "" {
			args = append(args, "--reason", reason)
		}

		cmd := exec.CommandContext(ctx, "bd", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return dismissedMsg{id: decisionID, err: fmt.Errorf("%s", stderr.String())}
		}

		return dismissedMsg{id: decisionID}
	}
}

// getSessionName converts a RequestedBy path to a tmux session name.
// e.g., "gastown/crew/decision_point" -> "bd-gastown-crew-decision_point"
func getSessionName(requestedBy string) (string, error) {
	if requestedBy == "" {
		return "", fmt.Errorf("no requestor specified")
	}

	// Handle special cases
	if requestedBy == "overseer" || requestedBy == "human" {
		return "", fmt.Errorf("cannot peek human session")
	}

	// Require at least rig/type format
	parts := strings.Split(requestedBy, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid requestor format: %s", requestedBy)
	}

	return "bd-" + strings.ReplaceAll(requestedBy, "/", "-"), nil
}

// captureTerminal captures the content of an agent's terminal.
func (m *Model) captureTerminal(sessionName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// First check if session exists
		checkCmd := exec.CommandContext(ctx, "tmux", "has-session", "-t", sessionName)
		if err := checkCmd.Run(); err != nil {
			return peekMsg{sessionName: sessionName, err: fmt.Errorf("session '%s' not found", sessionName)}
		}

		// Capture pane content with scrollback
		cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", sessionName, "-p", "-S", "-100")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return peekMsg{sessionName: sessionName, err: fmt.Errorf("capture failed: %s", stderr.String())}
		}

		return peekMsg{sessionName: sessionName, content: stdout.String()}
	}
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.detailViewport.Width = msg.Width - 4
		m.detailViewport.Height = msg.Height/2 - 4
		m.peekViewport.Width = msg.Width - 4
		m.peekViewport.Height = msg.Height - 6
		m.textInput.SetWidth(msg.Width - 10)

	case tea.KeyMsg:
		// Handle peek mode - arrow keys scroll, other keys dismiss
		if m.peeking {
			switch {
			case key.Matches(msg, m.keys.Up):
				m.peekViewport.LineUp(1)
				return m, nil
			case key.Matches(msg, m.keys.Down):
				m.peekViewport.LineDown(1)
				return m, nil
			case key.Matches(msg, m.keys.PageUp):
				m.peekViewport.HalfViewUp()
				return m, nil
			case key.Matches(msg, m.keys.PageDown):
				m.peekViewport.HalfViewDown()
				return m, nil
			default:
				// Any other key dismisses peek
				m.peeking = false
				m.peekContent = ""
				m.peekSessionName = ""
				return m, nil
			}
		}

		// Handle input mode first
		if m.inputMode != ModeNormal {
			return m.handleInputMode(msg)
		}

		switch {
		case key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Cancel):
			close(m.done)
			return m, tea.Quit

		case key.Matches(msg, m.keys.Help):
			m.showHelp = !m.showHelp

		case key.Matches(msg, m.keys.Up):
			if m.selected > 0 {
				m.selected--
				m.selectedOption = 0
			}

		case key.Matches(msg, m.keys.Down):
			if m.selected < len(m.decisions)-1 {
				m.selected++
				m.selectedOption = 0
			}

		case key.Matches(msg, m.keys.Select1):
			m.selectedOption = 1

		case key.Matches(msg, m.keys.Select2):
			m.selectedOption = 2

		case key.Matches(msg, m.keys.Select3):
			m.selectedOption = 3

		case key.Matches(msg, m.keys.Select4):
			m.selectedOption = 4

		case key.Matches(msg, m.keys.Rationale):
			if m.selectedOption > 0 {
				m.inputMode = ModeRationale
				m.textInput.Focus()
				m.textInput.SetValue("")
				m.textInput.Placeholder = "Enter rationale (optional)..."
			}

		case key.Matches(msg, m.keys.Text):
			m.inputMode = ModeText
			m.textInput.Focus()
			m.textInput.SetValue("")
			m.textInput.Placeholder = "Enter custom response..."

		case key.Matches(msg, m.keys.Peek):
			if len(m.decisions) > 0 && m.selected < len(m.decisions) {
				d := m.decisions[m.selected]
				sessionName, err := getSessionName(d.RequestedBy)
				if err != nil {
					m.status = fmt.Sprintf("Cannot peek: %v", err)
				} else {
					m.status = fmt.Sprintf("Peeking at %s...", sessionName)
					cmds = append(cmds, m.captureTerminal(sessionName))
				}
			}

		case key.Matches(msg, m.keys.Confirm):
			if m.selectedOption > 0 && len(m.decisions) > 0 && m.selected < len(m.decisions) {
				d := m.decisions[m.selected]
				if m.selectedOption <= len(d.Options) {
					optID := d.Options[m.selectedOption-1].ID
					cmds = append(cmds, m.resolveDecision(d.ID, optID, m.rationale))
					m.status = fmt.Sprintf("Resolving %s...", d.ID)
				}
			}

		case key.Matches(msg, m.keys.Refresh):
			cmds = append(cmds, m.fetchDecisions())
			m.status = "Refreshing..."

		case key.Matches(msg, m.keys.Dismiss):
			if len(m.decisions) > 0 && m.selected < len(m.decisions) {
				d := m.decisions[m.selected]
				cmds = append(cmds, m.dismissDecision(d.ID, "Dismissed via TUI"))
				m.status = fmt.Sprintf("Dismissing %s...", d.ID)
			}

		case key.Matches(msg, m.keys.FilterHigh):
			m.filter = "high"

		case key.Matches(msg, m.keys.FilterAll):
			m.filter = "all"
		}

	case fetchDecisionsMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			m.decisions = m.filterDecisions(msg.decisions)
			if m.selected >= len(m.decisions) {
				m.selected = max(0, len(m.decisions)-1)
			}
			m.status = fmt.Sprintf("Updated: %d pending", len(m.decisions))
		}

	case tickMsg:
		cmds = append(cmds, m.fetchDecisions())
		cmds = append(cmds, m.startPolling())

	case resolvedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.status = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.status = fmt.Sprintf("Resolved: %s", msg.id)
			m.selectedOption = 0
			m.rationale = ""
			cmds = append(cmds, m.fetchDecisions())
		}

	case dismissedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.status = fmt.Sprintf("Dismiss error: %v", msg.err)
		} else {
			m.status = fmt.Sprintf("Dismissed: %s", msg.id)
			m.selectedOption = 0
			cmds = append(cmds, m.fetchDecisions())
		}

	case peekMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Peek failed: %v", msg.err)
		} else {
			m.peeking = true
			m.peekSessionName = msg.sessionName
			m.peekContent = msg.content
			m.peekViewport.Width = m.width - 4
			m.peekViewport.Height = m.height - 6
			m.peekViewport.SetContent(msg.content)
			m.peekViewport.GotoBottom()
			m.status = fmt.Sprintf("Peeking: %s", msg.sessionName)
		}
	}

	// Update viewport
	var cmd tea.Cmd
	m.detailViewport, cmd = m.detailViewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// handleInputMode handles key presses in input mode.
func (m *Model) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.inputMode = ModeNormal
		m.textInput.Blur()
		return m, nil

	case tea.KeyEnter:
		if m.inputMode == ModeRationale {
			m.rationale = m.textInput.Value()
			m.inputMode = ModeNormal
			m.textInput.Blur()

			// Auto-confirm if we have an option selected
			if m.selectedOption > 0 && len(m.decisions) > 0 && m.selected < len(m.decisions) {
				d := m.decisions[m.selected]
				if m.selectedOption <= len(d.Options) {
					optID := d.Options[m.selectedOption-1].ID
					return m, m.resolveDecision(d.ID, optID, m.rationale)
				}
			}
		} else if m.inputMode == ModeText {
			m.status = "Custom text iteration not yet implemented. Use number keys (1-4) to select an option, or 'd' to dismiss."
			m.inputMode = ModeNormal
			m.textInput.Blur()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// filterDecisions filters and sorts decisions based on current filter.
func (m *Model) filterDecisions(decisions []DecisionItem) []DecisionItem {
	var result []DecisionItem

	if m.filter == "all" {
		result = decisions
	} else {
		for _, d := range decisions {
			if d.Urgency == m.filter {
				result = append(result, d)
			}
		}
	}

	// Sort by urgency (high first) then by time (newest first)
	sort.Slice(result, func(i, j int) bool {
		urgencyOrder := map[string]int{"high": 0, "medium": 1, "low": 2}
		ui := urgencyOrder[result[i].Urgency]
		uj := urgencyOrder[result[j].Urgency]
		if ui != uj {
			return ui < uj
		}
		return result[i].RequestedAt.After(result[j].RequestedAt)
	})

	return result
}

// View renders the TUI.
func (m *Model) View() string {
	return m.renderView()
}
