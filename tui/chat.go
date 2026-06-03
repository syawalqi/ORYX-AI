package tui

import (
	"fmt"
	"math"
	"strings"
)

type ChatMessage struct {
	Role      string   // "user", "assistant", "tool", "error"
	Content   string
	Reasoning string   // reasoning/thinking content (for assistant messages only)
	ToolCalls []string // compact tool call log (for assistant messages only)
}

// renderMessages renders all chat messages plus optional streaming content.
// width is the available viewport width for the chat area.
func renderMessages(messages []ChatMessage, streamContent, streamReasoning string,
	streamMsgs []string, width int, expandReasoning, expandTools bool, showLogo bool, pupilPhase float64) string {

	var b strings.Builder

	if showLogo && len(messages) == 0 {
		b.WriteString(renderEye(width, pupilPhase))
		return b.String()
	}

	if len(messages) == 0 && streamContent == "" && streamReasoning == "" && len(streamMsgs) == 0 {
		return dimmedStyle.Render("Send a message to start.\n")
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			b.WriteString(renderUserBox(msg.Content, width))
		case "assistant":
			b.WriteString(assistantMsgStyle.Render("Flare:") + "\n")
			b.WriteString(assistantContentStyle.Render(msg.Content) + "\n")
			if msg.Reasoning != "" {
				b.WriteString(renderReasoningBlock(msg.Reasoning, width, expandReasoning))
			}
			if len(msg.ToolCalls) > 0 {
				b.WriteString(renderToolCallsBlock(msg.ToolCalls, width, expandTools))
			}
			b.WriteString("\n")
		case "tool":
			renderToolBlock(&b, msg.Content, width, expandTools)
		case "error":
			b.WriteString(errorStyle.Render("⚠ " + msg.Content + "\n"))
		}
	}

	// During streaming — reasoning
	if streamReasoning != "" {
		b.WriteString(renderReasoningBlock(streamReasoning, width, expandReasoning))
	}

	// During streaming — tool call indicator
	for _, line := range streamMsgs {
		b.WriteString(toolMsgStyle.Render(line) + "\n")
	}

	// During streaming — content
	if streamContent != "" {
		b.WriteString(assistantMsgStyle.Render("Flare:") + "\n")
		b.WriteString(assistantContentStyle.Render(streamContent))
	}

	return b.String()
}

// --- User message box (dynamic width) ---

func renderUserBox(content string, width int) string {
	left1 := "╔═ You "
	right1 := "╗"
	fill1 := width - len(left1) - len(right1)
	if fill1 < 1 {
		fill1 = 1
	}
	top := userMsgStyle.Render(left1 + strings.Repeat("═", fill1) + right1)

	body := userContentStyle.Render(" " + content)

	left2 := "╚"
	right2 := "╝"
	fill2 := width - len(left2) - len(right2)
	if fill2 < 1 {
		fill2 = 1
	}
	bottom := userMsgStyle.Render(left2 + strings.Repeat("═", fill2) + right2)

	return top + "\n" + body + "\n" + bottom + "\n\n"
}

// --- Reasoning block (collapsible) ---

