package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/syawalqi/flare/agent"
	"github.com/syawalqi/flare/config"
	"github.com/syawalqi/flare/executor"
	"github.com/syawalqi/flare/llm"
	"github.com/syawalqi/flare/memory"
	"github.com/syawalqi/flare/state"
)

func Fix(cfg *config.Config) error {
	// Parse args
	useStdin := false
	ticketID := uint64(0)
	useLatest := false

	for i, arg := range os.Args {
		switch {
		case arg == "--stdin":
			useStdin = true
		case arg == "--ticket" && i+1 < len(os.Args):
			id, err := strconv.ParseUint(os.Args[i+1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid ticket ID: %s", os.Args[i+1])
			}
			ticketID = id
		case arg == "--latest":
			useLatest = true
		}
	}

	// Get ticket: from stdin (daemon spawn), from DB (manual), or from args
	var ticket *state.FixTicket

	switch {
	case useStdin:
		// Read ticket JSON from stdin (daemon spawning us — avoids DB lock)
		t := &state.FixTicket{}
		if err := json.NewDecoder(os.Stdin).Decode(t); err != nil {
			return fmt.Errorf("read ticket from stdin: %w", err)
		}
		ticket = t

	case useLatest || ticketID > 0:
		// Manual use — try DB (only works when daemon isn't running)
		db, err := tryOpenDB()
		if err != nil {
			return fmt.Errorf("cannot open state DB (daemon may be running): %w\nTry: flare fix --stdin (pipe ticket JSON)", err)
		}
		defer db.Close()

		if useLatest {
			tickets, err := db.GetUnresolvedTickets()
			if err != nil || len(tickets) == 0 {
				fmt.Println("No unresolved tickets found.")
				return nil
			}
			ticket = &tickets[0]
		} else {
			t, err := db.GetTicket(ticketID)
			if err != nil {
				return fmt.Errorf("ticket %d: %w", ticketID, err)
			}
			ticket = t
		}

		if ticket.Resolved {
			fmt.Printf("Ticket %d is already resolved.\n", ticket.ID)
			return nil
		}

	default:
		return fmt.Errorf("usage: flare fix --ticket <id> | flare fix --latest | flare fix --stdin (pipe JSON)")
	}

	fmt.Printf("🛠 Fixing ticket #%d: %s — %s\n", ticket.ID, ticket.CheckName, ticket.Message)

	// Setup provider, executor, and agent
	prov := getProvider(cfg)
	exec := executor.New(cfg.Executor.Timeout, cfg.Executor.MaxOutputLines, cfg.Executor.BlockedCommands)

	// Run a fresh system scan
	scanCtx, scanCancel := context.WithTimeout(context.Background(), 30*time.Second)
	info, scanErr := memory.Scan(scanCtx, exec)
	scanCancel()

	systemState := "System scan unavailable."
	if scanErr == nil && info != nil {
		systemState = info.Render()
	}

	// Build the fix prompt
	systemPrompt := buildFixPrompt(ticket, systemState)

	// Create agent with iterations limit
	maxIter := 50
	temperature := 0.3
	maxTokens := 4096

	ag := agent.New(prov, exec, cfg.Model, maxTokens, temperature, maxIter)

	// Run the fix loop
	fmt.Printf("📡 Running LLM fix agent (max %d iterations)...\n", maxIter)

	ctx := context.Background()
	userMsg := llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf(
		"Fix this issue: %s\n\nDetails: %s",
		ticket.CheckName, ticket.Message)}

	ch, err := ag.RunStream(ctx, systemPrompt, []llm.Message{userMsg})
	if err != nil {
		return fmt.Errorf("agent run: %w", err)
	}

	// Drain the channel (collect log for debugging)
	var logBuf strings.Builder
	for result := range ch {
		if result.Token != "" {
			logBuf.WriteString(result.Token)
		}
		if result.Reasoning != "" {
			logBuf.WriteString("[reasoning] " + result.Reasoning + "\n")
		}
		if result.ToolResult != nil {
			logBuf.WriteString(fmt.Sprintf("[tool %s] %s\n", result.ToolResult.Name, result.ToolResult.Output))
		}
		if result.Err != nil {
			logBuf.WriteString("[error] " + result.Err.Error() + "\n")
		}
	}

	// Verify fix
	fmt.Println("  → Verifying fix...")
	if verifyFix(exec, ticket.CheckName) {
		fmt.Printf("\n✅ Ticket #%d resolved!\n", ticket.ID)
		// Try to mark resolved in DB (best-effort, may fail if daemon is running)
		if db, err := tryOpenDB(); err == nil {
			db.MarkResolved(ticket.ID)
			db.Close()
		}
		return nil
	}

	// Log what the LLM tried
	stateDir := os.ExpandEnv("$HOME/.local/state/flare")
	os.MkdirAll(stateDir, 0755)
	logPath := fmt.Sprintf("%s/fix-%d-attempt.log", stateDir, ticket.ID)
	os.WriteFile(logPath, []byte(logBuf.String()), 0644)
	fmt.Printf("  → Fix log saved to %s\n", logPath)

	// Notify user
	msg := fmt.Sprintf("🔴 Flare fix failed for ticket #%d\nCheck: %s\nMessage: %s\nCould not fix after %d LLM iterations. Log: %s",
		ticket.ID, ticket.CheckName, ticket.Message, maxIter, logPath)

	fmt.Println(msg)
	return fmt.Errorf("fix failed: see log at %s", logPath)
}

