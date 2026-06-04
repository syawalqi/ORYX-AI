package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/syawalqi/flare/state"
)

// TelegramNotifier sends alerts via Telegram Bot API.
type TelegramNotifier struct {
	token  string
	chatID string
	client *http.Client
}

// NewTelegramNotifier creates a notifier that delivers to a Telegram chat.
func NewTelegramNotifier(token, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		token:  token,
		chatID: chatID,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send delivers an alert to the configured Telegram chat.
func (n *TelegramNotifier) Send(alert *state.Alert) error {
	var emoji string
	switch alert.Severity {
	case state.SeverityCritical:
		emoji = "🔴"
	case state.SeverityWarning:
		emoji = "🟡"
	case state.SeverityInfo:
		emoji = "ℹ️"
	default:
		emoji = "🚨"
	}

	title := alert.Title
	if alert.Severity == state.SeverityCritical || alert.Severity == state.SeverityWarning {
		title = fmt.Sprintf("*%s* (severity: %s)", alert.Title, alert.Severity)
	}

	text := fmt.Sprintf("%s Flare Alert\n%s\n\n%s", emoji, title, alert.Body)
	return n.SendRaw(text)
}

// SendRaw sends a plain text message to Telegram.
func (n *TelegramNotifier) SendRaw(text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.token)

	body := map[string]interface{}{
		"chat_id":    n.chatID,
		"text":       text,
		"parse_mode": "Markdown",
		"disable_web_page_preview": true,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("telegram marshal: %w", err)
	}

	resp, err := n.client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("telegram post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram returned %d", resp.StatusCode)
	}
	return nil
}
