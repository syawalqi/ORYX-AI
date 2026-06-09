package cmd

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/syawalqi/oryx/agent"
	"github.com/syawalqi/oryx/config"
	"github.com/syawalqi/oryx/executor"
	"github.com/syawalqi/oryx/llm"
	"github.com/syawalqi/oryx/state"
	"github.com/syawalqi/oryx/tui"
)

func Chat(cfg *config.Config, buildVersion string, resume bool) error {
	prov := getProvider(cfg)
	exec := executor.New(cfg.Executor.Timeout, cfg.Executor.MaxOutputLines, cfg.Executor.BlockedCommands)

	// Open state DB
	stateDir := os.ExpandEnv("$HOME/.local/state/oryx")
	os.MkdirAll(stateDir, 0755)
	dbPath := stateDir + "/oryx.db"
	db, err := state.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open state db: %w", err)
	}
	defer db.Close()

	// Create agent with budget tracking and audit logging
	reflexCfg := agent.DefaultReflexionConfig()
	reflexCfg.Enabled = true
	ag := agent.New(prov, exec, cfg.Model, cfg.Agent.MaxTokens, cfg.Agent.Temperature, cfg.Agent.MaxIterations,
		agent.WithBudget(cfg.Agent.MaxIterations, 0, cfg.Agent.MaxCost),
		agent.WithReflexion(reflexCfg),
		agent.WithAudit(func(tool, args, result string, success bool, duration string, iteration int) {
			db.AppendToolLog(tool, args, result, success, duration, iteration)
		}),
	)

	systemPrompt := "You are ORYX, a server management AI agent running as the `oryx chat` Go binary. You help manage a Linux VPS server. " +
		"You have access to tools: run_command, read_file, write_file, service_action, search_logs. " +
		"Use them to diagnose and fix issues. Be concise and direct.\n\n" +
		"## Identity\n" +
		"- **Your process:** The one running `oryx chat`. Find it with `ps aux | grep 'oryx chat' | grep -v grep`. It should show ~17 MB RSS.\n" +
		"- **Everything else:** Any other process you see (Python, MySQL, nginx, etc.) is a separate service. Do NOT attribute their resource usage to yourself.\n" +
		"- When asked about your resource usage, report ONLY the `oryx chat` Go binary. If you're unsure what a process is, check its command line with `cat /proc/<PID>/cmdline`.\n\n" +
		"## Output Format\n" +
		"- Respond in PLAIN TEXT only. No Markdown, no formatting, no bullet symbols, no bold, no tables.\n" +
		"- Use simple indentation or dashes for lists.\n" +
		"- Code examples or commands: put them on their own line, prefixed with `$ ` for shell commands."

	memoryPath := os.ExpandEnv("$HOME/.config/oryx/memory.md")
	if data, err := os.ReadFile(memoryPath); err == nil && len(data) > 0 {
		systemPrompt += "\n\n## Server Context\n" + string(data)
	}

	// Load existing conversation if --resume
	var history []llm.Message
	if resume {
		convs, err := db.ListConversations()
		if err == nil && len(convs) > 0 {
			history = convs[0].Messages
			// Filter out system messages (they're prepended by the agent)
			var filtered []llm.Message
			for _, m := range history {
				if m.Role != llm.RoleSystem {
					filtered = append(filtered, m)
				}
			}
			history = filtered
			systemPrompt += "\n\n(This is a resumed conversation. The conversation history below will be loaded into the chat.)"
		}
	}

	m := tui.NewModel(ag, db, systemPrompt,
		os.ExpandEnv("$HOME/.config/oryx/config.yaml"),
		memoryPath,
		os.ExpandEnv("$HOME/.config/oryx"),
		buildVersion,
		history,
	)

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	final, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui error: %w", err)
	}

	// Auto-save conversation on exit
	sessionID := fmt.Sprintf("chat-%d", time.Now().Unix())
	oryxModel := final.(*tui.Model)
	if msgs := oryxModel.GetHistory(); len(msgs) > 0 {
		if err := db.SaveConversation(sessionID, msgs, cfg.Model); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save conversation: %v\n", err)
		}
	}

	return nil
}

func getProvider(cfg *config.Config) llm.Provider {
	switch cfg.Provider {
	case "opencode", "opencode-go", "opencodego":
		baseURL := os.Getenv("OPENCODE_API_BASE")
		if baseURL == "" {
			baseURL = "https://opencode.ai/zen/go/v1"
		}
		return llm.NewOpenAIProvider(cfg.Provider, baseURL, cfg.APIKey)
	case "openrouter":
		baseURL := os.Getenv("OPENROUTER_BASE_URL")
		if baseURL == "" {
			baseURL = "https://openrouter.ai/api/v1"
		}
		return llm.NewOpenAIProvider(cfg.Provider, baseURL, cfg.APIKey)
	case "anthropic":
		key := cfg.APIKey
		if key == "" {
			key = os.Getenv("ANTHROPIC_API_KEY")
		}
		return llm.NewAnthropicProvider(key)
	case "ollama":
		baseURL := os.Getenv("OLLAMA_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return llm.NewOllamaProvider(baseURL)
	default:
		return llm.NewOpenAIProvider(cfg.Provider, "https://openrouter.ai/api/v1", cfg.APIKey)
	}
}
