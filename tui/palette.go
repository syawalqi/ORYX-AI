package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CmdPalette provides a filtered, navigable list of slash commands.
type CmdPalette struct {
	items    []CmdBarItem
	filtered []CmdBarItem
	cursor   int
}

// NewCmdPalette creates a palette pre-loaded with available commands.
func NewCmdPalette() *CmdPalette {
	items := make([]CmdBarItem, len(CmdBarItems))
	copy(items, CmdBarItems)
	return &CmdPalette{items: items}
}

// Filter narrows the displayed list to commands matching the typed prefix.
// The prefix is everything after "/" and before a space (first word only).
func (p *CmdPalette) Filter(prefix string) {
	if prefix == "" {
		p.filtered = make([]CmdBarItem, len(p.items))
		copy(p.filtered, p.items)
		p.cursor = 0
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
}

// Selected returns the currently highlighted command label, or "".
func (p *CmdPalette) Selected() string {
	if len(p.filtered) == 0 {
		return ""
	}
	return p.filtered[p.cursor].Label
}

// CursorUp moves the selection up, wrapping.
func (p *CmdPalette) CursorUp() {
	if len(p.filtered) == 0 {
		return
	}
	p.cursor--
	if p.cursor < 0 {
		p.cursor = len(p.filtered) - 1
	}
}

// CursorDown moves the selection down, wrapping.
func (p *CmdPalette) CursorDown() {
	if len(p.filtered) == 0 {
		return
	}
	p.cursor++
	if p.cursor >= len(p.filtered) {
		p.cursor = 0
	}
}

// Render builds a floating overlay box showing matching commands.
// Returns empty string if there's nothing to show.
func (p *CmdPalette) Render(width int) string {
	if len(p.filtered) == 0 {
		return ""
	}

	// Inner width for text (padding + border = 4 chars)
	innerW := width - 4
	if innerW < 20 {
		innerW = 20
	}
	descW := innerW / 3
	if descW > 30 {
		descW = 30
	}
	cmdW := innerW - descW - 2

	// Build item lines
	var lines []string
	for i, item := range p.filtered {
		cmd := item.Label
		if len(cmd) > cmdW {
			cmd = cmd[:cmdW-1] + "…"
		}
		desc := item.Desc
		if len(desc) > descW {
			desc = desc[:descW-1] + "…"
		}

		padded := lipgloss.NewStyle().Width(innerW)
		if i == p.cursor {
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

	// Max 8 items visible
	maxVisible := 8
	if len(lines) > maxVisible {
		// Show first 7 + "… N more"
		remain := len(lines) - maxVisible + 1
		lines = lines[:maxVisible-1]
		lines = append(lines, dimmedStyle.Render(fmt.Sprintf("  … %d more", remain)))
	}

	content := strings.Join(lines, "\n")

	paletteStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3B3B3B")).
		Padding(0, 1).
		Width(width)

	return paletteStyle.Render(content)
}
