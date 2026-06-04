package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/syawalqi/flare/agent"
	"github.com/syawalqi/flare/config"
	"github.com/syawalqi/flare/executor"
	"github.com/syawalqi/flare/llm"
	"github.com/syawalqi/flare/memory"
)

type streamResultMsg struct {
	result agent.StreamResult
}

type editorFinishedMsg struct {
	path string
	err  error
}

type model struct {
	ready    bool
	viewport viewport.Model
	input    string
	messages []ChatMessage
	header   HeaderData

	agent        *agent.Agent
	systemPrompt string
	configPath   string
	memoryPath   string
	configDir    string

	width  int
	height int

	// Expand/collapse toggles
	expandReasoning bool
	expandTools     bool

	// Startup logo state (static, shown once)
	showLogo bool

	// Streaming state
	loading         bool
	streamCh        <-chan agent.StreamResult
	streamContent   strings.Builder
	streamReasoning strings.Builder // accumulated reasoning text
	streamMsgs      []string        // lines emitted during streaming (tool calls shown here)
	streamToolRun   bool            // currently executing a tool

	// Spinner
	spinner spinner.Model

	// Command palette
	palette *CmdPalette

	// Sidebar
	sidebarData  SidebarData
	showSidebar  bool
	sidebarWidth int

	// Block regions for mouse click detection
	blockRegions []BlockRegion

	// Plan mode — read-only analysis mode with green theme
	planMode bool
}

// NewModel creates the chat TUI model.
func NewModel(ag *agent.Agent, systemPrompt, configPath, memoryPath, configDir string) tea.Model {
	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))
	s.Spinner = spinner.Line

	return &model{
		agent:        ag,
		systemPrompt: systemPrompt,
		configPath:   configPath,
		memoryPath:   memoryPath,
		configDir:    configDir,
		header: HeaderData{
			Model: ag.ModelName(),
		},
		spinner: s,
		palette: NewCmdPalette(),
		sidebarData: SidebarData{
			ModelName: ag.ModelName(),
		},
		showSidebar: true,
		showLogo:    true,
	}
}

