package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/syawalqi/oryx/agent"
)

// CmdBarItem describes a slash command shown in the palette and command bar.
type CmdBarItem struct {
	Label string
	Desc  string
}

// CmdBarItems lists all available slash commands.
var CmdBarItems = []CmdBarItem{
	{Label: "/save", Desc: "save conversation to state DB"},
	{Label: "/load", Desc: "load a saved conversation"},
	{Label: "/plan", Desc: "toggle plan mode (read-only)"},
	{Label: "/help", Desc: "show key bindings"},
	{Label: "/exit", Desc: "quit ORYX"},
}

// RenderCmdBar builds the footer command bar showing available shortcuts.
func RenderCmdBar(width int) string {
	items := []string{
		"Ctrl+P plan",
		"Ctrl+R reasoning",
		"Ctrl+T tools",
		"Ctrl+S sidebar",
		"/ help",
	}
	sep := dimmedStyle.Render(" • ")
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Render(dimmedStyle.Render(" " + strings.Join(items, sep)))
}

// handlePaletteKey processes keys when the command palette is active.
func (m *Model) handlePaletteKey(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "up":
		m.palette.CursorUp()
		return m, nil
	case "down":
		m.palette.CursorDown()
		return m, nil
	case "enter":
		selected := m.palette.Selected()
		if selected != "" {
			m.input = selected + " "
		}
		m.palette.Filter(m.input)
		m.updateViewport()
		return m, nil
	case "tab":
		selected := m.palette.Selected()
		if selected != "" {
			m.input = selected + " "
			m.palette.Filter(m.input)
			m.updateViewport()
		}
		return m, nil
	case "esc":
		m.input = ""
		m.palette.Filter("")
		m.updateViewport()
		return m, nil
	default:
		// Regular character input is already handled by handleKey
		return m, nil
	}
}

// planEventMsg carries a plan execution event for the TUI.
type planEventMsg struct {
	event agent.PlanEvent
}
