package telegrambot

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/syawalqi/oryx/llm"
)

// handleMessage processes one Telegram message through the agent loop.
func (b *Bot) handleMessage(ctx context.Context, msg *Message) {
	log.Printf("telegram bot: message from %d: %.50s", msg.Chat.ID, msg.Text)

	// /start or /help
	text := strings.TrimSpace(msg.Text)
	if text == "/start" || text == "/help" {
		help := "🤖 *ORYX Agent*\n\nI'm an AI agent. Send me any task and I'll use my tools to help.\n\nAvailable commands:\n`/plan <goal>` — Plan-and-Execute mode\n`/status` — Show agent status\n`/help` — This message"
		b.client.sendMessage(msg.Chat.ID, help)
		return
	}

	// Build the agent request
	systemPrompt := b.prompt
	var msgs []llm.Message

	// /plan prefix triggers plan mode
	if strings.HasPrefix(text, "/plan ") {
		goal := strings.TrimPrefix(text, "/plan ")
		systemPrompt += "\n\n⚠️ PLAN MODE ACTIVE — You will decompose the goal into steps and execute them sequentially."
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: goal})
	} else {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: text})
	}

	// Inform user we're processing
	b.client.sendMessage(msg.Chat.ID, "⏳ Processing...")

	// Run the agent
	resultCh, err := b.agent.RunStream(ctx, systemPrompt, msgs)
	if err != nil {
		errMsg := fmt.Sprintf("⚠️ Error: %v", err)
		b.client.sendMessage(msg.Chat.ID, errMsg)
		return
	}

	// Collect the full response
	var response strings.Builder
	var reasoning strings.Builder

	for result := range resultCh {
		if result.Err != nil {
			response.WriteString(fmt.Sprintf("\n\n⚠️ Error: %v", result.Err))
			break
		}
		if result.Revised {
			// Reflexion replaced the response — discard original, start fresh
			response.Reset()
			reasoning.Reset()
		}
		if result.Token != "" {
			response.WriteString(result.Token)
		}
		if result.Reasoning != "" {
			reasoning.WriteString(result.Reasoning)
		}
		if result.Done {
			break
		}
	}

	final := strings.TrimSpace(response.String())
	if final == "" {
		final = "Done."
	}

	// Telegram has 4096 char limit — split if needed
	const maxLen = 4000
	if len(final) > maxLen {
		// Send first part with "… (truncated)" note
		b.client.sendMessage(msg.Chat.ID, final[:maxLen]+"\n\n… _(response truncated)_")
	} else {
		b.client.sendMessage(msg.Chat.ID, final)
	}
}
