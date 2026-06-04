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
	dim       = lipgloss.Color("#374151") // dark gray for borders

	// Layout — main container with double border matching chat TUI
	docStyle = lipgloss.NewStyle().
			Padding(1, 2)

	// Outer border box
	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(primary).
			Padding(1, 2).
			Width(70)

	// Logo style — the FLARE ASCII eye (small version)
	logoStyle = lipgloss.NewStyle().
			Foreground(primary).
			Bold(true).
			Align(lipgloss.Center).
			Width(66)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primary).
			Align(lipgloss.Center).
			Width(66)

	// Progress step styles
	stepStyle = lipgloss.NewStyle().
			Foreground(muted).
			MarginBottom(1)

	activeStepStyle = lipgloss.NewStyle().
			Foreground(primary).
			Bold(true)

	successStepStyle = lipgloss.NewStyle().
				Foreground(success).
				Bold(true)

	// Content area box
	contentStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dim).
			Padding(1, 2).
			Width(64)

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
			BorderForeground(success)

	// Footer with keyboard hints
	footerStyle = lipgloss.NewStyle().
			Foreground(dim).
			Align(lipgloss.Center).
			Width(66).
			MarginTop(1)

	// Error state box
	errorBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(danger).
			Padding(1, 2).
			Width(64)
)
