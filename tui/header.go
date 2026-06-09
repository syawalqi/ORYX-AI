package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type HeaderData struct {
	Model      string
	Alerts     int
	DaemonRunning bool
	PlanMode   bool
	ServerInfo string // compact server stats shown when sidebar is toggled
}

func RenderHeader(data HeaderData, width int) string {
	hs := headerStyle
	ms := headerModelStyle
	if data.PlanMode {
		hs = planHeaderStyle
		ms = planHeaderModelStyle
	}

	left := hs.Render(" ORYX ") +
		ms.Render(fmt.Sprintf("[%s]", data.Model))

	// Server info in dimmed style between model and health indicator
	if data.ServerInfo != "" {
		left += dimmedStyle.Render(" " + data.ServerInfo)
	}

	var right string
	if data.PlanMode {
		right = lipgloss.NewStyle().
			Background(lipgloss.Color("#10B981")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1).
			Bold(true).
			Render(" PLAN ")
	} else if data.Alerts > 0 {
		right = alertBadgeStyle.Render(fmt.Sprintf("⚠ %d alert(s)", data.Alerts))
	} else {
		right = dimmedStyle.Render("● healthy")
	}

	filler := width - lipgloss.Width(left) - lipgloss.Width(right)
	if filler < 0 {
		filler = 0
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", filler), right)
}
