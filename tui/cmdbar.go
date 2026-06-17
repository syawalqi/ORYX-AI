package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/syawalqi/oryx/agent"
	"github.com/syawalqi/oryx/executor"
	"github.com/syawalqi/oryx/memory"
)

// planEventMsg carries a plan execution event for the TUI.
type planEventMsg struct {
	event agent.PlanEvent
}

// CmdBarItem describes a slash command shown in the palette and command bar.
type CmdBarItem struct {
	Label string
	Desc  string
}

// CmdBarItems lists all available slash commands.
var CmdBarItems = []CmdBarItem{
	{Label: "/clear", Desc: "clear chat history"},
	{Label: "/config", Desc: "view or edit config (/config edit)"},
	{Label: "/memory", Desc: "view or edit memory (/memory edit)"},
	{Label: "/model", Desc: "switch AI model (/model <name>)"},
	{Label: "/plan", Desc: "toggle plan mode (read-only)"},
	{Label: "/save", Desc: "save conversation to state DB"},
	{Label: "/load", Desc: "load a saved conversation"},
	{Label: "/scan", Desc: "rescan server and update context"},
	{Label: "/update", Desc: "self-update ORYX (--dev, --force)"},
	{Label: "/help", Desc: "show key bindings"},
	{Label: "/exit", Desc: "quit ORYX"},
}

// RenderCmdBar builds the footer command bar showing available shortcuts.
func RenderCmdBar(width int) string {
	items := []string{
		"Ctrl+P plan",
		"Ctrl+R reasoning",
		"Ctrl+T tools",
		"Ctrl+S sidebar",
		"/ help",
	}
	sep := dimmedStyle.Render(" • ")
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Render(dimmedStyle.Render(" " + strings.Join(items, sep)))
}

// handlePaletteKey processes keys when the command palette is active.
func (m *Model) handlePaletteKey(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "up":
		m.palette.CursorUp()
		return m, nil
	case "down":
		m.palette.CursorDown()
		return m, nil
	case "enter":
		selected := m.palette.Selected()
		if selected != "" {
			// Execute the selected command immediately
			m.input = ""
			m.palette.Filter("")
			m.updateViewport()
			return m, m.handleCommand(selected)
		}
		// Nothing selected — execute input as-is
		input := strings.TrimSpace(m.input)
		m.input = ""
		m.palette.Filter("")
		m.updateViewport()
		if input != "" {
			return m, m.handleCommand(input)
		}
		return m, nil
	case "tab":
		selected := m.palette.Selected()
		if selected != "" {
			m.input = selected + " "
			m.palette.Filter(m.input)
			m.updateViewport()
		}
		return m, nil
	case "esc":
		m.input = ""
		m.palette.Filter("")
		m.updateViewport()
		return m, nil
	case "backspace":
		if len(m.input) > 1 {
			m.input = m.input[:len(m.input)-1]
			m.palette.Filter(strings.TrimPrefix(m.input, "/"))
			m.updateViewport()
		} else {
			// Only "/" left — clear entirely
			m.input = ""
			m.palette.Filter("")
			m.updateViewport()
		}
		return m, nil
	default:
		// Regular character — append and refilter
		if len(key) == 1 {
			m.input += key
			m.palette.Filter(strings.TrimPrefix(m.input, "/"))
			m.updateViewport()
		}
		return m, nil
	}
}

