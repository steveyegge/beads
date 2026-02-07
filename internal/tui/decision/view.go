package decision

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/ui"
)

// renderView renders the entire view.
func (m *Model) renderView() string {
	var b strings.Builder

	// Check terminal size
	if m.width < 40 || m.height < 10 {
		return "Terminal too small. Please resize."
	}

	// Peek mode - show terminal content instead of normal view
	if m.peeking {
		return m.renderPeekMode()
	}

	// Title
	b.WriteString(titleStyle.Render("Decision Watch"))
	b.WriteString("\n")

	// Error message
	if m.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
	}

	// Input mode
	if m.inputMode != ModeNormal {
		return m.renderInputMode(&b)
	}

	// Empty state
	if len(m.decisions) == 0 {
		b.WriteString("\n")
		b.WriteString(statusStyle.Render("No pending decisions."))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Watching for new decisions... (press q to quit)"))
		b.WriteString("\n")
		return b.String()
	}

	// Calculate layout
	listHeight := m.height / 3
	if listHeight < 5 {
		listHeight = 5
	}

	// Render decision list
	b.WriteString(m.renderDecisionList(listHeight))
	b.WriteString("\n")

	// Render detail pane for selected decision
	if m.selected < len(m.decisions) {
		b.WriteString(m.renderDetailPane())
	}

	// Status line
	if m.status != "" {
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(m.status))
	}

	// Help
	if m.showHelp {
		b.WriteString("\n\n")
		b.WriteString(m.help.View(m.keys))
	} else {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("j/k: navigate  1-4: select  r: rationale  d: dismiss  p: peek  enter: confirm  ?: help  q: quit"))
	}

	return b.String()
}

