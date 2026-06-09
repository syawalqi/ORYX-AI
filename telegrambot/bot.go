// Package telegrambot provides a Telegram bot that runs the ORYX agent loop
// over Telegram messages. It uses long-polling (getUpdates) so no webhook
// or public endpoint is needed.
package telegrambot

import (
	"context"
	"log"
	"time"

	"github.com/syawalqi/oryx/agent"
	"github.com/syawalqi/oryx/state"
)

// Bot runs the ORYX agent loop over Telegram.
type Bot struct {
	token   string
	agent   *agent.Agent
	db      *state.DB
	client  *httpClient
	offset  int
	prompt  string
}

// New creates a Telegram bot that uses the given agent for responses.
func New(token string, ag *agent.Agent, db *state.DB, systemPrompt string) *Bot {
	return &Bot{
		token:  token,
		agent:  ag,
		db:     db,
		client: newHTTPClient(token),
		prompt: systemPrompt,
	}
}

// Run starts the long-polling loop. Blocks until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	log.Printf("telegram bot: starting long-polling (token=bot%s...)", b.token[:8])

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := b.client.getUpdates(ctx, b.offset, 30)
		if err != nil {
			log.Printf("telegram bot: getUpdates error: %v (retry in 5s)", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, upd := range updates {
			b.offset = upd.ID + 1
			if upd.Message == nil || upd.Message.Text == "" {
				continue
			}

			// Handle each message in a goroutine
			go b.handleMessage(ctx, upd.Message)
		}

		// Small pause between polls if no updates
		if len(updates) == 0 {
			time.Sleep(1 * time.Second)
		}
	}
}
