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
	"github.com/syawalqi/oryx/agent"
	"github.com/syawalqi/oryx/executor"
	"github.com/syawalqi/oryx/llm"
	"github.com/syawalqi/oryx/memory"
	"github.com/syawalqi/oryx/state"
	"github.com/syawalqi/oryx/updatepkg"
)

// borderOverhead is the total horizontal space consumed by the double border
// plus padding: left border (1) + left padding (1) + right padding (1) + right border (1).
const borderOverhead = 4

// Version is set at build time via -ldflags.
var version = "dev"

type streamResultMsg struct {
	result agent.StreamResult
}

type editorFinishedMsg struct {
	path string
	err  error
}

// autoScanMsg triggers a background system scan shortly after TUI starts.
type autoScanMsg struct{}

// Model is the Bubbletea model for the ORYX chat TUI.
type Model struct {
	ready    bool
	viewport viewport.Model
	input    string
	messages []ChatMessage
	header   HeaderData

	agent        *agent.Agent
	db           *state.DB
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
	streamCancel    context.CancelFunc
	streamWatchdog  *time.Timer
	streamCh        <-chan agent.StreamResult
	streamContent   strings.Builder
	streamReasoning strings.Builder
	streamMsgs      []string
	streamToolRun   bool

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

	// Plan mode
	planMode bool

	// Conversation history passed to LLM across turns
	history []llm.Message

	// Build version
	version string

	// Conversation list for /load command
	savedConvs   []state.Conversation
	showConvList bool
	convCursor   int

	// Plan-and-Execute state
	currentPlan    *agent.Plan
	planInProgress bool
}

// NewModel creates the chat TUI model.
// If initialHistory is provided (from --resume), it initializes the message
// display with those messages.
func NewModel(ag *agent.Agent, database *state.DB, systemPrompt, configPath, memoryPath, configDir, buildVersion string, initialHistory []llm.Message) tea.Model {
	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))
	s.Spinner = spinner.Line

	// Convert initial LLM history to ChatMessages for display
	var msgs []ChatMessage
	hasContent := false
	for _, m := range initialHistory {
		if m.Content != "" {
			hasContent = true
		}
		msgs = append(msgs, ChatMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}

	m := &Model{
		agent:        ag,
		db:           database,
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
		showLogo:    !hasContent,
		version:     buildVersion,
		messages:    msgs,
		history:     initialHistory,
	}

	// If we resumed with history, skip the logo
	if len(initialHistory) > 0 {
		m.showLogo = false
	}

	return m
}

// GetHistory returns the current conversation history for saving.
func (m *Model) GetHistory() []llm.Message {
	return m.history
}

