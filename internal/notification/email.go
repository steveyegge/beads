// Package notification provides email template rendering for decision points.
//
// hq-946577.21: Email notification template for decisions
package notification

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// EmailData holds the data for rendering email templates.
type EmailData struct {
	DecisionID   string
	Prompt       string
	Options      []EmailOption
	Default      string
	TimeoutAt    *time.Time
	TimeoutStr   string // Human-readable timeout
	RespondURL   string
	ViewURL      string
	Molecule     string
	Step         string
	Agent        string
}

// EmailOption represents a single option in the email.
type EmailOption struct {
	ID          string
	Short       string
	Label       string
	Description string
	IsDefault   bool
}

// EmailResult holds the rendered email content.
type EmailResult struct {
	Subject   string
	PlainText string
	HTML      string
}

// RenderEmail renders both HTML and plain text versions of the decision email.
func RenderEmail(payload *DecisionPayload) (*EmailResult, error) {
	// Build template data
	data := &EmailData{
		DecisionID: payload.ID,
		Prompt:     payload.Prompt,
		Default:    payload.Default,
		TimeoutAt:  payload.TimeoutAt,
		RespondURL: payload.RespondURL,
		ViewURL:    payload.ViewURL,
	}

	// Convert options
	for _, opt := range payload.Options {
		data.Options = append(data.Options, EmailOption{
			ID:          opt.ID,
			Short:       opt.Short,
			Label:       opt.Label,
			Description: opt.Description,
			IsDefault:   opt.ID == payload.Default,
		})
	}

	// Extract source info
	if payload.Source != nil {
		data.Molecule = payload.Source.Molecule
		data.Step = payload.Source.Step
		data.Agent = payload.Source.Agent
	}

	// Format timeout
	if payload.TimeoutAt != nil {
		remaining := time.Until(*payload.TimeoutAt)
		if remaining > 0 {
			data.TimeoutStr = formatDuration(remaining)
		} else {
			data.TimeoutStr = "OVERDUE"
		}
	}

	// Render subject
	subject := fmt.Sprintf("[Decision Required] %s", truncateSubject(payload.Prompt, 60))

	// Render plain text
	plainText, err := renderPlainText(data)
	if err != nil {
		return nil, fmt.Errorf("failed to render plain text: %w", err)
	}

	// Render HTML
	htmlContent, err := renderHTML(data)
	if err != nil {
		return nil, fmt.Errorf("failed to render HTML: %w", err)
	}

	return &EmailResult{
		Subject:   subject,
		PlainText: plainText,
		HTML:      htmlContent,
	}, nil
}

// truncateSubject shortens the subject line while preserving readability.
func truncateSubject(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Try to break at a word boundary
	if idx := strings.LastIndex(s[:maxLen-3], " "); idx > maxLen/2 {
		return s[:idx] + "..."
	}
	return s[:maxLen-3] + "..."
}

// formatDuration formats a duration in human-readable form.
func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("%d hours", hours)
	}
	days := hours / 24
	remainingHours := hours % 24
	if remainingHours == 0 {
		return fmt.Sprintf("%d days", days)
	}
	return fmt.Sprintf("%d days, %d hours", days, remainingHours)
}

// Plain text email template
const plainTextTemplate = `A decision is needed for workflow {{.Molecule}}:

  {{.Prompt}}

{{if .Options}}OPTIONS:
{{range .Options}}  [{{.ID}}] {{.Short}} - {{.Label}}{{if .IsDefault}} (default){{end}}
{{end}}
{{end}}{{if .Default}}Default option if no response: {{.Default}}
{{end}}{{if .TimeoutStr}}Timeout: {{.TimeoutStr}}{{if .TimeoutAt}} ({{.TimeoutAt.Format "2006-01-02 15:04 MST"}}){{end}}
{{end}}
RESPOND:
{{if .RespondURL}}  Click here: {{.RespondURL}}
{{end}}  Or reply to this email with just the option ID (e.g., "{{if .Options}}{{(index .Options 0).ID}}{{else}}yes{{end}}")
  Or use CLI: bd decision respond {{.DecisionID}} --select=<option>

{{if .ViewURL}}View details: {{.ViewURL}}
{{end}}---
This decision was created by {{if .Agent}}{{.Agent}}{{else}}beads{{end}}.
`

