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
}

func RenderHeader(data HeaderData, width int) string {
	left := headerStyle.Render(" Flare ") +
		headerModelStyle.Render(fmt.Sprintf("[%s]", data.Model))

	var right string
	if data.Alerts > 0 {
		right = alertBadgeStyle.Render(fmt.Sprintf("⚠ %d alert(s)", data.Alerts))
	} else {
		right = dimmedStyle.Render("● healthy")
	}

	filler := strings.Repeat(" ", width-lipgloss.Width(left)-lipgloss.Width(right))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, filler, right)
}
