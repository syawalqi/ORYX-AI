package tui

import (
	"fmt"
	"strings"
)

// BlockRegion tracks where a collapsible block header appears in rendered content.
type BlockRegion struct {
	ContentLine int
	BlockType   string // "reasoning" or "tools"
}

// detectBlockRegions scans rendered content for collapsible block headers (╔═ reasoning / ╔═ tools).
func detectBlockRegions(content string) []BlockRegion {
	var regions []BlockRegion
	for i, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "╔═ reasoning ") {
			regions = append(regions, BlockRegion{ContentLine: i, BlockType: "reasoning"})
		} else if strings.Contains(line, "╔═ tools ") {
			regions = append(regions, BlockRegion{ContentLine: i, BlockType: "tools"})
		}
	}
	return regions
}

type ChatMessage struct {
	Role      string   // "user", "assistant", "tool", "error"
	Content   string
	Reasoning string   // reasoning/thinking content (for assistant messages only)
	ToolCalls []string // compact tool call log (for assistant messages only)
}

// renderMessages renders all chat messages plus optional streaming content.
// width is the available viewport width for the chat area.
func renderMessages(messages []ChatMessage, streamContent, streamReasoning string,
	streamMsgs []string, width int, expandReasoning, expandTools bool, showLogo bool, viewportHeight int) string {

	var b strings.Builder

	if showLogo && len(messages) == 0 {
		b.WriteString(renderEye(width, viewportHeight))
		return b.String()
	}

	if len(messages) == 0 && streamContent == "" && streamReasoning == "" && len(streamMsgs) == 0 {
		return dimmedStyle.Render("Send a message to start.\n")
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			b.WriteString(renderUserBox(wrapText(msg.Content, width-2), width))
		case "assistant":
			b.WriteString(assistantMsgStyle.Render("Flare:") + "\n")
			b.WriteString(assistantContentStyle.Render(wrapText(msg.Content, width)) + "\n")
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
		b.WriteString(assistantContentStyle.Render(wrapText(streamContent, width)))
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
		maxLine := width - 5
		if maxLine < 10 {
			maxLine = 10
		}
		for _, line := range lines {
			runes := []rune(line)
			for len(runes) > 0 {
				chunk := runes
				if len(chunk) > maxLine {
					chunk = chunk[:maxLine]
				}
				b.WriteString(thoughtStyle.Render("║ "+string(chunk)) + "\n")
				runes = runes[len(chunk):]
			}
		}
	} else {
		maxLine := width - 5
		if maxLine < 10 {
			maxLine = 10
		}
		for i := 0; i < 3 && i < n; i++ {
			runes := []rune(lines[i])
			for len(runes) > 0 {
				chunk := runes
				if len(chunk) > maxLine {
					chunk = chunk[:maxLine]
				}
				b.WriteString(thoughtStyle.Render("║ "+string(chunk)) + "\n")
				runes = runes[len(chunk):]
			}
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

// --- Startup logo (highly detailed static ASCII eye) ---

var flareTagline = "Flare - Server Management AI Agent"

// renderEye displays the user's custom ASCII art eclipse/petal design.
func renderEye(width int, viewportHeight int) string {
	art := []string{
		":            .:::.            :",
		"                    :::::          :--=--:          :::::        ",
		"                  :::::::         :-+***+-:         :::::::      ",
		"                ::::::::::       :=+*%@##+=:       ::::::::::        ",
		"               :::::::::::::     -=*%@@@#*+-     :::::::::::::      ",
		"               ---::::::::::::   -+#%@@@%#+-   ::::::::::::---       ",
		"               ---:::::::::::::  --+*@@@*+--  :::::::::::::---     ",
		"                ---::::::::::::: :=*-@@@-*=: :::::::::::::---        ",
		"                  --:::::::::::::::+**@**+::::::::::::::--      ",
		"                    --::::::::::.::-=+++=-::.:::::::::::--         ",
		"                       --:::::::::.:-----:.:::::::::--          ",
		"                           --:::::::#@#@#:::::::--                   ",
	}

	// Determine the max width of trimmed content
	maxContentW := 0
	trimmed := make([]string, len(art))
	for i, line := range art {
		trimmed[i] = strings.TrimSpace(line)
		if len(trimmed[i]) > maxContentW {
			maxContentW = len(trimmed[i])
		}
	}

	// Center each trimmed line within maxContentW (left-pad only)
	for i, line := range trimmed {
		if len(line) < maxContentW {
			left := (maxContentW - len(line)) / 2
			trimmed[i] = strings.Repeat(" ", left) + line
		}
	}

	var b strings.Builder

	// Vertical centering: art + blank + tagline + hint
	artLines := len(trimmed)
	totalArtHeight := artLines + 3 // art + blank line + tagline + hint
	topPad := (viewportHeight - totalArtHeight) / 2
	if topPad < 0 {
		topPad = 0
	}
	for i := 0; i < topPad; i++ {
		b.WriteString("\n")
	}

	pad := (width - maxContentW) / 2
	if pad < 0 {
		pad = 0
	}

	for _, line := range trimmed {
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(assistantContentStyle.Render(line) + "\n")
	}

	b.WriteString("\n")

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

// wrapText splits long lines at word boundaries so they don't overflow the viewport.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		runes := []rune(line)
		for len(runes) > 0 {
			// If the remaining text fits, write it as-is
			if len(runes) <= width {
				b.WriteString(string(runes))
				break
			}
			// Try to break at a space within the width
			breakAt := width
			for i := width; i > 0; i-- {
				if runes[i] == ' ' {
					breakAt = i
					break
				}
			}
			// If no space found, break at width (covers very long words)
			b.WriteString(string(runes[:breakAt]))
			b.WriteByte('\n')
			// Skip the space if we broke at one
			if breakAt < len(runes) && runes[breakAt] == ' ' {
				breakAt++
			}
			runes = runes[breakAt:]
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
