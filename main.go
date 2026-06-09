package main

import (
	"fmt"
	"os"

	"github.com/syawalqi/oryx/cmd"
	"github.com/syawalqi/oryx/config"
)

// Version is set at build time via -ldflags.
var version = "dev"

func main() {
	cfg := loadConfig()
	if len(os.Args) < 2 {
		// Default to chat when invoked as just "oryx"
		runChat(cfg)
		return
	}

	subcommand := os.Args[1]

	switch subcommand {
	case "chat":
		runChat(cfg)
	case "fix":
		if err := cmd.Fix(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "fix error: %v\n", err)
			os.Exit(1)
		}
	case "setup":
		if err := cmd.Setup(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "setup error: %v\n", err)
			os.Exit(1)
		}
	case "daemon":
		if err := cmd.Daemon(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
			os.Exit(1)
		}
	case "alert":
		if err := cmd.AlertCli(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "alert error: %v\n", err)
			os.Exit(1)
		}
	case "telegram":
		if err := cmd.Telegram(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "telegram error: %v\n", err)
			os.Exit(1)
		}
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", subcommand)
		usage()
		os.Exit(1)
	}
}

func runChat(cfg *config.Config) {
	resume := false
	for _, arg := range os.Args {
		if arg == "--resume" || arg == "-r" {
			resume = true
			break
		}
	}
	if err := cmd.Chat(cfg, version, resume); err != nil {
		fmt.Fprintf(os.Stderr, "chat error: %v\n", err)
		os.Exit(1)
	}
}

func loadConfig() *config.Config {
	cfg := config.Default()
	configPath := os.ExpandEnv("$HOME/.config/oryx/config.yaml")
	if parsed, err := config.Load(configPath); err == nil {
		cfg = parsed
	}
	// Env overrides for API key
	if key := os.Getenv("LLM_API_KEY"); key != "" {
		cfg.APIKey = key
	}
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" && cfg.Provider == "openrouter" {
		cfg.APIKey = key
	}
	return cfg
}

func usage() {
	fmt.Print(`ORYX — General Management AI Agent

Usage:
  oryx setup      Interactive first-run configuration
  oryx chat       Interactive chat with LLM agent
  oryx telegram   Run as Telegram bot (long-polling)
  oryx daemon     Background monitoring daemon
  oryx fix        Fix an anomaly (auto-remediate with LLM)
  oryx alert      Send an alert (script hook)
  oryx help       Show this help
`)
}
