// Package notification handles sending notifications for decision points.
// Notifications are dispatched to configured channels (email, webhook, SMS)
// based on routes defined in settings/escalation.json.
//
// hq-946577.20: Notification dispatch for decision points
package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// DecisionPayload is the notification payload sent when a decision point is created.
type DecisionPayload struct {
	Type       string                 `json:"type"`       // "decision_point"
	ID         string                 `json:"id"`         // Decision point ID (e.g., "gt-abc123.decision-1")
	Prompt     string                 `json:"prompt"`     // Question being asked
	Options    []types.DecisionOption `json:"options"`    // Available choices
	Default    string                 `json:"default"`    // Default option ID if timeout
	TimeoutAt  *time.Time             `json:"timeout_at"` // When the decision times out
	RespondURL string                 `json:"respond_url"`// URL to submit response
	ViewURL    string                 `json:"view_url"`   // URL to view decision details
	Source     *PayloadSource         `json:"source"`     // Context about what created this
}

// PayloadSource provides context about the decision point origin.
type PayloadSource struct {
	Agent    string `json:"agent,omitempty"`    // Agent that created this
	Molecule string `json:"molecule,omitempty"` // Parent molecule
	Step     string `json:"step,omitempty"`     // Step ID
}

// EscalationConfig holds the escalation settings from escalation.json.
type EscalationConfig struct {
	Type             string                       `json:"type"`
	Version          int                          `json:"version"`
	Routes           map[string][]string          `json:"routes"`
	DecisionRoutes   map[string][]string          `json:"decision_routes"`
	Contacts         map[string]string            `json:"contacts"`
	DecisionSettings *DecisionSettings            `json:"decision_settings"`
}

// DecisionSettings holds decision-specific configuration.
type DecisionSettings struct {
	DefaultTimeout  string `json:"default_timeout"`
	RemindInterval  string `json:"remind_interval"`
	MaxReminders    int    `json:"max_reminders"`
}

