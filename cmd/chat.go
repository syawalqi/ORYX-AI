package cmd

const defaultModel = defaultModel

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/syawalqi/flare/agent"
	"github.com/syawalqi/flare/config"
	"github.com/syawalqi/flare/executor"
	"github.com/syawalqi/flare/llm"
	"github.com/syawalqi/flare/tui"
)

func Chat(cfg *config.Config) error {
	prov := getProvider(cfg)
	exec := executor.New(cfg.Executor.Timeout, cfg.Executor.MaxOutputLines, cfg.Executor.BlockedCommands)
	ag := agent.New(prov, exec, cfg.Model, cfg.Agent.MaxTokens, cfg.Agent.Temperature, cfg.Agent.MaxIterations)

	systemPrompt := "You are Flare, a server management AI agent. You help manage a Linux VPS server. " +
		"You have access to tools: run_command, read_file, write_file, service_action, search_logs. " +
		"Use them to diagnose and fix issues. Be concise and direct."

	memoryPath := os.ExpandEnv("$HOME/.config/flare/memory.md")
	if data, err := os.ReadFile(memoryPath); err == nil && len(data) > 0 {
		systemPrompt += "\n\n## Server Context\n" + string(data)
	}

	m := tui.NewModel(ag, systemPrompt,
		os.ExpandEnv("$HOME/.config/flare/config.yaml"),
		memoryPath,
		os.ExpandEnv("$HOME/.config/flare"),
	)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui error: %w", err)
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
	default:
		return llm.NewOpenAIProvider(cfg.Provider, "https://openrouter.ai/api/v1", cfg.APIKey)
	}
}