func (m *model) Init() tea.Cmd {
	// Disable terminal flow control so Ctrl+S (sidebar toggle) isn't intercepted
	exec.Command("stty", "-ixon").Run()
	return m.spinner.Tick
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		chatW := msg.Width - 4 // 2 for border, 2 for padding
		if chatW < 40 {
			chatW = 40
		}
		m.sidebarWidth = 28 // always allocate for header display

		if !m.ready {
			m.viewport = viewport.New(chatW, msg.Height-5)
			m.viewport.YPosition = 2 // header(1) + border top(1)
			m.ready = true
		} else {
			m.viewport.Width = chatW
			m.viewport.Height = msg.Height - 5
		}
		m.updateViewport()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		// Also advance spinner when logo is showing (background animation)
		if !m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case streamResultMsg:
		return m.handleStreamResult(msg.result)

	case editorFinishedMsg:
		m.loading = false
		if msg.err != nil {
			m.messages = append(m.messages, ChatMessage{
				Role: "error", Content: fmt.Sprintf("edit cancelled or failed: %v", msg.err),
			})
		} else {
			data, _ := os.ReadFile(msg.path)
			label := "Config"
			if strings.Contains(msg.path, "memory") {
				label = "Memory"
			}
			content := string(data)
			if len(content) > 3000 {
				content = content[:3000] + "\n... (truncated)"
			}
			m.messages = append(m.messages, ChatMessage{
				Role: "assistant", Content: fmt.Sprintf("✏️ %s saved (%d bytes).\n```\n%s\n```", label, len(data), content),
			})
		}
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil

	case tea.MouseMsg:
		// Left-click on a block header toggles expand/collapse
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && !m.loading {
			contentLine := msg.Y - m.viewport.YPosition + m.viewport.YOffset
			for _, region := range m.blockRegions {
				if region.ContentLine == contentLine {
					switch region.BlockType {
					case "reasoning":
						m.expandReasoning = !m.expandReasoning
					case "tools":
						m.expandTools = !m.expandTools
					}
					m.updateViewport()
					return m, nil
				}
			}
		}
		// Forward mouse events (wheel scrolling etc.) to viewport
		if !m.loading {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// Forward to viewport when not loading (for smooth scrolling)
	if !m.loading {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) View() string {
	if !m.ready {
		return "Loading..."
	}

	m.header.PlanMode = m.planMode
	header := RenderHeader(m.header, m.width)
	cmdBar := RenderCmdBar(m.width)

	// Input line
	var inputLine string
	if m.loading {
		spinnerStr := fmt.Sprintf(" %s ", m.spinner.View())
		ls := loadingStyle
		if m.planMode {
			ls = planLoadingStyle
		}
		if m.streamToolRun {
			inputLine = ls.Render("● " + spinnerStr + "executing tool...")
		} else if m.streamContent.Len() > 0 {
			inputLine = ls.Render("● " + spinnerStr + "streaming...")
		} else if m.streamReasoning.Len() > 0 {
			inputLine = ls.Render("● " + spinnerStr + "reasoning...")
		} else {
			inputLine = ls.Render("● " + spinnerStr + "thinking...")
		}
	} else {
		promptColor := "#7C3AED"
		if m.planMode {
			promptColor = "#10B981"
		}
		prompt := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor)).Render("> ")
		cursor := dimmedStyle.Render("▎")
		inputLine = prompt + m.input + cursor
	}

	// Build the content inside the border
	chatContent := m.viewport.View()

	// Update header with sidebar data when visible
	if m.showSidebar {
		m.sidebarData.MessageCount = len(m.messages)
		m.header.ServerInfo = m.sidebarData.CompactString()
	} else {
		m.header.ServerInfo = ""
	}

	// Palette overlay inside the border
	if strings.HasPrefix(m.input, "/") && !m.loading {
		paletteView := m.palette.Render(m.width - 4)
		if paletteView != "" {
			chatContent = lipgloss.JoinVertical(lipgloss.Left, chatContent, paletteView)
		}
	}

	// Wrap in the double border
	bs := chatBorderStyle
	if m.planMode {
		bs = planBorderStyle
	}
	body := bs.Render(chatContent)

	footer := lipgloss.JoinVertical(lipgloss.Left, inputLine, cmdBar)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// --- key handling ---

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global: quit
	if key == "ctrl+c" || key == "ctrl+d" {
		return m, tea.Quit
	}

	// During streaming: ctrl+c cancels, scroll keys still work
	if m.loading {
		if key == "ctrl+c" {
			m.loading = false
			m.streamCh = nil
			m.finalizeStream()
			m.updateViewport()
			return m, nil
		}
		// Allow scrolling during streaming
		switch key {
		case "up", "down", "pgup", "pgdown", "home", "end":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil // all other keys blocked during streaming
	}

	// --- Command palette mode (input starts with "/") ---
	// Must come before viewport scroll keys so up/down navigate palette, not scroll
	if strings.HasPrefix(m.input, "/") && !m.loading {
		return m.handlePaletteKey(key, msg)
	}

	// Viewport scroll keys
	switch key {
	case "up", "down", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	// Toggle keys (ctrl+ combinations to avoid interfering with typing)
	if key == "ctrl+r" && !m.loading {
		m.expandReasoning = !m.expandReasoning
		m.updateViewport()
		return m, nil
	}
	if key == "ctrl+t" && !m.loading {
		m.expandTools = !m.expandTools
		m.updateViewport()
		return m, nil
	}

	// Toggle sidebar
	if key == "ctrl+s" && !m.loading {
		m.showSidebar = !m.showSidebar
		m.reflowViewport()
		return m, nil
	}

	// Toggle plan mode (Ctrl+P)
	if key == "ctrl+p" && !m.loading {
		m.togglePlanMode()
		return m, nil
	}

	// Enter — send message
	if key == "enter" {
		input := strings.TrimSpace(m.input)
		if input == "" {
			return m, nil
		}
		if strings.HasPrefix(input, "/") {
			m.input = ""
			return m, m.handleCommand(input)
		}
		m.input = ""
		return m, m.startStream(input)
	}

	// Backspace
	if key == "backspace" && len(m.input) > 0 {
		m.input = m.input[:len(m.input)-1]
		return m, nil
	}

	// Typed character
	if len(key) == 1 {
		m.input += key
		// Initialize palette immediately when "/" is first typed
		if m.input == "/" {
			m.palette.Filter("")
		}
	}

	return m, nil
}

// --- streaming ---

func (m *model) startStream(input string) tea.Cmd {
	m.loading = true
	m.streamContent.Reset()
	m.streamReasoning.Reset()
	m.streamMsgs = nil
	m.streamToolRun = false
	m.showLogo = false // hide startup logo after first message

	ctx := context.Background()
	userMsg := llm.Message{Role: llm.RoleUser, Content: input}

	// Add user message to display
	m.messages = append(m.messages, ChatMessage{Role: "user", Content: input})

	// Start streaming in background
	prompt := m.systemPrompt
	if m.planMode {
		prompt += "\n\n⚠️ PLAN MODE ACTIVE — You can ONLY read files (read_file) and search logs (search_logs). " +
			"You CANNOT run commands, write files, or modify services. " +
			"Do NOT attempt destructive actions — they will be blocked. Analyze and suggest changes only."
	}
	ch, err := m.agent.RunStream(ctx, prompt, []llm.Message{userMsg})
	if err != nil {
		m.loading = false
		m.messages = append(m.messages, ChatMessage{Role: "error", Content: fmt.Sprintf("stream error: %v", err)})
		m.updateViewport()
		return nil
	}
	m.streamCh = ch

	return m.pollStream()
}

func (m *model) pollStream() tea.Cmd {
	return func() tea.Msg {
		result, ok := <-m.streamCh
		if !ok {
			return streamResultMsg{result: agent.StreamResult{Done: true}}
		}
		return streamResultMsg{result: result}
	}
}

func (m *model) handleStreamResult(result agent.StreamResult) (tea.Model, tea.Cmd) {
	if result.Err != nil {
		m.loading = false
		m.streamCh = nil
		m.messages = append(m.messages, ChatMessage{Role: "error", Content: fmt.Sprintf("error: %v", result.Err)})
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil
	}

	if result.Token != "" {
		m.streamContent.WriteString(result.Token)
		m.streamToolRun = false
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, tea.Batch(m.pollStream(), m.spinner.Tick)
	}

	if result.Reasoning != "" {
		m.streamReasoning.WriteString(result.Reasoning)
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, tea.Batch(m.pollStream(), m.spinner.Tick)
	}

	if result.ToolResult != nil {
		m.streamToolRun = true
		// One-line compact summary per tool result
		tr := result.ToolResult
		outputLines := strings.Split(strings.TrimRight(tr.Output, "\n"), "\n")
		n := len(outputLines)
		// Extract duration/exit code from first line if present
		summary := fmt.Sprintf("  ◈ %s ✓", tr.Name)
		for _, l := range outputLines {
			if strings.HasPrefix(l, "exit code:") || strings.HasPrefix(l, "duration:") {
				summary += " " + strings.TrimSpace(l)
			}
		}
		summary += fmt.Sprintf(" — %d lines", n)
		m.streamMsgs = append(m.streamMsgs, summary)
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, tea.Batch(m.pollStream(), m.spinner.Tick)
	}

	if result.ToolCalls != nil && len(result.ToolCalls) > 0 {
		m.streamToolRun = true
		for _, tc := range result.ToolCalls {
			args := tc.Function.Arguments
			if len(args) > 60 {
				args = args[:60] + "…"
			}
			line := fmt.Sprintf("  ◈ %s(%s)", tc.Function.Name, args)
			m.streamMsgs = append(m.streamMsgs, line)
		}
		// Keep only last 3 tool entries in viewport during streaming
		if len(m.streamMsgs) > 3 {
			m.streamMsgs = m.streamMsgs[len(m.streamMsgs)-3:]
		}
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, m.pollStream()
	}

	if result.Done {
		m.loading = false
		m.streamCh = nil
		m.finalizeStream()
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil
	}

	// No relevant field — keep polling
	return m, m.pollStream()
}

func (m *model) finalizeStream() {
	content := m.streamContent.String()
	reasoning := m.streamReasoning.String()
	toolCalls := make([]string, len(m.streamMsgs))
	copy(toolCalls, m.streamMsgs)
	if content != "" || reasoning != "" || len(toolCalls) > 0 {
		m.messages = append(m.messages, ChatMessage{
			Role: "assistant", Content: content, Reasoning: reasoning, ToolCalls: toolCalls,
		})
	}
	m.streamContent.Reset()
	m.streamReasoning.Reset()
	m.streamMsgs = nil
}

// --- command palette ---

func (m *model) handlePaletteKey(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "up":
		m.palette.CursorUp()
		return m, nil

	case "down":
		m.palette.CursorDown()
		return m, nil

	case "tab":
		if sel := m.palette.Selected(); sel != "" {
			m.input = sel + " "
			m.palette.Filter("")
		}
		return m, nil

	case "enter":
		if sel := m.palette.Selected(); sel != "" {
			// Execute the selected command immediately
			m.input = ""
			m.palette.Filter("")
			return m, m.handleCommand(sel)
		}
		// Nothing selected — execute input as-is
		input := strings.TrimSpace(m.input)
		m.input = ""
		if input != "" {
			return m, m.handleCommand(input)
		}
		return m, nil

	case "esc":
		m.input = ""
		return m, nil

	case "backspace":
		if len(m.input) > 1 {
			m.input = m.input[:len(m.input)-1]
			m.palette.Filter(strings.TrimPrefix(m.input, "/"))
		} else {
			// Only "/" left — clear entirely
			m.input = ""
		}
		return m, nil

	default:
		// Regular character — append and refilter
		if len(key) == 1 {
			m.input += key
			m.palette.Filter(strings.TrimPrefix(m.input, "/"))
		}
		return m, nil
	}
}

// --- commands ---

func (m *model) handleCommand(input string) tea.Cmd {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case "/quit":
		return tea.Quit

	case "/clear":
		m.messages = nil
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
				"  /model <id>      Change model (e.g. /model deepseek/deepseek-v4-flash)\n" +
				"  /plan            Toggle plan mode (read-only analysis)\n" +
				"  /save            Save conversation to /tmp/\n" +
				"  /scan            Rescan server and update memory\n" +
				"  /skill           List FLARE's built-in abilities\n" +
				"  /help            Show this help\n" +
				"  /quit            Exit\n" +
				"  /update          Check for updates\n\n" +
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
		m.togglePlanMode()
		return nil

	case "/skill":
		m.messages = append(m.messages, ChatMessage{
			Role: "assistant",
			Content: "FLARE's built-in abilities:\n" +
				"  • Run shell commands on the server\n" +
				"  • Read/write files\n" +
				"  • Manage systemd services\n" +
				"  • Search journal logs\n" +
				"  • Health monitoring (daemon mode)\n" +
				"  • Webhook alerts\n\n" +
				"New abilities can be added to the flare-ultimate skill in Hermes.",
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return nil

	case "/update":
		return m.selfUpdate()

	case "/save":
		return m.saveConversation()

	default:
		m.messages = append(m.messages, ChatMessage{
			Role:    "error",
			Content: fmt.Sprintf("unknown command: %s", cmd),
		})
		m.updateViewport()
		return nil
	}
}

func (m *model) showConfig() tea.Cmd {
	cfg, err := config.Load(m.configPath)
	if err != nil {
		m.messages = append(m.messages, ChatMessage{
			Role: "error", Content: fmt.Sprintf("read config: %v", err),
		})
		m.updateViewport()
		return nil
	}
	masked := cfg.APIKey
	if len(masked) > 8 {
		masked = masked[:4] + "…" + masked[len(masked)-4:]
	} else if masked != "" {
		masked = "***"
	}
	m.messages = append(m.messages, ChatMessage{
		Role: "assistant",
		Content: fmt.Sprintf("📋 Config summary:\n  Provider: %s\n  Model: %s\n  API Key: %s\n  Max iterations: %d\n\nEdit with `/config edit` to change values.", cfg.Provider, cfg.Model, masked, cfg.Agent.MaxIterations),
	})
	m.updateViewport()
	m.viewport.GotoBottom()
	return nil
}

func (m *model) showMemory() tea.Cmd {
	data, err := os.ReadFile(m.memoryPath)
	if err != nil {
		m.messages = append(m.messages, ChatMessage{
			Role: "error", Content: fmt.Sprintf("read memory: %v", err),
		})
		m.updateViewport()
		return nil
	}

	size := len(data)

	m.messages = append(m.messages, ChatMessage{
		Role: "assistant",
		Content: fmt.Sprintf("🧠 Server memory: %d bytes\nEdit with `/memory edit` to update server context.", size),
	})
	m.updateViewport()
	m.viewport.GotoBottom()
	return nil
}

func (m *model) changeModel(parts []string) tea.Cmd {
	if len(parts) < 2 {
		// Show current model
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("🤖 Current model: **%s**\nUsage: `/model <model-id>`\nExample: `/model deepseek/deepseek-v4-flash`", m.agent.ModelName()),
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return nil
	}

	newModel := parts[1]

	// Update config file
	cfg, err := config.Load(m.configPath)
	if err != nil {
		m.messages = append(m.messages, ChatMessage{
			Role: "error", Content: fmt.Sprintf("load config: %v", err),
		})
		m.updateViewport()
		return nil
	}
	cfg.Model = newModel
	cfg.DaemonModel = newModel
	if err := cfg.Save(m.configPath); err != nil {
		m.messages = append(m.messages, ChatMessage{
			Role: "error", Content: fmt.Sprintf("save config: %v", err),
		})
		m.updateViewport()
		return nil
	}

	// Update agent and header
	m.agent.SetModel(newModel)
	m.header.Model = newModel
	m.sidebarData.ModelName = newModel

	m.messages = append(m.messages, ChatMessage{
		Role:    "assistant",
		Content: fmt.Sprintf("✓ Model changed to: %s\nNew chat sessions will use this model.", newModel),
	})
	m.updateViewport()
	m.viewport.GotoBottom()
	return nil
}

func (m *model) rescanServer() tea.Cmd {
	m.messages = append(m.messages, ChatMessage{
		Role:    "assistant",
		Content: "🔄 Scanning server... this may take a moment.",
	})
	m.updateViewport()

	exec := executor.New(30, 200, []string{})
	info, err := memory.Scan(context.Background(), exec)
	if err != nil {
		m.messages = append(m.messages, ChatMessage{
			Role: "error", Content: fmt.Sprintf("scan failed: %v", err),
		})
		m.updateViewport()
		return nil
	}

	content := info.Render()
	if err := os.WriteFile(m.memoryPath, []byte(content), 0644); err != nil {
		m.messages = append(m.messages, ChatMessage{
			Role: "error", Content: fmt.Sprintf("save memory: %v", err),
		})
		m.updateViewport()
		return nil
	}

	m.messages = append(m.messages, ChatMessage{
		Role:    "assistant",
		Content: fmt.Sprintf("✓ Server scan complete — %d bytes written to memory.", len(content)),
	})

	// Update sidebar data
	m.sidebarData.UpdateFromScan(info)

	m.updateViewport()
	m.viewport.GotoBottom()
	return nil
}

func (m *model) editFile(path string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nano"
	}

	m.messages = append(m.messages, ChatMessage{
		Role:    "assistant",
		Content: fmt.Sprintf("📝 Opening %s in %s...\nSave & exit to return.", path, editor),
	})
	m.loading = true
	m.updateViewport()

	c := exec.Command(editor, path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{path: path, err: err}
	})
}