func (m *Model) Init() tea.Cmd {
	exec.Command("stty", "-ixon").Run()
	return tea.Batch(
		m.spinner.Tick,
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return autoScanMsg{}
		}),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		chatW := msg.Width - borderOverhead
		if chatW < 40 {
			chatW = 40
		}
		m.sidebarWidth = 28

		if !m.ready {
			m.viewport = viewport.New(chatW, msg.Height-5)
			m.viewport.YPosition = 2
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
		if !m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case autoScanMsg:
		exec := executor.New(30, 200, []string{})
		if info, err := memory.Scan(context.Background(), exec); err == nil {
			content := info.Render()
			os.WriteFile(m.memoryPath, []byte(content), 0644)
			m.sidebarData.UpdateFromScan(info)
		}
		return m, nil

	case streamResultMsg:
		return m.handleStreamResult(msg.result)

	case updateResultMsg:
		return m.handleUpdateResult(msg)

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

	case convListMsg:
		m.showConvList = msg.show
		m.savedConvs = msg.convs
		m.convCursor = 0
		m.updateViewport()
		return m, nil

	case planEventMsg:
		return m.handlePlanEvent(msg.event)

	case tea.MouseMsg:
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
		if !m.loading {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if !m.loading {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// --- rendering ---

func (m *Model) View() string {
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
		if m.showConvList {
			inputLine = dimmedStyle.Render("[/load] select conversation (↑↓, Enter to load, Esc to cancel)")
		} else {
			inputLine = prompt + m.input + cursor
		}
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

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Conversation list navigation
	if m.showConvList {
		switch key {
		case "up":
			if m.convCursor > 0 {
				m.convCursor--
			}
			m.updateViewport()
			return m, nil
		case "down":
			if m.convCursor < len(m.savedConvs)-1 {
				m.convCursor++
			}
			m.updateViewport()
			return m, nil
		case "enter":
			return m.loadSelectedConversation()
		case "esc":
			m.showConvList = false
			m.savedConvs = nil
			m.updateViewport()
			return m, nil
		}
		return m, nil
	}

	// Global: quit
	if key == "ctrl+c" || key == "ctrl+d" {
		return m, tea.Quit
	}

	// During streaming: ctrl+c cancels, scroll keys still work
	if m.loading {
		if key == "ctrl+c" {
			m.loading = false
			m.cancelStream()
			m.streamCh = nil
			m.finalizeStream()
			m.updateViewport()
			return m, nil
		}
		switch key {
		case "up", "down", "pgup", "pgdown", "home", "end":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// --- Command palette mode (input starts with "/") ---
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

	// Toggle keys
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
	if key == "ctrl+s" && !m.loading {
		m.showSidebar = !m.showSidebar
		m.updateViewport()
		return m, nil
	}
	if key == "ctrl+p" && !m.loading {
		m.togglePlanMode()
		return m, nil
	}

	// Regular input
	if key == "enter" && !m.loading && !m.showConvList {
		return m.sendMessage()
	}
	if key == "backspace" && len(m.input) > 0 {
		m.input = m.input[:len(m.input)-1]
		m.updateViewport()
		if strings.HasPrefix(m.input, "/") {
			m.palette.Filter(m.input)
		}
		return m, nil
	}
	if len(key) == 1 && !m.loading {
		m.input += key
		m.updateViewport()
		if strings.HasPrefix(m.input, "/") {
			m.palette.Filter(m.input)
		}
		return m, nil
	}

	return m, nil
}

// loadSelectedConversation loads the conversation at convCursor into the chat.
func (m *Model) loadSelectedConversation() (tea.Model, tea.Cmd) {
	if m.convCursor < 0 || m.convCursor >= len(m.savedConvs) {
		m.showConvList = false
		return m, nil
	}
	conv := m.savedConvs[m.convCursor]
	m.showConvList = false
	m.savedConvs = nil

	// Convert LLM messages to ChatMessages for display
	m.messages = nil
	m.history = nil
	for _, lm := range conv.Messages {
		if lm.Role == llm.RoleSystem {
			continue
		}
		m.messages = append(m.messages, ChatMessage{
			Role:    string(lm.Role),
			Content: lm.Content,
		})
		m.history = append(m.history, lm)
	}

	m.showLogo = false
	m.updateViewport()
	m.viewport.GotoBottom()
	return m, nil
}

func (m *Model) sendMessage() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	m.input = ""
	m.palette.Filter("")

	if input == "" {
		return m, nil
	}

	// Handle slash commands
	if strings.HasPrefix(input, "/") {
		switch input {
		case "/save":
			if m.db == nil {
				m.messages = append(m.messages, ChatMessage{Role: "error", Content: "no state database available"})
				m.updateViewport()
				return m, nil
			}
			ts := fmt.Sprintf("chat-%d", time.Now().Unix())
			if err := m.db.SaveConversation(ts, m.history, m.agent.ModelName()); err != nil {
				m.messages = append(m.messages, ChatMessage{Role: "error", Content: fmt.Sprintf("save failed: %v", err)})
			} else {
				m.messages = append(m.messages, ChatMessage{Role: "assistant", Content: fmt.Sprintf("💾 Conversation saved as `%s` (%d messages)", ts, len(m.history))})
			}
			m.updateViewport()
			return m, nil
		case "/load":
			if m.db == nil {
				m.messages = append(m.messages, ChatMessage{Role: "error", Content: "no state database available"})
				m.updateViewport()
				return m, nil
			}
			return m, m.loadConversationList()
		case "/update":
			force := false
			trackOverride := ""
			for _, arg := range strings.Fields(input) {
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
			return m, m.runUpdate(force, trackOverride)
		}
	}

	m.streamContent.Reset()
	m.streamReasoning.Reset()
	m.streamMsgs = nil
	m.streamToolRun = false
	m.loading = true

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	m.streamCancel = cancel

	userMsg := llm.Message{Role: llm.RoleUser, Content: input}

	m.messages = append(m.messages, ChatMessage{Role: "user", Content: input})
	m.history = append(m.history, userMsg)

	msgs := make([]llm.Message, len(m.history))
	copy(msgs, m.history)

	prompt := m.systemPrompt
	if m.planMode {
		prompt += "\n\n⚠️ PLAN MODE ACTIVE — You can ONLY read files (read_file) and search logs (search_logs). " +
			"You CANNOT run commands, write files, or modify services. " +
			"Do NOT attempt destructive actions — they will be blocked. Analyze and suggest changes only."
	}
	// If plan mode is active, use Plan-and-Execute
	if m.planMode {
		return m.startPlan(input)
	}

	ch, err := m.agent.RunStream(ctx, prompt, msgs)
	if err != nil {
		m.loading = false
		m.cancelStream()
		m.messages = append(m.messages, ChatMessage{Role: "error", Content: fmt.Sprintf("stream error: %v", err)})
		m.updateViewport()
		return m, nil
	}
	m.streamCh = ch

	return m, m.pollStream()
}

// startPlan kicks off Plan-and-Execute mode. It creates a plan from the
// user's input and executes it step by step.
func (m *Model) startPlan(input string) (tea.Model, tea.Cmd) {
	m.streamContent.Reset()
	m.streamReasoning.Reset()
	m.streamMsgs = nil
	m.streamToolRun = false
	m.loading = true
	m.planInProgress = true
	m.currentPlan = nil

	ctx := context.Background()

	return m, func() tea.Msg {
		planCh, err := m.agent.RunPlanStream(ctx, m.systemPrompt, input)
		if err != nil {
			return planEventMsg{event: agent.PlanEvent{Error: err.Error()}}
		}
		// Drain the plan channel and forward events to the TUI
		for evt := range planCh {
			// Send planEventMsg via the TUI's message loop
			// We need to return these one at a time via the poll mechanism
			// For now, collect the final result
			if evt.Done {
				m.loading = false
				m.planInProgress = false
				return planEventMsg{event: evt}
			}
			if evt.Error != "" {
				m.loading = false
				m.planInProgress = false
				return planEventMsg{event: evt}
			}
			if evt.Plan != nil {
				m.currentPlan = evt.Plan
			}
			if evt.StepUpdate != nil {
				// Update the plan in-place
				if m.currentPlan != nil && evt.StepUpdate.ID > 0 && evt.StepUpdate.ID <= len(m.currentPlan.Steps) {
					m.currentPlan.Steps[evt.StepUpdate.ID-1] = *evt.StepUpdate
				}
			}
		}
		m.loading = false
		m.planInProgress = false
		return planEventMsg{event: agent.PlanEvent{Done: true}}
	}
}

// handlePlanEvent processes a plan execution event from the agent.
func (m *Model) handlePlanEvent(evt agent.PlanEvent) (tea.Model, tea.Cmd) {
	if evt.Error != "" {
		m.messages = append(m.messages, ChatMessage{Role: "error", Content: "⚠ " + evt.Error})
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil
	}

	if evt.Plan != nil {
		m.currentPlan = evt.Plan
		// Show the plan as an assistant message
		var b strings.Builder
		b.WriteString("📋 **Plan created:**\n\n")
		for _, step := range evt.Plan.Steps {
			b.WriteString(fmt.Sprintf("  %d. %s\n", step.ID, step.Description))
		}
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: b.String(),
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil
	}

	if evt.StepUpdate != nil {
		step := evt.StepUpdate
		var statusSym string
		switch step.Status {
		case "running":
			statusSym = "▶"
		case "done":
			statusSym = "✅"
		case "failed":
			statusSym = "❌"
		default:
			statusSym = "⏳"
		}
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("%s Step %d: %s", statusSym, step.ID, step.Description),
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil
	}

	if evt.StepResult != "" {
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: evt.StepResult,
		})
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil
	}

	if evt.Done {
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: "✅ Plan execution complete.",
		})
		m.loading = false
		m.planInProgress = false
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil
	}

	return m, nil
}

// loadConversationList fetches saved conversations from the DB and displays them.
func (m *Model) loadConversationList() tea.Cmd {
	return func() tea.Msg {
		convs, err := m.db.ListConversations()
		if err != nil {
			return convListMsg{show: true}
		}
		return convListMsg{show: len(convs) > 0, convs: convs}
	}
}

// convListMsg carries the conversation list for /load display.
type convListMsg struct {
	show  bool
	convs []state.Conversation
}

// cancelStream cancels the streaming context if active.
func (m *Model) cancelStream() {
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
}

// updateResultMsg carries the result of a self-update.
type updateResultMsg struct {
	err     error
	version string
}

func (m *Model) runUpdate(force bool, trackOverride string) tea.Cmd {
	version := m.version
	return func() tea.Msg {
		var err error
		if trackOverride != "" {
			err = updatepkg.RunWithTrack(version, updatepkg.Track(trackOverride), force)
		} else {
			err = updatepkg.Run(version, force)
		}
		return updateResultMsg{err: err, version: version}
	}
}

func (m *Model) handleUpdateResult(msg updateResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.messages = append(m.messages, ChatMessage{Role: "error", Content: fmt.Sprintf("update failed: %v", msg.err)})
	} else {
		m.messages = append(m.messages, ChatMessage{Role: "assistant", Content: "✅ Update complete! Restart ORYX to use the new version."})
	}
	m.updateViewport()
	m.viewport.GotoBottom()
	return m, nil
}