func renderReasoningBlock(reasoning string, width int, expanded bool) string {
	lines := strings.Split(strings.TrimRight(reasoning, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	n := len(lines)

	// Title line with +/- indicator
	var toggleInd string
	if expanded {
		toggleInd = "[-]"
	} else if n > 3 {
		toggleInd = "[+]"
	}

	left := "╔═ reasoning "
	right := "╗ " + toggleInd
	fill := width - len(left) - len(right)
	if fill < 1 {
		fill = 1
	}
	b.WriteString(thoughtStyle.Render(left+strings.Repeat("═", fill)+right) + "\n")

	if expanded || n <= 3 {
		// Show all lines, truncated to fit
		maxLine := width - 5
		if maxLine < 10 {
			maxLine = 10
		}
		for _, line := range lines {
			runes := []rune(line)
			if len(runes) > maxLine {
				runes = append(runes[:maxLine-3], []rune("...")...)
			}
			b.WriteString(thoughtStyle.Render("║ "+string(runes))+"\n")
		}
	} else {
		// Show first 3 lines + "...", truncated to fit
		maxLine := width - 5
		if maxLine < 10 {
			maxLine = 10
		}
		for i := 0; i < 3 && i < n; i++ {
			runes := []rune(lines[i])
			if len(runes) > maxLine {
				runes = append(runes[:maxLine-3], []rune("...")...)
			}
			b.WriteString(thoughtStyle.Render("║ "+string(runes))+"\n")
		}
		b.WriteString(thoughtStyle.Render(fmt.Sprintf("║ ... (%d more lines)", n-3)) + "\n")
	}

	b.WriteString(thoughtStyle.Render("╚"+strings.Repeat("═", width-2)+"╝") + "\n")
	return b.String()
}

// --- Tool output block (truncated, collapsible) ---

func renderToolBlock(b *strings.Builder, content string, width int, expanded bool) {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) == 0 {
		return
	}

	n := len(lines)
	maxLines := 8
	showAll := expanded || n <= maxLines

	// Header
	b.WriteString(dimmedStyle.Render("  Script Output") + "\n")

	if showAll {
		for _, line := range lines {
			b.WriteString(dimmedStyle.Render("  │ "+line) + "\n")
		}
	} else {
		for i := 0; i < maxLines; i++ {
			b.WriteString(dimmedStyle.Render("  │ "+lines[i]) + "\n")
		}
		remaining := n - maxLines
		if remaining > 0 {
			b.WriteString(dimmedStyle.Render(fmt.Sprintf("  │ ... (%d more lines) [t to expand]", remaining)) + "\n")
		}
	}
	b.WriteString("\n")
}

// --- Tool call history block (collapsible) ---

func renderToolCallsBlock(toolCalls []string, width int, expanded bool) string {
	if len(toolCalls) == 0 {
		return ""
	}
	var b strings.Builder
	n := len(toolCalls)

	var toggleInd string
	if expanded {
		toggleInd = "[-]"
	} else if n > 3 {
		toggleInd = "[+]"
	}

	left := "╔═ tools "
	right := "╗ " + toggleInd
	fill := width - len(left) - len(right)
	if fill < 1 {
		fill = 1
	}
	b.WriteString(toolMsgStyle.Render(left+strings.Repeat("═", fill)+right) + "\n")

	if expanded || n <= 3 {
		for _, line := range toolCalls {
			b.WriteString(toolMsgStyle.Render("║ "+line) + "\n")
		}
	} else {
		for i := 0; i < 3; i++ {
			b.WriteString(toolMsgStyle.Render("║ "+toolCalls[i]) + "\n")
		}
		b.WriteString(toolMsgStyle.Render(fmt.Sprintf("║ ... (%d more calls)", n-3)) + "\n")
	}

	b.WriteString(toolMsgStyle.Render("╚"+strings.Repeat("═", width-2)+"╝") + "\n")
	return b.String()
}

// --- Startup logo (procedural eye animation) ---

var flareTagline = "Server Management AI Agent"

// renderEye generates a large eye with a moving iris+pupil.
// phase ranges 0.0 (looking left) to 1.0 (looking right).
// The entire iris circle shifts as one unit — the pupil moves with the iris.
func renderEye(width int, phase float64) string {
	// Interior dimensions (between the ║ borders)
	iw := 46 // interior width
	ih := 15 // interior height (16 including border)

	// Iris circle parameters
	radius := 7.0
	cy := ih / 2 // iris center Y (middle of interior)
	cxMin := int(radius) + 1
	cxMax := iw - int(radius) - 1
	cxRange := cxMax - cxMin
	if cxRange < 1 {
		cxRange = 1
	}

	// Clamp phase to [0.0, 1.0]
	if phase < 0.0 {
		phase = 0.0
	}
	if phase > 1.0 {
		phase = 1.0
	}

	// Iris center X — smoothly interpolated
	cx := cxMin + int(phase*float64(cxRange))

	// --- Build the interior grid ---
	grid := make([][]rune, ih)
	for y := 0; y < ih; y++ {
		grid[y] = make([]rune, iw)
		for x := 0; x < iw; x++ {
			grid[y][x] = '░' // sclera fill (light shade)
		}
	}

	// Draw the iris as a filled circle at (cx, cy)
	// Gradient: ● pupil → █ dark ring → ▓ iris → ▒ edge → ░ sclera
	bbox := int(radius) + 1
	for dy := -bbox; dy <= bbox; dy++ {
		for dx := -bbox; dx <= bbox; dx++ {
			dist := math.Sqrt(float64(dx*dx + dy*dy))
			px := cx + dx
			py := cy + dy
			if px >= 0 && px < iw && py >= 0 && py < ih {
				switch {
				case dist <= 0.5:
					grid[py][px] = '●' // single-cell pupil
				case dist <= 3.5:
					grid[py][px] = '█' // dark ring around pupil
				case dist <= radius - 0.3:
					grid[py][px] = '▓' // outer iris body
				case dist <= radius + 0.5:
					grid[py][px] = '▒' // soft edge transition to sclera
				}
			}
		}
	}

	// --- Build the bordered output ---
	var b strings.Builder
	b.WriteString("\n")

	outerW := iw + 2 // +2 for left/right ║ chars
	pad := (width - outerW) / 2
	if pad < 0 {
		pad = 0
	}

	// Top border
	b.WriteString(strings.Repeat(" ", pad) +
		assistantContentStyle.Render("╔"+strings.Repeat("═", iw)+"╗") + "\n")

	// Content rows with side borders
	for y := 0; y < ih; y++ {
		line := string(grid[y])
		b.WriteString(strings.Repeat(" ", pad) +
			assistantContentStyle.Render("║"+line+"║") + "\n")
	}

	// Bottom border
	b.WriteString(strings.Repeat(" ", pad) +
		assistantContentStyle.Render("╚"+strings.Repeat("═", iw)+"╝") + "\n\n")

	// Tagline centered
	tagPad := (width - len(flareTagline)) / 2
	if tagPad < 0 {
		tagPad = 0
	}
	b.WriteString(strings.Repeat(" ", tagPad) + dimmedStyle.Render(flareTagline) + "\n")

	// Hint
	hint := "Send a message to start."
	hintPad := (width - len(hint)) / 2
	if hintPad < 0 {
		hintPad = 0
	}
	b.WriteString(strings.Repeat(" ", hintPad) + dimmedStyle.Render(hint) + "\n")

	return b.String()
}
