package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	appStyle = lipgloss.NewStyle().
			Margin(0)

	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#7C3AED")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1).
			Bold(true)

	headerModelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#A78BFA")).
				Padding(0, 1)

	alertBadgeStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#EF4444")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1).
			Bold(true)

	userMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981")).
			Bold(true)

	userContentStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#34D399"))

	assistantMsgStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#60A5FA")).Bold(true)

	assistantContentStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#93C5FD"))

	toolMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Italic(true)

	cmdBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Padding(0, 1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444")).
			Bold(true)

	dimmedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))

	thoughtStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8B8B8B")).
			Italic(true)

	loadingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A78BFA")).
			Bold(true)

	sepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563"))

	chatBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("#7C3AED")).
			Padding(0, 1)
)
