package setup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/syawalqi/flare/llm"
)

// --- steps ---
type step int

const (
	stepProvider step = iota
	stepCustomURL
	stepAPIKey
	stepTesting
	stepModel
	stepConfirm
)

var stepTitles = map[step]string{
	stepProvider:  "Provider",
	stepCustomURL: "Custom URL",
	stepAPIKey:    "API Key",
	stepTesting:   "Testing",
	stepModel:     "Model",
	stepConfirm:   "Confirm",
}

// --- list items ---

type providerItem struct {
	name    string
	id      string // "opencode-go", "openrouter", "custom"
	baseURL string
	desc    string
}

func (i providerItem) Title() string       { return i.name }
func (i providerItem) Description() string  { return i.desc }
func (i providerItem) FilterValue() string  { return i.name }

var providerOptions = []list.Item{
	providerItem{name: "OpenCode Go", id: "opencode-go", baseURL: "https://opencode.ai/zen/go/v1", desc: "OpenCode AI — GPT-4 class models"},
	providerItem{name: "OpenRouter", id: "openrouter", baseURL: "https://openrouter.ai/api/v1", desc: "Multi-provider router — 200+ models"},
	providerItem{name: "Custom", id: "custom", baseURL: "", desc: "Any OpenAI-compatible API"},
}

type modelItem struct {
	id   string
	name string
	desc string
}

func (i modelItem) Title() string       { return i.id }
func (i modelItem) Description() string  { return i.desc }
func (i modelItem) FilterValue() string  { return i.id + " " + i.name }

// --- messages ---

type testAndFetchDoneMsg struct {
	models []llm.ModelInfo
	err    error
}

// --- model ---

type model struct {
	step    step
	result  *Result
	err     error
	quitting bool

	width  int
	height int

	// collected data
	providerID string
	baseURL    string
	apiKey     string
	modelID    string
	models     []llm.ModelInfo

	// widgets
	providerList list.Model
	urlInput     textinput.Model
	keyInput     textinput.Model
	spn          spinner.Model
	modelList    list.Model

	// transient state
	testing  bool
	testErr  error
}

// Result holds the configuration collected by the wizard.
type Result struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
}

// Run starts the setup wizard TUI and returns the collected configuration.
func Run() (*Result, error) {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("#7C3AED"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color("#A78BFA"))

	pl := list.New(providerOptions, delegate, 0, 0)
	pl.Title = "Select LLM Provider"
	pl.SetShowStatusBar(false)
	pl.SetFilteringEnabled(false)

	// Key input (masked)
	ki := textinput.New()
	ki.Placeholder = "sk-..."
	ki.EchoMode = textinput.EchoPassword
	ki.EchoCharacter = '•'
	ki.Width = 60
	ki.Focus()

	// URL input (for custom provider)
	ui := textinput.New()
	ui.Placeholder = "https://your-api.com/v1"
	ui.Width = 60

	// Spinner
	spn := spinner.New()
	spn.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))
	spn.Spinner = spinner.Dot

	m := &model{
		step:         stepProvider,
		providerList: pl,
		urlInput:     ui,
		keyInput:     ki,
		spn:          spn,
	}
	return m.run()
}

func (m *model) run() (*Result, error) {
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	mdl := final.(*model)
	if mdl.result != nil {
		return mdl.result, nil
	}
	return nil, mdl.err
}

// --- tea.Model ---

func (m *model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.providerList.SetSize(msg.Width-10, msg.Height-10)
		if m.modelList.Items() != nil && len(m.modelList.Items()) > 0 {
			m.modelList.SetSize(msg.Width-10, msg.Height-10)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			m.err = fmt.Errorf("cancelled")
			return m, tea.Quit
		case "esc":
			return m.goBack()
		case "enter":
			return m.advance()
		}

	case testAndFetchDoneMsg:
		m.testing = false
		if msg.err != nil {
			m.testErr = msg.err
			return m, nil
		}
		m.models = msg.models
		if len(m.models) == 0 {
			// No models returned — let user type one manually
			m.step = stepModel
			// Build an empty list with manual entry hint
			m.buildEmptyModelList()
			return m, nil
		}
		m.step = stepModel
		m.buildModelList()
		return m, nil
	}

	return m.forwardMsg(msg)
}

func (m *model) View() string {
	if m.quitting {
		return ""
	}

	var body string
	switch m.step {
	case stepProvider:
		body = m.renderProvider()
	case stepCustomURL:
		body = m.renderCustomURL()
	case stepAPIKey:
		body = m.renderAPIKey()
	case stepTesting:
		body = m.renderTesting()
	case stepModel:
		body = m.renderModel()
	case stepConfirm:
		body = m.renderConfirm()
	}

	return docStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			m.renderProgress(),
			"",
			body,
		),
	)
}

