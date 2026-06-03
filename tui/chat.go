package tui

import (
	"fmt"
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
	streamMsgs []string, width int, expandReasoning, expandTools bool, showLogo bool) string {

	var b strings.Builder

	if showLogo && len(messages) == 0 {
		b.WriteString(renderLogo(width))
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

// --- Startup logo ---

var flareLogo = "" +
	"     ██      ██\n" +
	"    ████    ████\n" +
	"   ██████  ██████\n" +
	"   ██████████████\n" +
	"    ████████████\n" +
	"     ██████████\n" +
	"      ████████\n" +
	"       ██████\n" +
	"        ████\n" +
	"         ██\n" +
	"\n" +
	"   ______ _               _____  ______\n" +
	"  |  ____| |        /\\   |  __ \\|  ____|\n" +
	"  | |__  | |       /  \\  | |__) | |__\n" +
	"  |  __| | |      / /\\ \\ |  _  /|  __|\n" +
	"  | |    | |____ / ____ \\| | \\ \\| |____\n" +
	"  |_|    |______/_/    \\_\\_|  \\_\\______|"

var flareTagline = "Server Management AI Agent"

func renderLogo(width int) string {
	var b strings.Builder

	logoLines := strings.Split(flareLogo, "\n")
	tagPadding := (width - len(flareTagline)) / 2
	if tagPadding < 0 {
		tagPadding = 0
	}

	// Center vertically with padding
	topPad := 3
	for i := 0; i < topPad; i++ {
		b.WriteString("\n")
	}

	// Render logo lines centered
	for _, line := range logoLines {
		trimmed := strings.TrimRight(line, " ")
		pad := (width - len(trimmed)) / 2
		if pad < 0 {
			pad = 0
		}
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(assistantContentStyle.Render(trimmed) + "\n")
	}

	// Tagline centered
	b.WriteString("\n")
	b.WriteString(strings.Repeat(" ", tagPadding))
	b.WriteString(dimmedStyle.Render(flareTagline) + "\n\n")

	// Bottom hint
	hint := "Send a message to start."
	hintPad := (width - len(hint)) / 2
	if hintPad < 0 {
		hintPad = 0
	}
	b.WriteString(strings.Repeat(" ", hintPad))
	b.WriteString(dimmedStyle.Render(hint) + "\n")

	return b.String()
}