// DispatchResult records the outcome of a notification dispatch.
type DispatchResult struct {
	Channel string `json:"channel"` // e.g., "email:human", "webhook"
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Dispatcher handles sending notifications for decision points.
type Dispatcher struct {
	config     *EscalationConfig
	baseURL    string // Base URL for respond/view URLs
	httpClient *http.Client
}

// NewDispatcher creates a new notification dispatcher.
// beadsDir is the path to the .beads directory containing settings.
// baseURL is the base URL for response endpoints (can be empty for CLI-only use).
func NewDispatcher(beadsDir, baseURL string) (*Dispatcher, error) {
	config, err := LoadEscalationConfig(beadsDir)
	if err != nil {
		// Return dispatcher with nil config - will use defaults
		return &Dispatcher{
			config:     nil,
			baseURL:    baseURL,
			httpClient: &http.Client{Timeout: 30 * time.Second},
		}, nil
	}

	return &Dispatcher{
		config:     config,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// LoadEscalationConfig loads the escalation configuration from settings/escalation.json.
func LoadEscalationConfig(beadsDir string) (*EscalationConfig, error) {
	configPath := filepath.Join(beadsDir, "settings", "escalation.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read escalation config: %w", err)
	}

	var config EscalationConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse escalation config: %w", err)
	}

	return &config, nil
}

// BuildPayload creates a notification payload for a decision point.
func (d *Dispatcher) BuildPayload(dp *types.DecisionPoint, issue *types.Issue, source *PayloadSource) (*DecisionPayload, error) {
	// Parse options from JSON string
	var options []types.DecisionOption
	if dp.Options != "" {
		if err := json.Unmarshal([]byte(dp.Options), &options); err != nil {
			return nil, fmt.Errorf("failed to parse decision options: %w", err)
		}
	}

	// Calculate timeout
	var timeoutAt *time.Time
	if issue != nil && issue.Timeout > 0 {
		t := dp.CreatedAt.Add(issue.Timeout)
		timeoutAt = &t
	}

	// Build URLs
	respondURL := ""
	viewURL := ""
	if d.baseURL != "" {
		respondURL = fmt.Sprintf("%s/api/decisions/%s/respond", d.baseURL, dp.IssueID)
		viewURL = fmt.Sprintf("%s/decisions/%s", d.baseURL, dp.IssueID)
	}

	return &DecisionPayload{
		Type:       "decision_point",
		ID:         dp.IssueID,
		Prompt:     dp.Prompt,
		Options:    options,
		Default:    dp.DefaultOption,
		TimeoutAt:  timeoutAt,
		RespondURL: respondURL,
		ViewURL:    viewURL,
		Source:     source,
	}, nil
}

// Dispatch sends notifications to all configured channels for a decision point.
// routeKey is the route to use (e.g., "default", "urgent"). Empty uses "default".
func (d *Dispatcher) Dispatch(payload *DecisionPayload, routeKey string) []DispatchResult {
	if routeKey == "" {
		routeKey = "default"
	}

	// Get routes to notify
	routes := d.getRoutes(routeKey)
	if len(routes) == 0 {
		// No routes configured, log a warning and return
		return []DispatchResult{{
			Channel: "none",
			Success: false,
			Error:   "no notification routes configured",
		}}
	}

	var results []DispatchResult

	for _, route := range routes {
		result := d.dispatchToChannel(payload, route)
		results = append(results, result)
	}

	return results
}

// getRoutes returns the notification routes for the given key.
func (d *Dispatcher) getRoutes(routeKey string) []string {
	if d.config == nil || d.config.DecisionRoutes == nil {
		// Default fallback routes
		return []string{"log"}
	}

	routes, ok := d.config.DecisionRoutes[routeKey]
	if !ok {
		// Fall back to default if specific route not found
		routes, ok = d.config.DecisionRoutes["default"]
		if !ok {
			return []string{"log"}
		}
	}

	return routes
}

// dispatchToChannel sends a notification to a specific channel.
func (d *Dispatcher) dispatchToChannel(payload *DecisionPayload, channel string) DispatchResult {
	result := DispatchResult{Channel: channel}

	switch {
	case channel == "log":
		result.Success = true
		d.logNotification(payload)

	case strings.HasPrefix(channel, "email:"):
		recipient := strings.TrimPrefix(channel, "email:")
		email := d.resolveContact(recipient, "email")
		if email == "" {
			result.Success = false
			result.Error = fmt.Sprintf("no email configured for %s", recipient)
		} else {
			err := d.sendEmail(payload, email)
			result.Success = err == nil
			if err != nil {
				result.Error = err.Error()
			}
		}

	case channel == "webhook":
		webhookURL := d.resolveContact("decision_webhook", "")
		if webhookURL == "" {
			result.Success = false
			result.Error = "no webhook URL configured"
		} else {
			err := d.sendWebhook(payload, webhookURL)
			result.Success = err == nil
			if err != nil {
				result.Error = err.Error()
			}
		}

	case strings.HasPrefix(channel, "sms:"):
		recipient := strings.TrimPrefix(channel, "sms:")
		phone := d.resolveContact(recipient, "sms")
		if phone == "" {
			result.Success = false
			result.Error = fmt.Sprintf("no phone number configured for %s", recipient)
		} else {
			err := d.sendSMS(payload, phone)
			result.Success = err == nil
			if err != nil {
				result.Error = err.Error()
			}
		}

	default:
		result.Success = false
		result.Error = fmt.Sprintf("unknown channel type: %s", channel)
	}

	return result
}

// resolveContact looks up a contact from the configuration.
func (d *Dispatcher) resolveContact(name, contactType string) string {
	if d.config == nil || d.config.Contacts == nil {
		return ""
	}

	// Try specific key first (e.g., "human_email" for email:human)
	if contactType != "" {
		key := fmt.Sprintf("%s_%s", name, contactType)
		if val, ok := d.config.Contacts[key]; ok {
			return val
		}
	}

	// Try direct key
	if val, ok := d.config.Contacts[name]; ok {
		return val
	}

	return ""
}

// logNotification logs the notification to stdout (for testing/debugging).
func (d *Dispatcher) logNotification(payload *DecisionPayload) {
	fmt.Printf("\n📬 Decision Point Notification\n")
	fmt.Printf("   ID: %s\n", payload.ID)
	fmt.Printf("   Prompt: %s\n", payload.Prompt)
	fmt.Printf("   Options:\n")
	for _, opt := range payload.Options {
		defaultMark := ""
		if opt.ID == payload.Default {
			defaultMark = " (default)"
		}
		fmt.Printf("     [%s] %s%s\n", opt.ID, opt.Label, defaultMark)
	}
	if payload.TimeoutAt != nil {
		fmt.Printf("   Timeout: %s\n", payload.TimeoutAt.Format(time.RFC3339))
	}
	if payload.RespondURL != "" {
		fmt.Printf("   Respond URL: %s\n", payload.RespondURL)
	}
	fmt.Println()
}

// sendEmail sends an email notification.
func (d *Dispatcher) sendEmail(payload *DecisionPayload, to string) error {
	// Build email content
	subject := fmt.Sprintf("[Decision Required] %s", truncate(payload.Prompt, 60))

	var body strings.Builder
	body.WriteString(fmt.Sprintf("A decision is needed for %s:\n\n", payload.ID))
	body.WriteString(fmt.Sprintf("  %s\n\n", payload.Prompt))

	if len(payload.Options) > 0 {
		body.WriteString("Options:\n")
		for _, opt := range payload.Options {
			defaultMark := ""
			if opt.ID == payload.Default {
				defaultMark = " (default)"
			}
			body.WriteString(fmt.Sprintf("  [%s] %s - %s%s\n", opt.ID, opt.Short, opt.Label, defaultMark))
		}
		body.WriteString("\n")
	}

	if payload.TimeoutAt != nil {
		body.WriteString(fmt.Sprintf("Timeout: %s\n", payload.TimeoutAt.Format("2006-01-02 15:04 MST")))
	}

	if payload.RespondURL != "" {
		body.WriteString(fmt.Sprintf("\nRespond: %s\n", payload.RespondURL))
	}

	body.WriteString(fmt.Sprintf("\nOr use CLI: bd decision respond %s --select=<option>\n", payload.ID))

	// Try to send via system mail command
	cmd := exec.Command("mail", "-s", subject, to)
	cmd.Stdin = strings.NewReader(body.String())

	if err := cmd.Run(); err != nil {
		// Fall back to logging
		fmt.Printf("📧 Email notification (to %s):\n", to)
		fmt.Printf("   Subject: %s\n", subject)
		fmt.Printf("   Body:\n%s\n", body.String())
		return fmt.Errorf("mail command failed (logged instead): %w", err)
	}

	return nil
}

// sendWebhook sends a webhook notification.
func (d *Dispatcher) sendWebhook(payload *DecisionPayload, webhookURL string) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Beads-Event", "decision_point")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// sendSMS sends an SMS notification.
func (d *Dispatcher) sendSMS(payload *DecisionPayload, phone string) error {
	// Build compact SMS message
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("[Beads] Decision: %s\n", truncate(payload.Prompt, 40)))

	if len(payload.Options) > 0 {
		var opts []string
		for _, opt := range payload.Options {
			opts = append(opts, fmt.Sprintf("%s) %s", opt.ID, opt.Short))
		}
		msg.WriteString(strings.Join(opts, "  "))
		msg.WriteString("\n")
	}

	if payload.Default != "" && payload.TimeoutAt != nil {
		remaining := time.Until(*payload.TimeoutAt).Round(time.Hour)
		msg.WriteString(fmt.Sprintf("Default: %s (%s)\n", payload.Default, remaining))
	}

	// Try to send via twilio CLI or similar
	// For now, just log the message
	fmt.Printf("📱 SMS notification (to %s):\n", phone)
	fmt.Printf("%s\n", msg.String())

	return nil
}

// truncate shortens a string to the specified length with ellipsis.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return "..."
	}
	return s[:maxLen-3] + "..."
}

// DispatchDecisionNotification is a convenience function for dispatching
// a notification for a new decision point.
func DispatchDecisionNotification(beadsDir string, dp *types.DecisionPoint, issue *types.Issue, routeKey string) ([]DispatchResult, error) {
	dispatcher, err := NewDispatcher(beadsDir, "")
	if err != nil {
		return nil, err
	}

	// Extract source info from issue ID
	var source *PayloadSource
	if issue != nil {
		// Parse molecule and step from decision ID (format: mol.decision-step)
		parts := strings.Split(dp.IssueID, ".decision-")
		if len(parts) == 2 {
			source = &PayloadSource{
				Molecule: parts[0],
				Step:     parts[1],
			}
		}
		if issue.Assignee != "" {
			if source == nil {
				source = &PayloadSource{}
			}
			source.Agent = issue.Assignee
		}
	}

	payload, err := dispatcher.BuildPayload(dp, issue, source)
	if err != nil {
		return nil, err
	}

	return dispatcher.Dispatch(payload, routeKey), nil
}
