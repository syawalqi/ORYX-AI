package tui

import (
	"strings"
	"unicode/utf8"
)

// RenderOryxLogo returns "ORYX" in ASCII block text (ANSI Shadow font).
func RenderOryxLogo(width, viewportHeight int) string {
	art := []string{
		` ██████╗ ██████╗ ██╗   ██╗██╗  ██╗`,
		`██╔═══██╗██╔══██╗╚██╗ ██╔╝╚██╗██╔╝`,
		`██║   ██║██████╔╝ ╚████╔╝  ╚███╔╝ `,
		`██║   ██║██╔══██╗  ╚██╔╝   ██╔██╗ `,
		`╚██████╔╝██║  ██║   ██║   ██╔╝ ██╗`,
		` ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝`,
	}

	// Calculate visual width (rune count) of each line
	maxVisualW := 0
	for _, line := range art {
		w := utf8.RuneCountInString(line)
		if w > maxVisualW {
			maxVisualW = w
		}
	}

	var b strings.Builder

	// Vertical centering
	totalArtHeight := len(art) + 3
	topPad := (viewportHeight - totalArtHeight) / 2
	if topPad < 0 {
		topPad = 0
	}
	for i := 0; i < topPad; i++ {
		b.WriteString("\n")
	}

	pad := (width - maxVisualW) / 2
	if pad < 0 {
		pad = 0
	}

	for _, line := range art {
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(assistantContentStyle.Render(line) + "\n")
	}

	b.WriteString("\n")

	tagline := "ORYX — General Purpose AI Agent"
	tagPad := (width - utf8.RuneCountInString(tagline)) / 2
	if tagPad < 0 {
		tagPad = 0
	}
	b.WriteString(strings.Repeat(" ", tagPad) + dimmedStyle.Render(tagline) + "\n")

	hint := "Send a message to start."
	hintPad := (width - len(hint)) / 2
	if hintPad < 0 {
		hintPad = 0
	}
	b.WriteString(strings.Repeat(" ", hintPad) + dimmedStyle.Render(hint) + "\n")

	return b.String()
}