// --- progress bar ---

func (m *model) renderProgress() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("⚡ Flare Setup"))
	b.WriteString("\n")

	total := int(stepConfirm) + 1
	for i := 0; i < total; i++ {
		s := step(i)
		title := stepTitles[s]
		if i == int(m.step) {
			b.WriteString(activeStepStyle.Render(fmt.Sprintf(" ● %s ", title)))
		} else if i < int(m.step) {
			b.WriteString(successStyle.Render(fmt.Sprintf(" ✓ %s ", title)))
		} else {
			b.WriteString(stepStyle.Render(fmt.Sprintf(" ○ %s ", title)))
		}
		if i < total-1 {
			b.WriteString(stepStyle.Render(" → "))
		}
	}
	return b.String()
}

// --- step renderers ---

func (m *model) renderProvider() string {
	return m.providerList.View()
}

func (m *model) renderCustomURL() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		labelStyle.Render("Enter the API base URL:"),
		m.urlInput.View(),
		infoStyle.Render("\n↑ Enter to confirm • Esc to go back"),
	)
}

func (m *model) renderAPIKey() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		labelStyle.Render("Enter your API key:"),
		m.keyInput.View(),
		infoStyle.Render("\n↑ Key is masked for security • Enter to test • Esc to go back"),
	)
}

func (m *model) renderTesting() string {
	if m.testing {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.spn.View()+" Testing connection and fetching models...",
			infoStyle.Render("\nThis may take a few seconds."),
		)
	}
	if m.testErr != nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			errorStyle.Render("✗ Connection failed:"),
			errorStyle.Render(m.testErr.Error()),
			"",
			stepStyle.Render("Press Enter to retry or Esc to go back."),
		)
	}
	return successStyle.Render("✓ Connection OK!")
}

func (m *model) renderModel() string {
	if m.modelList.Items() == nil || len(m.modelList.Items()) == 0 {
		// No models from API — show a manual entry prompt
		return lipgloss.JoinVertical(lipgloss.Left,
			infoStyle.Render("Provider didn't return a model list."),
			infoStyle.Render("Type your model ID manually and press Enter."),
			"",
			labelStyle.Render("Model ID:"),
			m.keyInput.View(), // reuse key input widget for model name
			infoStyle.Render("\n↑ Example: deepseek/deepseek-v4-flash • Enter to confirm"),
		)
	}
	return m.modelList.View()
}

func (m *model) renderConfirm() string {
	keyDisplay := m.apiKey
	if len(keyDisplay) > 12 {
		keyDisplay = keyDisplay[:6] + "…" + keyDisplay[len(keyDisplay)-4:]
	}

	provName := m.providerID
	for _, item := range providerOptions {
		if it := item.(providerItem); it.id == m.providerID {
			provName = it.name
			break
		}
	}

	sections := lipgloss.JoinVertical(lipgloss.Left,
		labelStyle.Render("Provider:  ")+valueStyle.Render(provName),
		labelStyle.Render("Base URL:  ")+valueStyle.Render(m.baseURL),
		labelStyle.Render("Model:     ")+valueStyle.Render(m.modelID),
		labelStyle.Render("API Key:   ")+keySummaryStyle.Render(keyDisplay),
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		confirmStyle.Render(sections),
		"",
		successStyle.Render("✓ Press Enter to save"),
		stepStyle.Render("Esc to go back and change"),
	)
}

// --- message forwarding ---

func (m *model) forwardMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.step {
	case stepProvider:
		m.providerList, cmd = m.providerList.Update(msg)
	case stepCustomURL:
		var c tea.Cmd
		m.urlInput, c = m.urlInput.Update(msg)
		cmd = c
	case stepAPIKey:
		var c tea.Cmd
		m.keyInput, c = m.keyInput.Update(msg)
		cmd = c
	case stepTesting:
		m.spn, cmd = m.spn.Update(msg)
	case stepModel:
		// If list has items (from API), forward to list
		if m.modelList.Items() != nil && len(m.modelList.Items()) > 0 {
			m.modelList, cmd = m.modelList.Update(msg)
		} else {
			// Manual entry mode — forward to the reused textinput
			var c tea.Cmd
			m.keyInput, c = m.keyInput.Update(msg)
			cmd = c
		}
	}
	return m, cmd
}

// --- navigation ---

