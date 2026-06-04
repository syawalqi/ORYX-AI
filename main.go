package main

import (
	"fmt"
	"os"

	"github.com/syawalqi/flare/cmd"
	"github.com/syawalqi/flare/config"
)

// Version is set at build time via -ldflags.
var version = "dev"

func main() {
	cfg := loadConfig()
	if len(os.Args) < 2 {
		// Default to chat when invoked as just "flare"
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
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", subcommand)
		usage()
		os.Exit(1)
	}
}

func runChat(cfg *config.Config) {
	if err := cmd.Chat(cfg, version); err != nil {
		fmt.Fprintf(os.Stderr, "chat error: %v\n", err)
		os.Exit(1)
	}
}

func loadConfig() *config.Config {
	cfg := config.Default()
	configPath := os.ExpandEnv("$HOME/.config/flare/config.yaml")
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
	fmt.Print(`Flare — Server Management AI Agent

Usage:
  flare setup      Interactive first-run configuration
  flare chat       Interactive chat with LLM agent
  flare daemon     Background monitoring daemon
  flare fix        Fix an anomaly (auto-remediate with LLM)
  flare alert      Send an alert (script hook)
  flare help       Show this help
`)
}
