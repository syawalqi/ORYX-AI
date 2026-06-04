package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

var (
	mdRenderer     *glamour.TermRenderer
	mdRendererOnce sync.Once
)

// getRenderer returns the cached glamour renderer, creating it once.
func getRenderer() *glamour.TermRenderer {
	mdRendererOnce.Do(func() {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(200), // generous width; terminal handles actual wrapping
		)
		if err != nil {
			// Fallback: renderer will be nil, renderMarkdown falls back to plain text
			return
		}
		mdRenderer = r
	})
	return mdRenderer
}

// renderMarkdown converts Markdown text to ANSI-styled terminal output.
// The renderer is cached at first call, not recreated on every call.
func renderMarkdown(content string, width int) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}

	r := getRenderer()
	if r == nil {
		return wrapText(content, width)
	}

	out, err := r.Render(content)
	if err != nil {
		return wrapText(content, width)
	}

	// Trim trailing newlines glamour adds
	return strings.TrimRight(out, "\n")
}