func (m *model) advance() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepProvider:
		selected := m.providerList.SelectedItem()
		if selected == nil {
			return m, nil
		}
		item := selected.(providerItem)
		m.providerID = item.id
		m.baseURL = item.baseURL

		if item.id == "custom" {
			m.step = stepCustomURL
			m.urlInput.Focus()
			return m, textinput.Blink
		}
		m.step = stepAPIKey
		m.keyInput.Focus()
		return m, textinput.Blink

	case stepCustomURL:
		url := strings.TrimSpace(m.urlInput.Value())
		if url == "" {
			return m, nil
		}
		m.baseURL = url
		m.step = stepAPIKey
		m.keyInput.Focus()
		return m, textinput.Blink

	case stepAPIKey:
		key := strings.TrimSpace(m.keyInput.Value())
		if key == "" {
			return m, nil
		}
		m.apiKey = key
		return m, m.startTestingAndFetch()

	case stepTesting:
		if m.testErr != nil {
			m.testErr = nil
			return m, m.startTestingAndFetch()
		}
		return m, nil

	case stepModel:
		// If list has items, get selection from list
		if m.modelList.Items() != nil && len(m.modelList.Items()) > 0 {
			selected := m.modelList.SelectedItem()
			if selected == nil {
				return m, nil
			}
			m.modelID = selected.(modelItem).id
		} else {
			// Manual entry mode — get from text input
			model := strings.TrimSpace(m.keyInput.Value())
			if model == "" {
				return m, nil
			}
			m.modelID = model
		}
		m.step = stepConfirm
		return m, nil

	case stepConfirm:
		m.result = &Result{
			Provider: m.providerID,
			BaseURL:  m.baseURL,
			APIKey:   m.apiKey,
			Model:    m.modelID,
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) goBack() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepCustomURL:
		m.step = stepProvider
	case stepAPIKey:
		if m.baseURL == "" || m.providerID == "custom" {
			m.step = stepCustomURL
		} else {
			m.step = stepProvider
		}
	case stepTesting:
		if m.testing {
			return m, nil // can't go back during loading
		}
		m.step = stepAPIKey
		m.keyInput.Focus()
		return m, textinput.Blink
	case stepModel:
		if m.modelList.Items() != nil && len(m.modelList.Items()) > 0 {
			m.step = stepTesting
		} else {
			// If in manual entry mode, reset keyInput for API key use
			m.keyInput.SetValue("")
			m.keyInput.EchoMode = textinput.EchoPassword
			m.keyInput.EchoCharacter = '•'
			m.step = stepTesting
		}
	case stepConfirm:
		m.step = stepModel
	}
	return m, nil
}

// --- async operations ---

func (m *model) startTestingAndFetch() tea.Cmd {
	m.testing = true
	m.testErr = nil
	m.step = stepTesting

	baseURL := m.baseURL
	if baseURL == "" {
		baseURL = llm.DefaultBaseURL(m.providerID)
	}
	prov := llm.NewOpenAIProvider(m.providerID, baseURL, m.apiKey)

	return func() tea.Msg {
		time.Sleep(300 * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		models, err := prov.ListModels(ctx)
		if err != nil {
			return testAndFetchDoneMsg{err: err}
		}
		return testAndFetchDoneMsg{models: models}
	}
}

func (m *model) buildModelList() {
	items := make([]list.Item, len(m.models))
	for i, mm := range m.models {
		desc := mm.Name
		if desc == "" {
			desc = mm.Description
		}
		items[i] = modelItem{
			id:   mm.ID,
			name: mm.Name,
			desc: desc,
		}
	}

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("#7C3AED"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color("#A78BFA"))

	w := m.width - 10
	h := m.height - 10
	if w < 40 {
		w = 40
	}
	if h < 10 {
		h = 10
	}

	ml := list.New(items, delegate, w, h)
	ml.Title = fmt.Sprintf("Select Model (%d available)", len(items))
	ml.SetShowStatusBar(false)
	ml.SetFilteringEnabled(true)
	// Don't set initial filter text — SetFilterText sets the FILTER VALUE, not the prompt.
	// The default prompt "Filter: " is set by the list constructor.
	m.modelList = ml
}

func (m *model) buildEmptyModelList() {
	// Switch the key input widget to accept a model name
	m.keyInput.SetValue("")
	m.keyInput.EchoMode = textinput.EchoNormal
	m.keyInput.EchoCharacter = 0
	m.keyInput.Placeholder = "deepseek/deepseek-v4-flash"
	m.keyInput.Focus()
}