// handleCommand processes a slash command and returns a tea.Cmd for side effects.
func (m *Model) handleCommand(input string) tea.Cmd {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case "/exit", "/quit":
		return tea.Quit

	case "/clear":
		m.messages = nil
		m.history = nil
		m.updateViewport()
		return nil

	case "/help":
		m.messages = append(m.messages, ChatMessage{
			Role: "assistant",
			Content: "Commands:\n" +
				"  /clear           Clear chat\n" +
				"  /config          Show current config\n" +
				"  /config edit     Edit config in nano/vim\n" +
				"  /memory          Show server context (memory.md)\n" +
				"  /memory edit     Edit memory in nano/vim\n" +
				"  /model <id>      Change model\n" +
				"  /plan            Toggle plan mode (read-only analysis)\n" +
				"  /save            Save conversation to state DB\n" +
				"  /load            Load a saved conversation\n" +
				"  /scan            Rescan server and update memory\n" +
				"  /update          Check for updates\n" +
				"  /help            Show this help\n" +
				"  /exit            Exit\n\n" +
				"Keys:\n" +
				"  ↑/↓      Scroll\n" +
				"  PgUp/Dn  Page scroll\n" +
				"  Ctrl+R    Toggle reasoning expand/collapse\n" +
				"  Ctrl+T    Toggle tool output expand/collapse\n" +
				"  Ctrl+S    Toggle sidebar\n" +
				"  Ctrl+P    Toggle plan mode\n" +
				"  Enter    Send message",
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return nil

	case "/config":
		if len(parts) > 1 && parts[1] == "edit" {
			return m.editFile(m.configPath)
		}
		return m.showConfig()

	case "/memory":
		if len(parts) > 1 && parts[1] == "edit" {
			return m.editFile(m.memoryPath)
		}
		return m.showMemory()

	case "/model":
		return m.changeModel(parts)

	case "/scan":
		return m.rescanServer()

	case "/plan":
		m.planMode = !m.planMode
		m.header.PlanMode = m.planMode
		m.updateViewport()
		return nil

	case "/skill":
		m.messages = append(m.messages, ChatMessage{
			Role: "assistant",
			Content: "ORYX's built-in abilities:\n" +
				"  • Run shell commands on the server\n" +
				"  • Read/write files\n" +
				"  • Manage systemd services\n" +
				"  • Search journal logs\n" +
				"  • Health monitoring (daemon mode)\n" +
				"  • Webhook alerts\n" +
				"  • Plan-and-Execute mode\n" +
				"  • MCP tool support",
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return nil

	case "/update":
		force := false
		trackOverride := ""
		for _, arg := range parts[1:] {
			switch arg {
			case "--force", "-f":
				force = true
			case "--dev":
				trackOverride = "dev"
			case "--stable":
				trackOverride = "stable"
			}
		}
		m.messages = append(m.messages, ChatMessage{Role: "assistant", Content: "⏳ Updating ORYX..."})
		m.updateViewport()
		m.viewport.GotoBottom()
		return m.runUpdate(force, trackOverride)

	case "/save":
		return m.saveConversation()

	case "/load":
		return m.loadConversationList()

	default:
		m.messages = append(m.messages, ChatMessage{
			Role:    "error",
			Content: fmt.Sprintf("Unknown command: %s (type /help for available commands)", cmd),
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return nil
	}
}

// editFile opens a file in the user's editor (nano/vim) and reloads it.
func (m *Model) editFile(path string) tea.Cmd {
	return func() tea.Msg {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "nano"
		}
		cmd := exec.Command(editor, path)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		err := cmd.Run()
		return editorFinishedMsg{path: path, err: err}
	}
}

// showConfig displays the current config file contents.
func (m *Model) showConfig() tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(m.configPath)
		if err != nil {
			return editorFinishedMsg{path: m.configPath, err: err}
		}
		content := string(data)
		if len(content) > 3000 {
			content = content[:3000] + "\n... (truncated)"
		}
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("📄 Config (%s):\n```\n%s\n```", m.configPath, content),
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return editorFinishedMsg{path: m.configPath, err: nil}
	}
}

// showMemory displays the current memory.md contents.
func (m *Model) showMemory() tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(m.memoryPath)
		if err != nil {
			return editorFinishedMsg{path: m.memoryPath, err: err}
		}
		content := string(data)
		if len(content) > 3000 {
			content = content[:3000] + "\n... (truncated)"
		}
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("🧠 Server Context (%s):\n```\n%s\n```", m.memoryPath, content),
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return editorFinishedMsg{path: m.memoryPath, err: nil}
	}
}

// changeModel switches the LLM model.
func (m *Model) changeModel(parts []string) tea.Cmd {
	return func() tea.Msg {
		if len(parts) < 2 {
			m.messages = append(m.messages, ChatMessage{
				Role:    "assistant",
				Content: fmt.Sprintf("Current model: %s\nUsage: /model <model-id>", m.agent.ModelName()),
			})
			m.updateViewport()
			m.viewport.GotoBottom()
			return editorFinishedMsg{err: nil}
		}
		newModel := parts[1]
		m.agent.SetModel(newModel)
		m.header.Model = newModel
		m.sidebarData.ModelName = newModel
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("✅ Model changed to: %s", newModel),
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return editorFinishedMsg{err: nil}
	}
}

// rescanServer triggers a background system scan.
func (m *Model) rescanServer() tea.Cmd {
	return func() tea.Msg {
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: "🔍 Scanning server...",
		})
		m.updateViewport()
		m.viewport.GotoBottom()

		exec := executor.New(30, 200, []string{})
		if info, err := memory.Scan(context.Background(), exec); err == nil {
			content := info.Render()
			os.WriteFile(m.memoryPath, []byte(content), 0644)
			m.sidebarData.UpdateFromScan(info)
			m.messages = append(m.messages, ChatMessage{
				Role:    "assistant",
				Content: fmt.Sprintf("✅ Server scan complete. Memory updated (%d bytes).", len(content)),
			})
		} else {
			m.messages = append(m.messages, ChatMessage{
				Role:    "error",
				Content: fmt.Sprintf("Scan failed: %v", err),
			})
		}
		m.updateViewport()
		m.viewport.GotoBottom()
		return editorFinishedMsg{err: nil}
	}
}