// renderDecisionList renders the list of pending decisions.
func (m *Model) renderDecisionList(maxHeight int) string {
	var b strings.Builder

	header := fmt.Sprintf("─── Pending Decisions (%d) ", len(m.decisions))
	header += strings.Repeat("─", max(0, m.width-len(header)-2))
	b.WriteString(helpStyle.Render(header))
	b.WriteString("\n")

	for i, d := range m.decisions {
		if i >= maxHeight-2 {
			b.WriteString(helpStyle.Render(fmt.Sprintf("  ... and %d more", len(m.decisions)-i)))
			break
		}

		isSelected := i == m.selected
		line := m.renderDecisionLine(d, isSelected)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// renderDecisionLine renders a single decision list item.
func (m *Model) renderDecisionLine(d DecisionItem, selected bool) string {
	urgency := urgencyLabel(d.Urgency)
	timeAgo := formatTimeAgo(d.RequestedAt)

	// Truncate ID for display
	displayID := d.ID
	maxIDLen := 45
	if len(displayID) > maxIDLen {
		displayID = displayID[:maxIDLen-3] + "..."
	}

	// Build line with ID
	line := fmt.Sprintf(" %s %s", urgency, displayID)

	// Add requester and time on meta line
	meta := fmt.Sprintf("  %s · %s", detailLabelStyle.Render(d.RequestedBy), detailLabelStyle.Render(timeAgo))

	if selected {
		return selectedItemStyle.Render(line) + "\n" + meta
	}
	return normalItemStyle.Render(line) + "\n" + meta
}

// renderDetailPane renders the detail view for the selected decision.
func (m *Model) renderDetailPane() string {
	if m.selected >= len(m.decisions) {
		return ""
	}

	d := m.decisions[m.selected]
	var b strings.Builder

	// Header
	header := "─── Decision Details "
	header += strings.Repeat("─", max(0, m.width-len(header)-2))
	b.WriteString(helpStyle.Render(header))
	b.WriteString("\n\n")

	// Question (wrap to terminal width)
	wrappedPrompt := ui.WrapText(d.Prompt, m.width-4)
	b.WriteString(detailTitleStyle.Render(wrappedPrompt))
	b.WriteString("\n\n")

	// Predecessor chain info (if chained decision)
	if d.PredecessorID != "" {
		b.WriteString(detailLabelStyle.Render("Predecessor: "))
		b.WriteString(detailValueStyle.Render(d.PredecessorID))
		b.WriteString("\n\n")
	}

	// Context if available (with JSON formatting)
	if d.Context != "" {
		b.WriteString(detailLabelStyle.Render("Context:"))
		b.WriteString("\n")
		b.WriteString(formatContextDisplay(d.Context, m.width-4))
		b.WriteString("\n\n")
	}

	// Options
	b.WriteString(detailLabelStyle.Render("Options:"))
	b.WriteString("\n")

	for i, opt := range d.Options {
		optNum := i + 1
		isSelected := optNum == m.selectedOption

		// Option number
		numStr := optionNumberStyle.Render(fmt.Sprintf("[%d]", optNum))

		// Option label
		label := opt.Label
		if opt.Short != "" && opt.Short != opt.ID {
			label = opt.Short + ": " + label
		}

		// Build option line (number + label)
		optLine := fmt.Sprintf("  %s %s", numStr, label)

		if isSelected {
			b.WriteString(selectedOptionStyle.Render(optLine))
			b.WriteString(" \u2190")
		} else {
			b.WriteString(optionLabelStyle.Render(optLine))
		}
		b.WriteString("\n")

		// Option description on separate line, wrapped and indented
		if opt.Description != "" {
			wrappedDesc := ui.WrapText(opt.Description, m.width-10)
			for _, descLine := range strings.Split(wrappedDesc, "\n") {
				b.WriteString("       ")
				b.WriteString(optionDescStyle.Render(descLine))
				b.WriteString("\n")
			}
		}
	}

	// Show selected option instructions
	if m.selectedOption > 0 {
		b.WriteString("\n")
		b.WriteString(successStyle.Render(fmt.Sprintf("Option %d selected. ", m.selectedOption)))
		b.WriteString(helpStyle.Render("Press Enter to confirm, r for rationale"))
	}

	// Show rationale if set
	if m.rationale != "" {
		b.WriteString("\n")
		b.WriteString(detailLabelStyle.Render("Rationale: "))
		b.WriteString(detailValueStyle.Render(m.rationale))
	}

	return b.String()
}

// renderInputMode renders the input mode view.
func (m *Model) renderInputMode(b *strings.Builder) string {
	b.WriteString("\n")

	switch m.inputMode {
	case ModeRationale:
		b.WriteString(inputLabelStyle.Render("Enter rationale:"))
		b.WriteString("\n\n")
		b.WriteString(m.textInput.View())
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Enter: confirm  Esc: cancel"))

	case ModeText:
		b.WriteString(inputLabelStyle.Render("Custom text response (not yet implemented):"))
		b.WriteString("\n\n")
		b.WriteString(m.textInput.View())
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Enter: (will show message)  Esc: cancel"))
	}

	return b.String()
}

// formatTimeAgo formats a time as "X ago".
func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// renderPeekMode renders the terminal peek overlay.
func (m *Model) renderPeekMode() string {
	var b strings.Builder

	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Header
	header := fmt.Sprintf("─── Terminal Peek: %s ", m.peekSessionName)
	header += strings.Repeat("─", max(0, m.width-len(header)-2))
	b.WriteString(titleStyle.Render(header))
	b.WriteString("\n\n")

	// Content
	if m.peekContent == "" {
		b.WriteString(helpStyle.Render("(No terminal content captured)"))
		b.WriteString("\n\n")
		b.WriteString(strings.Repeat("─", max(0, m.width-2)))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Press any key to close"))
		return b.String()
	}

	if m.peekViewport.Width == 0 || m.peekViewport.Height == 0 {
		m.peekViewport.Width = m.width - 4
		m.peekViewport.Height = m.height - 6
	}

	b.WriteString(m.peekViewport.View())
	b.WriteString("\n")

	// Footer with scroll position
	scrollPercent := m.peekViewport.ScrollPercent() * 100
	scrollInfo := fmt.Sprintf(" %.0f%% ", scrollPercent)
	footerLeft := strings.Repeat("─", max(0, (m.width-len(scrollInfo))/2))
	footerRight := strings.Repeat("─", max(0, m.width-len(footerLeft)-len(scrollInfo)-2))
	b.WriteString(helpStyle.Render(footerLeft + scrollInfo + footerRight))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓/j/k: scroll  pgup/pgdn: page  any other key: close"))

	return b.String()
}

// formatContextDisplay formats JSON context for display with pretty-printing.
func formatContextDisplay(context string, maxWidth int) string {
	if context == "" {
		return ""
	}

	var b strings.Builder

	// Try to parse as JSON
	var parsed interface{}
	if err := json.Unmarshal([]byte(context), &parsed); err != nil {
		// Not valid JSON, display as plain text
		wrappedContext := ui.WrapText(context, maxWidth)
		b.WriteString(detailValueStyle.Render(wrappedContext))
		return b.String()
	}

	// Check for successor_schemas in context
	if obj, ok := parsed.(map[string]interface{}); ok {
		if schemas, hasSchemas := obj["successor_schemas"]; hasSchemas {
			b.WriteString(successorSchemaStyle.Render("  Successor Schemas:"))
			b.WriteString("\n")

			if schemaMap, ok := schemas.(map[string]interface{}); ok {
				for optLabel, schema := range schemaMap {
					b.WriteString(fmt.Sprintf("    %s:\n", optionLabelStyle.Render(optLabel)))
					schemaJSON, _ := json.MarshalIndent(schema, "      ", "  ")
					for _, line := range strings.Split(string(schemaJSON), "\n") {
						b.WriteString("      ")
						b.WriteString(jsonValueStyle.Render(line))
						b.WriteString("\n")
					}
				}
			}
			b.WriteString("\n")

			delete(obj, "successor_schemas")
			if len(obj) == 0 {
				return b.String()
			}
			parsed = obj
		}
	}

	// Pretty-print the JSON with indentation
	prettyJSON, err := json.MarshalIndent(parsed, "  ", "  ")
	if err != nil {
		wrappedContext := ui.WrapText(context, maxWidth)
		b.WriteString(detailValueStyle.Render(wrappedContext))
		return b.String()
	}

	// Display formatted JSON with syntax coloring
	for _, line := range strings.Split(string(prettyJSON), "\n") {
		b.WriteString("  ")
		b.WriteString(colorizeJSONLine(line))
		b.WriteString("\n")
	}

	return b.String()
}

// colorizeJSONLine applies simple syntax highlighting to a JSON line.
func colorizeJSONLine(line string) string {
	result := line

	if strings.Contains(line, ":") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := parts[0]
			value := parts[1]

			key = jsonKeyStyle.Render(key)

			trimmedValue := strings.TrimSpace(value)
			if strings.HasPrefix(trimmedValue, "\"") {
				value = jsonStringStyle.Render(value)
			} else if trimmedValue == "true" || trimmedValue == "false" || trimmedValue == "null" {
				value = jsonBoolStyle.Render(value)
			} else if len(trimmedValue) > 0 && (trimmedValue[0] >= '0' && trimmedValue[0] <= '9' || trimmedValue[0] == '-') {
				value = jsonNumberStyle.Render(value)
			}

			return key + ":" + value
		}
	}

	return jsonValueStyle.Render(result)
}
