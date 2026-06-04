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
		interval = 1 * time.Minute
	}

	// Setup notifiers based on delivery config
	notifiers := setupNotifiers(cfg)

	// Setup check engine
	engine := scheduler.New(&cfg.Checks, exec, db)

	fmt.Printf("Check interval: %s\n", interval)
	fmt.Printf("Services monitored: %v\n", cfg.Checks.Services)
	fmt.Printf("State DB: %s\n", stateDir+"/flare.db")
	fmt.Printf("Alert delivery: %s\n", cfg.Alerts.Delivery)
	if len(notifiers) > 0 {
		fmt.Printf("Notifiers active: %d\n", len(notifiers))
	}
	fmt.Printf("Anomaly checks: authfail, disk_growth, mem_growth, process\n")

	// Immediate first run
	runChecks(engine, notifiers, db, cfg)

	// Scheduled loop
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		runChecks(engine, notifiers, db, cfg)
	}

	return nil
}

// setupNotifiers creates alert notifiers based on config delivery mode.
func setupNotifiers(cfg *config.Config) []alert.Notifier {
	var notifiers []alert.Notifier

	switch cfg.Alerts.Delivery {
	case "telegram":
		if cfg.Alerts.TelegramToken != "" && cfg.Alerts.TelegramChat != "" {
			notifiers = append(notifiers, alert.NewTelegramNotifier(cfg.Alerts.TelegramToken, cfg.Alerts.TelegramChat))
			fmt.Println("  ✓ Telegram notifier active")
		} else {
			fmt.Println("  ⚠ Telegram delivery configured but token/chat missing, falling back to stdout")
		}
	case "webhook":
		if cfg.Alerts.WebhookURL != "" {
			notifiers = append(notifiers, alert.NewWebhookNotifier(cfg.Alerts.WebhookURL))
			fmt.Println("  ✓ Webhook notifier active")
		}
	case "all":
		if cfg.Alerts.TelegramToken != "" && cfg.Alerts.TelegramChat != "" {
			notifiers = append(notifiers, alert.NewTelegramNotifier(cfg.Alerts.TelegramToken, cfg.Alerts.TelegramChat))
			fmt.Println("  ✓ Telegram notifier active")
		}
		if cfg.Alerts.WebhookURL != "" {
			notifiers = append(notifiers, alert.NewWebhookNotifier(cfg.Alerts.WebhookURL))
			fmt.Println("  ✓ Webhook notifier active")
		}
	default:
		// "stdout" or anything else — just log to stdout
	}

	return notifiers
}

func runChecks(engine *scheduler.CheckEngine, notifiers []alert.Notifier, db *state.DB, cfg *config.Config) {
	fmt.Printf("[%s] Running health checks...\n", time.Now().Format(time.RFC3339))
	results := engine.RunAll()

	anomalies := scheduler.AlertFromResults(results)
	if len(anomalies) == 0 {
		fmt.Println("All checks passed.")
		return
	}

	fmt.Printf("%d anomaly(ies) detected:\n", len(anomalies))
	for _, a := range anomalies {
		fmt.Printf("  %s [%s]: %s\n", a.Name, a.Status, a.Message)
	}

	// Create alerts in DB and send notifications
	for _, a := range anomalies {
		sev := state.SeverityWarning
		switch a.Status {
		case "critical":
			sev = state.SeverityCritical
		case "warning":
			sev = state.SeverityWarning
		default:
			sev = state.SeverityInfo
		}

		dbAlert, err := db.CreateAlert(a.Name, a.Message, sev)
		if err == nil && len(notifiers) > 0 {
			for _, n := range notifiers {
				err := n.Send(dbAlert)
				if err != nil {
					fmt.Printf("  [%s] notification failed: %v\n", a.Name, err)
				}
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

	// Send via configured notifiers
	notifiers := setupNotifiers(cfg)
	for _, n := range notifiers {
		sa := &state.Alert{Title: title, Body: body, Severity: state.SeverityWarning}
		if err := n.Send(sa); err != nil {
			fmt.Fprintf(os.Stderr, "alert send error: %v\n", err)
		}
	}
	return nil
}
