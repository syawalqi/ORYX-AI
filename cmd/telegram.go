package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/syawalqi/oryx/agent"
	"github.com/syawalqi/oryx/config"
	"github.com/syawalqi/oryx/executor"
	"github.com/syawalqi/oryx/state"
	"github.com/syawalqi/oryx/telegrambot"
)

func Telegram(cfg *config.Config) error {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		if cfg.Alerts.TelegramToken != "" {
			token = cfg.Alerts.TelegramToken
		} else {
			return fmt.Errorf("TELEGRAM_BOT_TOKEN not set and no telegram_token in config")
		}
	}

	prov := getProvider(cfg)
	exec := executor.New(cfg.Executor.Timeout, cfg.Executor.MaxOutputLines, cfg.Executor.BlockedCommands)

	// Open state DB for audit logging
	stateDir := os.ExpandEnv("$HOME/.local/state/oryx")
	os.MkdirAll(stateDir, 0755)
	db, err := state.Open(stateDir + "/oryx.db")
	if err != nil {
		return fmt.Errorf("open state db: %w", err)
	}
	defer db.Close()

	// Create agent with budget and audit
	reflexCfg := agent.DefaultReflexionConfig()
	reflexCfg.Enabled = false
	ag := agent.New(prov, exec, cfg.Model, cfg.Agent.MaxTokens, cfg.Agent.Temperature, cfg.Agent.MaxIterations,
		agent.WithBudget(cfg.Agent.MaxIterations, 0, cfg.Agent.MaxCost),
		agent.WithReflexion(reflexCfg),
		agent.WithAudit(func(tool, args, result string, success bool, duration string, iteration int) {
			db.AppendToolLog(tool, args, result, success, duration, iteration)
		}),
	)

	systemPrompt := "You are ORYX, an AI agent running as a Telegram bot. " +
		"You can run commands, read/write files, manage services, and search logs on a Linux VPS. " +
		"Be concise.\n\n" +
		"## Output Format\n" +
		"- Respond in plain text. No markdown except for code blocks.\n" +
		"- For shell commands, use `$ command` format.\n" +
		"- Be direct and helpful."

	// Load memory context
	memoryPath := os.ExpandEnv("$HOME/.config/oryx/memory.md")
	if data, err := os.ReadFile(memoryPath); err == nil && len(data) > 0 {
		systemPrompt += "\n\n## Server Context\n" + string(data)
	}

	bot := telegrambot.New(token, ag, db, systemPrompt)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("telegram bot: shutting down...")
		cancel()
	}()

	log.Println("telegram bot: starting")
	if err := bot.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("telegram bot error: %w", err)
	}
	return nil
}
