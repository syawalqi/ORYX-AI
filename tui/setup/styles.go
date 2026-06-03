package setup

import "github.com/charmbracelet/lipgloss"

// Style variables for the setup wizard TUI.
var (
	// Colors
	primary   = lipgloss.Color("#7C3AED") // purple
	success   = lipgloss.Color("#10B981") // green
	danger    = lipgloss.Color("#EF4444") // red
	muted     = lipgloss.Color("#6B7280") // gray
	highlight = lipgloss.Color("#F59E0B") // amber

	// Layout
	docStyle = lipgloss.NewStyle().
			Padding(1, 2)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primary).
			MarginBottom(1)

	stepStyle = lipgloss.NewStyle().
			Foreground(muted).
			MarginBottom(1)

	activeStepStyle = lipgloss.NewStyle().
				Foreground(primary).
				Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(danger).
			MarginTop(1)

	successStyle = lipgloss.NewStyle().
			Foreground(success).
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(muted).
			Italic(true)

	keySummaryStyle = lipgloss.NewStyle().
			Foreground(highlight).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)

	labelStyle = lipgloss.NewStyle().
			Bold(true).
			MarginRight(1)

	valueStyle = lipgloss.NewStyle().
			Foreground(muted)

	confirmStyle = lipgloss.NewStyle().
			MarginTop(1).
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primary)
)