func (m *model) selfUpdate() tea.Cmd {
	m.messages = append(m.messages, ChatMessage{
		Role:    "assistant",
		Content: "🔄 Flare self-update not yet implemented.\nSource at: github.com/syawalqi/flare",
	})
	m.updateViewport()
	m.viewport.GotoBottom()
	return nil
}

// --- rendering ---

func (m *model) updateViewport() {
	chatW := m.viewport.Width
	if chatW < 1 {
		chatW = m.width - 4
	}
	content := renderMessages(m.messages, m.streamContent.String(), m.streamReasoning.String(), m.streamMsgs, chatW, m.expandReasoning, m.expandTools, m.showLogo, m.viewport.Height)
	m.viewport.SetContent(content)
	m.blockRegions = detectBlockRegions(content)
}

func (m *model) reflowViewport() {
	chatW := m.width - 4
	if chatW < 40 {
		chatW = 40
	}
	m.sidebarWidth = 28
	m.viewport.Width = chatW
	m.viewport.Height = m.height - 5
	m.updateViewport()
}

// togglePlanMode switches between normal and read-only plan mode.
// The agent blocks destructive tools, the system prompt is updated,
// and the UI turns green.
func (m *model) togglePlanMode() {
	m.planMode = !m.planMode
	m.agent.SetPlanMode(m.planMode)

	label := "PLAN MODE (read-only)"
	if !m.planMode {
		label = "NORMAL MODE"
	}
	m.messages = append(m.messages, ChatMessage{
		Role:    "assistant",
		Content: fmt.Sprintf("🟢 Switched to **%s**", label),
	})
	m.updateViewport()
	m.viewport.GotoBottom()
}

// saveConversation writes the conversation to a timestamped file.
func (m *model) saveConversation() tea.Cmd {
	path := fmt.Sprintf("/tmp/flare-chat-%s.txt", time.Now().Format("20060102-150405"))
	var b strings.Builder
	for i, msg := range m.messages {
		b.WriteString(fmt.Sprintf("[%d] %s:\n", i+1, msg.Role))
		if msg.Content != "" {
			b.WriteString(msg.Content + "\n")
		}
		if msg.Reasoning != "" {
			b.WriteString("--- reasoning ---\n" + msg.Reasoning + "\n")
		}
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		m.messages = append(m.messages, ChatMessage{
			Role: "error", Content: fmt.Sprintf("save failed: %v", err),
		})
	} else {
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("💾 Conversation saved to `%s` (%d bytes)\nRead it with: `cat %s`", path, len(b.String()), path),
		})
	}
	m.updateViewport()
	m.viewport.GotoBottom()
	return nil
}