func (m *Model) pollStream() tea.Cmd {
	return func() tea.Msg {
		result, ok := <-m.streamCh
		if !ok {
			return streamResultMsg{result: agent.StreamResult{Done: true}}
		}
		return streamResultMsg{result: result}
	}
}

func (m *Model) handleStreamResult(result agent.StreamResult) (tea.Model, tea.Cmd) {
	if result.Err != nil {
		m.loading = false
		m.cancelStream()
		m.streamCh = nil
		m.messages = append(m.messages, ChatMessage{Role: "error", Content: fmt.Sprintf("error: %v", result.Err)})
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil
	}

	if result.Revised {
		// Reflexion replaced the response — discard original, start fresh
		m.streamContent.Reset()
		m.streamReasoning.Reset()
		m.streamMsgs = nil
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
		tr := result.ToolResult
		outputLines := strings.Split(strings.TrimRight(tr.Output, "\n"), "\n")
		n := len(outputLines)
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
		if len(m.streamMsgs) > 3 {
			m.streamMsgs = m.streamMsgs[len(m.streamMsgs)-3:]
		}
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, tea.Batch(m.pollStream(), m.spinner.Tick)
	}

	if result.Done {
		m.loading = false
		m.cancelStream()
		m.streamCh = nil
		m.finalizeStream()
		m.updateViewport()
		m.viewport.GotoBottom()
		return m, nil
	}

	return m, tea.Batch(m.pollStream(), m.spinner.Tick)
}

