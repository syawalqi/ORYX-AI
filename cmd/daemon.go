package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/syawalqi/flare/alert"
	"github.com/syawalqi/flare/config"
	"github.com/syawalqi/flare/executor"
	"github.com/syawalqi/flare/scheduler"
	"github.com/syawalqi/flare/state"
)

func Daemon(cfg *config.Config) error {
	fmt.Println("Flare daemon starting...")

	// Open state database
	stateDir := os.ExpandEnv("$HOME/.local/state/flare")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("mkdir state: %w", err)
	}
	db, err := state.Open(stateDir + "/flare.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	// Setup executor
	exec := executor.New(cfg.Executor.Timeout, cfg.Executor.MaxOutputLines, cfg.Executor.BlockedCommands)

	// Parse interval
	interval, err := time.ParseDuration(cfg.Checks.Interval)
	if err != nil {
		interval = 5 * time.Minute
	}

	// Setup notifier
	var notifier *alert.WebhookNotifier
	if cfg.Alerts.Enabled && cfg.Alerts.WebhookURL != "" {
		notifier = alert.NewWebhookNotifier(cfg.Alerts.WebhookURL)
	}

	// Setup check engine
	engine := scheduler.New(&cfg.Checks, exec, db)

	fmt.Printf("Check interval: %s\n", interval)
	fmt.Printf("Services monitored: %v\n", cfg.Checks.Services)
	fmt.Printf("State DB: %s\n", stateDir+"/flare.db")

	// Immediate first run
	runChecks(engine, notifier, db, cfg)

	// Scheduled loop
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		runChecks(engine, notifier, db, cfg)
	}

	return nil
}

func runChecks(engine *scheduler.CheckEngine, notifier *alert.WebhookNotifier, db *state.DB, cfg *config.Config) {
	fmt.Printf("[%s] Running health checks...\n", time.Now().Format(time.RFC3339))
	results := engine.RunAll()

	anomalies := scheduler.AlertFromResults(results)
	if len(anomalies) == 0 {
		fmt.Println("All checks passed.")
		return
	}

	fmt.Printf("%d anomaly(ies) detected:\n", len(anomalies))
	for _, a := range anomalies {
		fmt.Printf("  - %s: %s (%s)\n", a.Name, a.Message, a.Status)
	}

	// Create alerts in DB
	for _, a := range anomalies {
		sev := state.SeverityWarning
		if a.Status == "critical" {
			sev = state.SeverityCritical
		}
		dbAlert, err := db.CreateAlert(a.Name, a.Message, sev)
		if err == nil && notifier != nil {
			err := notifier.Send(dbAlert)
			if err != nil {
				fmt.Printf("Alert notification failed: %v\n", err)
			}
		}
	}
}

func AlertCli(cfg *config.Config) error {
	if len(os.Args) < 4 {
		return fmt.Errorf("usage: flare alert <title> <body>")
	}
	title := os.Args[2]
	body := os.Args[3]

	// Store in DB
	stateDir := os.ExpandEnv("$HOME/.local/state/flare")
	db, err := state.Open(stateDir + "/flare.db")
	if err == nil {
		defer db.Close()
		db.CreateAlert(title, body, state.SeverityWarning)
	}

	// Send webhook
	if cfg.Alerts.Enabled && cfg.Alerts.WebhookURL != "" {
		notifier := alert.NewWebhookNotifier(cfg.Alerts.WebhookURL)
		return notifier.SendRaw(title, body)
	}
	return nil
}
