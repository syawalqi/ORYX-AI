package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/syawalqi/flare/alert"
	"github.com/syawalqi/flare/config"
	"github.com/syawalqi/flare/executor"
	"github.com/syawalqi/flare/memory"
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

	// Auto-fix escalation chain: canned → LLM ticket
	for _, a := range anomalies {
		fmt.Printf("  → Attempting auto-fix for %s...\n", a.Name)

		// Step 1: Try canned fix (in-process, no API cost)
		fixed := engine.Remediate(a)
		if fixed {
			fmt.Printf("  ✓ %s resolved by canned fix.\n", a.Name)
			continue
		}

		// Step 2: Canned fix failed or doesn't exist → create LLM fix ticket
		fmt.Printf("  → Canned fix insufficient for %s, escalating to LLM.\n", a.Name)

		// Capture system state for the ticket
		systemState := captureSystemState(engine)

		ticket, err := db.CreateTicket(a.Name, a.Status, a.Message, systemState)
		if err != nil {
			fmt.Printf("  ✗ Failed to create fix ticket: %v\n", err)
			// Fall through to direct alert
			sendAlert(notifiers, db, a)
			continue
		}

		fmt.Printf("  → Created fix ticket #%d\n", ticket.ID)

		// Step 3: Spawn flare fix in background (pipe ticket via stdin to avoid DB lock)
		go func(tid uint64, name string, ticket *state.FixTicket) {
			cmd := exec.Command("flare", "fix", "--stdin")
			stdin, err := cmd.StdinPipe()
			if err != nil {
				fmt.Printf("  [ticket #%d] stdin pipe error: %v\n", tid, err)
				return
			}

			// Write ticket JSON to stdin
			if err := json.NewEncoder(stdin).Encode(ticket); err != nil {
				fmt.Printf("  [ticket #%d] encode error: %v\n", tid, err)
				stdin.Close()
				return
			}
			stdin.Close()

			output, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("  [ticket #%d] flare fix exited: %v\n", tid, err)
			}
			if len(output) > 0 {
				fmt.Printf("  [ticket #%d] output:\n%s\n", tid, string(output))
			}
		}(ticket.ID, a.Name, ticket)
	}
}

// captureSystemState gathers current system snapshot for the fix ticket.
func captureSystemState(engine *scheduler.CheckEngine) string {
	exec := executor.New(30, 200, []string{})
	scanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	info, err := memory.Scan(scanCtx, exec)
	if err != nil {
		return fmt.Sprintf("scan error: %v", err)
	}
	return info.Render()
}

// sendAlert creates a DB alert and sends notifications.
func sendAlert(notifiers []alert.Notifier, db *state.DB, result scheduler.CheckResult) {
	sev := state.SeverityWarning
	switch result.Status {
	case "critical":
		sev = state.SeverityCritical
	case "warning":
		sev = state.SeverityWarning
	default:
		sev = state.SeverityInfo
	}

	dbAlert, err := db.CreateAlert(result.Name, result.Message, sev)
	if err == nil && len(notifiers) > 0 {
		for _, n := range notifiers {
			if err := n.Send(dbAlert); err != nil {
				fmt.Printf("  [%s] notification failed: %v\n", result.Name, err)
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