func (m *Model) finalizeStream() {
	content := strings.TrimRight(m.streamContent.String(), "\n")
	reasoning := strings.TrimRight(m.streamReasoning.String(), "\n")

	// Reset stream buffers so renderMessages doesn't duplicate the content
	m.streamContent.Reset()
	m.streamReasoning.Reset()
	m.streamMsgs = nil

	if content != "" || reasoning != "" || len(m.streamMsgs) > 0 {
		msg := ChatMessage{
			Role:      "assistant",
			Content:   content,
			Reasoning: reasoning,
			ToolCalls: m.streamMsgs,
		}
		if m.streamMsgs != nil {
			// Copy to avoid aliasing
			msg.ToolCalls = make([]string, len(m.streamMsgs))
			copy(msg.ToolCalls, m.streamMsgs)
		}
		m.messages = append(m.messages, msg)
		// Add to LLM history
		m.history = append(m.history, llm.Message{
			Role:    llm.RoleAssistant,
			Content: content,
		})
	}
}

// --- rendering helpers ---

func (m *Model) updateViewport() {
	chatW := m.viewport.Width
	if chatW < 1 {
		chatW = m.width - borderOverhead
	}

	var content string
	if m.showConvList {
		content = renderConvList(m.savedConvs, m.convCursor)
	} else {
		content = renderMessages(m.messages, m.streamContent.String(), m.streamReasoning.String(), m.streamMsgs, chatW, m.expandReasoning, m.expandTools, m.showLogo, m.viewport.Height)
	}

	m.viewport.SetContent(content)
	m.blockRegions = detectBlockRegions(content)
}

func (m *Model) reflowViewport() {
	chatW := m.width - borderOverhead
	if chatW < 40 {
		chatW = 40
	}
	m.sidebarWidth = 28
	m.viewport.Width = chatW
	m.viewport.Height = m.height - 5
	m.updateViewport()
}

func (m *Model) togglePlanMode() {
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
func (m *Model) saveConversation() tea.Cmd {
	path := fmt.Sprintf("/tmp/oryx-chat-%s.txt", time.Now().Format("20060102-150405"))
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

// renderConvList shows saved conversations for /load selection.
func renderConvList(convs []state.Conversation, cursor int) string {
	if len(convs) == 0 {
		return dimmedStyle.Render("No saved conversations found.\n\nStart one with `oryx chat` and use /save to store it.")
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true).Render("Saved Conversations\n\n"))
	for i, conv := range convs {
		cursorSym := " "
		if i == cursor {
			cursorSym = "▸"
		}
		summary := conv.Summary
		if summary == "" {
			summary = "(no messages)"
		}
		ts := conv.UpdatedAt.Format("Jan 02 15:04")
		n := len(conv.Messages)
		line := fmt.Sprintf("%s [%s] %s (%d msgs) — %s", cursorSym, ts, conv.Model, n, summary)
		if i == cursor {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Render(line) + "\n")
		} else {
			b.WriteString(dimmedStyle.Render(line) + "\n")
		}
	}
	return b.String()
}
