package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

// renderMarkdown converts Markdown text to ANSI-styled terminal output.
// A new renderer is created per-call with the correct width so lines
// never exceed the available viewport space.
func renderMarkdown(content string, width int) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}

	if width < 10 {
		width = 10
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return wrapText(content, width)
	}

	out, err := r.Render(content)
	if err != nil {
		return wrapText(content, width)
	}

	// Trim trailing newlines glamour adds
	return strings.TrimRight(out, "\n")
}