// tryOpenDB attempts to open the state DB with a short timeout.
func tryOpenDB() (*state.DB, error) {
	stateDir := os.ExpandEnv("$HOME/.local/state/flare")
	return state.Open(stateDir + "/flare.db")
}

// buildFixPrompt creates the system prompt for the LLM fix agent.
func buildFixPrompt(ticket *state.FixTicket, systemState string) string {
	var b strings.Builder

	b.WriteString(`You are Flare, a server management agent running on a Linux VPS.
Your task is to diagnose and fix a server anomaly.

## Anomaly Details
`)
	b.WriteString(fmt.Sprintf("- Check: %s\n", ticket.CheckName))
	b.WriteString(fmt.Sprintf("- Severity: %s\n", ticket.Severity))
	b.WriteString(fmt.Sprintf("- Message: %s\n", ticket.Message))
	b.WriteString("\n")

	b.WriteString("## Current System State\n")
	b.WriteString(systemState)
	b.WriteString("\n")

	b.WriteString(`## Tools Available
- run_command: Execute shell commands
- read_file: Read file contents
- write_file: Write to files (for config changes)
- service_action: Start/stop/restart systemd services
- search_logs: Search journald logs

## Rules
1. Diagnose the root cause before attempting a fix
2. After each fix attempt, verify by running the relevant check
3. If a fix fails, try a different approach
4. NEVER run: rm -rf, mkfs, dd, or format commands
5. NEVER install new packages without explicit permission
6. You have no user to ask — act autonomously within these rules
7. If you're stuck, explain what you've tried

## Fix Strategy
1. First: diagnose (check logs, disk usage, process state)
2. Then: apply the fix (clean up, restart, reconfigure)
3. Finally: verify (re-run the check that flagged the anomaly)
`)

	return b.String()
}

// verifyFix runs the relevant check to see if the anomaly is resolved.
func verifyFix(exec *executor.Executor, checkName string) bool {
	ctx := context.Background()

	switch {
	case strings.HasPrefix(checkName, "service:"):
		svcName := strings.TrimPrefix(checkName, "service:")
		r, err := exec.Run(ctx, fmt.Sprintf("systemctl is-active %s 2>/dev/null", svcName))
		if err != nil {
			return false
		}
		return strings.TrimSpace(r.Stdout) == "active"

	case checkName == "disk" || checkName == "disk_growth":
		r, err := exec.Run(ctx, "df -P / 2>/dev/null | tail -1 | awk '{print $5}' | tr -d '%'")
		if err != nil {
			return false
		}
		usage := 0
		fmt.Sscanf(r.Stdout, "%d", &usage)
		return usage < 85 // below warning threshold

	case checkName == "memory" || checkName == "mem_growth":
		r, err := exec.Run(ctx, "free -m 2>/dev/null | awk '/Mem:/ {printf \"%.0f\", $3/$2 * 100}'")
		if err != nil {
			return false
		}
		usage := 0
		fmt.Sscanf(r.Stdout, "%d", &usage)
		return usage < 85

	case checkName == "authfail":
		r, err := exec.Run(ctx, "journalctl -u sshd --since \"-5m\" --no-pager 2>/dev/null | grep -c 'Failed password'")
		if err != nil {
			return false
		}
		count := 0
		fmt.Sscanf(strings.TrimSpace(r.Stdout), "%d", &count)
		return count < 5 // less than 5 fails in 5m is fine

	default:
		// Unknown check — assume not fixed
		return false
	}
}

// init registers the fix subcommand in the config defaults.
func init() {
	// No-op; registration done via main.go switch
}
