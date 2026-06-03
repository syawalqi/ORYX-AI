package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/syawalqi/flare/config"
	"github.com/syawalqi/flare/executor"
	"github.com/syawalqi/flare/memory"
	"github.com/syawalqi/flare/tui/setup"
)

func Setup(cfg *config.Config) error {
	result, err := setup.Run()
	if err != nil {
		return fmt.Errorf("setup cancelled: %w", err)
	}

	// Save to config
	cfg.Provider = result.Provider
	cfg.APIKey = result.APIKey
	cfg.Model = result.Model
	cfg.DaemonModel = result.Model

	configDir := os.ExpandEnv("$HOME/.config/flare")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("mkdir config: %w", err)
	}

	configPath := configDir + "/config.yaml"
	if err := cfg.Save(configPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\n✅ Config saved to %s\n", configPath)
	fmt.Printf("   Provider: %s\n", cfg.Provider)
	fmt.Printf("   Model:    %s\n", cfg.Model)
	fmt.Printf("   API Key:  %s…\n", maskKey(cfg.APIKey))

	// Scan server for memory
	fmt.Println("\nScanning server for memory.md...")
	exec := executor.New(cfg.Executor.Timeout, cfg.Executor.MaxOutputLines, cfg.Executor.BlockedCommands)
	info, err := memory.Scan(context.Background(), exec)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	memoryPath := configDir + "/memory.md"
	content := info.Render()
	if err := memory.Save(memoryPath, content); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}
	fmt.Printf("✅ Memory saved to %s (%d bytes)\n", memoryPath, len(content))
	fmt.Println("\nSetup complete! Run 'flare chat' to start.")
	return nil
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "…" + key[len(key)-4:]
}
