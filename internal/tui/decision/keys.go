package decision

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines the key bindings for the decision TUI.
type KeyMap struct {
	// Navigation
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding

	// Quick select options (1-4)
	Select1 key.Binding
	Select2 key.Binding
	Select3 key.Binding
	Select4 key.Binding

	// Actions
	Confirm   key.Binding
	Rationale key.Binding
	Text      key.Binding
	Peek      key.Binding // Peek at agent's terminal
	Cancel    key.Binding // Exit TUI without making selection
	Dismiss   key.Binding // Dismiss/defer decision
	Refresh   key.Binding

	// Filtering
	FilterHigh key.Binding
	FilterAll  key.Binding

	// General
	Help key.Binding
	Quit key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("\u2191/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("\u2193/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdn", "page down"),
		),
		Select1: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "option 1"),
		),
		Select2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "option 2"),
		),
		Select3: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "option 3"),
		),
		Select4: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "option 4"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		Rationale: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "rationale"),
		),
		Text: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "text (stub)"),
		),
		Peek: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "peek terminal"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "exit"),
		),
		Dismiss: key.NewBinding(
			key.WithKeys("d", "D"),
			key.WithHelp("d", "dismiss"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "refresh"),
		),
		FilterHigh: key.NewBinding(
			key.WithKeys("!"),
			key.WithHelp("!", "urgent only"),
		),
		FilterAll: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "show all"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// ShortHelp returns key bindings for the short help view.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select1, k.Confirm, k.Rationale, k.Dismiss, k.Peek, k.Quit, k.Help}
}

// FullHelp returns key bindings for the full help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown},
		{k.Select1, k.Select2, k.Select3, k.Select4},
		{k.Confirm, k.Rationale, k.Text, k.Peek, k.Cancel},
		{k.Refresh, k.FilterHigh, k.FilterAll},
		{k.Help, k.Quit},
	}
}
