package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const paletteMaxVisible = 8

// CmdPalette provides a filtered, navigable list of slash commands.
type CmdPalette struct {
	items    []CmdBarItem
	filtered []CmdBarItem
	cursor   int
	offset   int // scroll position into filtered list
}

// NewCmdPalette creates a palette pre-loaded with available commands.
func NewCmdPalette() *CmdPalette {
	items := make([]CmdBarItem, len(CmdBarItems))
	copy(items, CmdBarItems)
	return &CmdPalette{items: items}
}

// Filter narrows the displayed list to commands matching the typed prefix.
func (p *CmdPalette) Filter(prefix string) {
	if prefix == "" {
		p.filtered = make([]CmdBarItem, len(p.items))
		copy(p.filtered, p.items)
		p.cursor = 0
		p.offset = 0
		return
	}

	p.filtered = nil
	lower := strings.ToLower(prefix)
	for _, item := range p.items {
		labelLower := strings.ToLower(item.Label)
		if strings.HasPrefix(labelLower, lower) {
			p.filtered = append(p.filtered, item)
		}
	}

	// Sort: exact match first, then alphabetical
	sort.SliceStable(p.filtered, func(i, j int) bool {
		li := strings.ToLower(p.filtered[i].Label)
		lj := strings.ToLower(p.filtered[j].Label)
		if li == lower {
			return true
		}
		if lj == lower {
			return false
		}
		return li < lj
	})

	if p.cursor >= len(p.filtered) {
		p.cursor = 0
	}
	if p.offset >= len(p.filtered) {
		p.offset = 0
	}
	// Ensure cursor is visible
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+paletteMaxVisible && len(p.filtered) > paletteMaxVisible {
		p.offset = p.cursor - paletteMaxVisible + 1
	}
}

// Selected returns the currently highlighted command label, or "".
func (p *CmdPalette) Selected() string {
	if len(p.filtered) == 0 {
		return ""
	}
	return p.filtered[p.cursor].Label
}

// CursorUp moves the selection up, scrolling the visible window if needed.
func (p *CmdPalette) CursorUp() {
	if len(p.filtered) == 0 {
		return
	}
	p.cursor--
	if p.cursor < 0 {
		p.cursor = len(p.filtered) - 1
		// Jump offset to show the last items
		if len(p.filtered) > paletteMaxVisible {
			p.offset = len(p.filtered) - paletteMaxVisible
		} else {
			p.offset = 0
		}
		return
	}
	// Scroll offset up if cursor moves above the visible window
	if p.cursor < p.offset && p.offset > 0 {
		p.offset--
	}
}

// CursorDown moves the selection down, scrolling the visible window if needed.
func (p *CmdPalette) CursorDown() {
	if len(p.filtered) == 0 {
		return
	}
	p.cursor++
	if p.cursor >= len(p.filtered) {
		p.cursor = 0
		p.offset = 0
		return
	}
	// Scroll offset down if cursor moves below the visible window
	if p.cursor >= p.offset+paletteMaxVisible {
		p.offset++
	}
}

// Render builds a floating overlay box showing matching commands.
func (p *CmdPalette) Render(width int) string {
	if len(p.filtered) == 0 {
		return ""
	}

	// Inner width for text (padding + border = 4 chars)
	innerW := width - 4
	if innerW < 20 {
		innerW = 20
	}

	// Build visible slice
	visible := p.filtered
	hasMore := false
	moreCount := 0
	if len(p.filtered) > paletteMaxVisible {
		end := p.offset + paletteMaxVisible
		if end > len(p.filtered) {
			end = len(p.filtered)
		}
		visible = p.filtered[p.offset:end]
		hasMore = p.offset+paletteMaxVisible < len(p.filtered)
		moreCount = len(p.filtered) - end
	}

	descW := innerW / 3
	if descW > 30 {
		descW = 30
	}
	cmdW := innerW - descW - 2

	// Build item lines
	var lines []string
	for i, item := range visible {
		cmd := item.Label
		if len(cmd) > cmdW {
			cmd = cmd[:cmdW-1] + "…"
		}
		desc := item.Desc
		if len(desc) > descW {
			desc = desc[:descW-1] + "…"
		}

		absIdx := p.offset + i // absolute index in filtered
		padded := lipgloss.NewStyle().Width(innerW)
		if absIdx == p.cursor {
			mark := lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Render("▸")
			cmdStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true).Render(cmd)
			descStyled := dimmedStyle.Render(desc)
			line := lipgloss.JoinHorizontal(lipgloss.Left, mark+" ", cmdStyled, " ", descStyled)
			lines = append(lines, padded.Render(line))
		} else {
			cmdStyled := lipgloss.NewStyle().Render(cmd)
			descStyled := dimmedStyle.Render(desc)
			line := lipgloss.JoinHorizontal(lipgloss.Left, "  ", cmdStyled, " ", descStyled)
			lines = append(lines, padded.Render(line))
		}
	}

	// "N more" indicator if there are items below the visible window
	if hasMore && moreCount > 0 {
		lines = append(lines, dimmedStyle.Render(fmt.Sprintf("  … %d more", moreCount)))
	}

	content := strings.Join(lines, "\n")

	paletteStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3B3B3B")).
		Padding(0, 1).
		Width(width)

	return paletteStyle.Render(content)
}
