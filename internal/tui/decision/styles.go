package decision

import "github.com/charmbracelet/lipgloss"

// Color palette
var (
	colorHighUrgency   = lipgloss.Color("196") // bright red
	colorMediumUrgency = lipgloss.Color("214") // orange
	colorLowUrgency    = lipgloss.Color("76")  // green
	colorSelected      = lipgloss.Color("39")  // blue
	colorMuted         = lipgloss.Color("242") // gray
	colorWhite         = lipgloss.Color("15")
)

// Styles for the decision TUI
var (
	// Title styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")).
			MarginBottom(1)

	// List item styles
	selectedItemStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("236")).
				Foreground(colorWhite).
				Bold(true)

	normalItemStyle = lipgloss.NewStyle().
			Foreground(colorWhite)

	// Urgency indicators
	highUrgencyStyle = lipgloss.NewStyle().
				Foreground(colorHighUrgency).
				Bold(true)

	mediumUrgencyStyle = lipgloss.NewStyle().
				Foreground(colorMediumUrgency)

	lowUrgencyStyle = lipgloss.NewStyle().
			Foreground(colorLowUrgency)

	// Detail pane styles
	detailTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorSelected).
				MarginBottom(1)

	detailLabelStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	// Option styles
	optionNumberStyle = lipgloss.NewStyle().
				Foreground(colorSelected).
				Bold(true)

	optionLabelStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	optionDescStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	selectedOptionStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("236")).
				Foreground(colorWhite)

	// Input styles
	inputLabelStyle = lipgloss.NewStyle().
			Foreground(colorSelected).
			Bold(true)

	// Help and status
	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	statusStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorHighUrgency)

	successStyle = lipgloss.NewStyle().
			Foreground(colorLowUrgency)

	// JSON syntax highlighting styles
	jsonKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")) // cyan - for JSON keys

	jsonStringStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("107")) // green - for string values

	jsonNumberStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("141")) // purple - for numbers

	jsonBoolStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")) // orange - for booleans/null

	jsonValueStyle = lipgloss.NewStyle().
			Foreground(colorWhite) // default for other JSON

	// Successor schema styling
	successorSchemaStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("178")). // gold
				Bold(true)
)

// urgencyLabel returns a styled urgency label
func urgencyLabel(urgency string) string {
	switch urgency {
	case "high":
		return highUrgencyStyle.Render("[HIGH]")
	case "medium":
		return mediumUrgencyStyle.Render("[MED]")
	case "low":
		return lowUrgencyStyle.Render("[LOW]")
	default:
		return mediumUrgencyStyle.Render("[MED]")
	}
}