// HTML email template
const htmlTemplate = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Decision Required</title>
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
      line-height: 1.6;
      color: #333;
      max-width: 600px;
      margin: 0 auto;
      padding: 20px;
    }
    .header {
      background: #f8f9fa;
      border-left: 4px solid #007bff;
      padding: 15px;
      margin-bottom: 20px;
    }
    .header h1 {
      margin: 0;
      font-size: 18px;
      color: #007bff;
    }
    .prompt {
      font-size: 16px;
      font-weight: 500;
      margin: 20px 0;
      padding: 15px;
      background: #fff3cd;
      border-radius: 4px;
    }
    .options {
      margin: 20px 0;
    }
    .option {
      padding: 12px 15px;
      margin: 8px 0;
      background: #f8f9fa;
      border-radius: 4px;
      border: 1px solid #e9ecef;
    }
    .option.default {
      border-color: #28a745;
      background: #d4edda;
    }
    .option-id {
      display: inline-block;
      background: #6c757d;
      color: white;
      padding: 2px 8px;
      border-radius: 3px;
      font-family: monospace;
      font-size: 14px;
      margin-right: 8px;
    }
    .option.default .option-id {
      background: #28a745;
    }
    .option-label {
      font-weight: 500;
    }
    .option-short {
      color: #6c757d;
      font-size: 14px;
    }
    .default-badge {
      background: #28a745;
      color: white;
      padding: 2px 6px;
      border-radius: 3px;
      font-size: 12px;
      margin-left: 8px;
    }
    .meta {
      margin: 20px 0;
      padding: 15px;
      background: #f8f9fa;
      border-radius: 4px;
      font-size: 14px;
    }
    .meta-row {
      margin: 5px 0;
    }
    .meta-label {
      color: #6c757d;
      display: inline-block;
      width: 80px;
    }
    .cta {
      margin: 25px 0;
      text-align: center;
    }
    .cta-button {
      display: inline-block;
      background: #007bff;
      color: white;
      padding: 12px 30px;
      text-decoration: none;
      border-radius: 4px;
      font-weight: 500;
    }
    .cta-button:hover {
      background: #0056b3;
    }
    .alt-response {
      margin: 20px 0;
      padding: 15px;
      background: #e9ecef;
      border-radius: 4px;
      font-size: 14px;
    }
    .alt-response h3 {
      margin: 0 0 10px 0;
      font-size: 14px;
      color: #495057;
    }
    .alt-response code {
      background: #fff;
      padding: 2px 6px;
      border-radius: 3px;
      font-family: monospace;
    }
    .footer {
      margin-top: 30px;
      padding-top: 15px;
      border-top: 1px solid #e9ecef;
      font-size: 12px;
      color: #6c757d;
    }
  </style>
</head>
<body>
  <div class="header">
    <h1>Decision Required</h1>
  </div>

  <p>A decision is needed for workflow <strong>{{.Molecule}}</strong>:</p>

  <div class="prompt">{{.Prompt}}</div>

  {{if .Options}}
  <div class="options">
    <h3>Options</h3>
    {{range .Options}}
    <div class="option{{if .IsDefault}} default{{end}}">
      <span class="option-id">{{.ID}}</span>
      <span class="option-label">{{.Label}}</span>
      {{if .IsDefault}}<span class="default-badge">default</span>{{end}}
      {{if .Short}}<br><span class="option-short">{{.Short}}</span>{{end}}
    </div>
    {{end}}
  </div>
  {{end}}

  <div class="meta">
    {{if .Default}}
    <div class="meta-row">
      <span class="meta-label">Default:</span>
      <strong>{{.Default}}</strong> (if no response)
    </div>
    {{end}}
    {{if .TimeoutStr}}
    <div class="meta-row">
      <span class="meta-label">Timeout:</span>
      {{.TimeoutStr}}{{if .TimeoutAt}} ({{.TimeoutAt.Format "Jan 2, 2006 3:04 PM MST"}}){{end}}
    </div>
    {{end}}
  </div>

  {{if .RespondURL}}
  <div class="cta">
    <a href="{{.RespondURL}}" class="cta-button">Respond Now</a>
  </div>
  {{end}}

  <div class="alt-response">
    <h3>Alternative Ways to Respond</h3>
    <p>Reply to this email with just the option ID (e.g., <code>{{if .Options}}{{(index .Options 0).ID}}{{else}}yes{{end}}</code>)</p>
    <p>Or use the CLI: <code>bd decision respond {{.DecisionID}} --select=&lt;option&gt;</code></p>
  </div>

  {{if .ViewURL}}
  <p><a href="{{.ViewURL}}">View full details</a></p>
  {{end}}

  <div class="footer">
    <p>This decision was created by {{if .Agent}}{{.Agent}}{{else}}beads{{end}}.</p>
    <p>Decision ID: {{.DecisionID}}</p>
  </div>
</body>
</html>`

// renderPlainText renders the plain text email.
func renderPlainText(data *EmailData) (string, error) {
	tmpl, err := template.New("plaintext").Parse(plainTextTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// renderHTML renders the HTML email.
func renderHTML(data *EmailData) (string, error) {
	tmpl, err := template.New("html").Parse(htmlTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// FormatOptionsCompact formats options for compact display (SMS, CLI).
func FormatOptionsCompact(payload *DecisionPayload) string {
	if len(payload.Options) == 0 {
		return ""
	}

	var parts []string
	for _, opt := range payload.Options {
		if opt.Short != "" {
			parts = append(parts, fmt.Sprintf("%s) %s", opt.ID, opt.Short))
		} else {
			parts = append(parts, fmt.Sprintf("%s) %s", opt.ID, opt.Label))
		}
	}
	return strings.Join(parts, "  ")
}
