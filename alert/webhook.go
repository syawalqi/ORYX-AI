package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/syawalqi/flare/state"
)

type WebhookNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewWebhookNotifier(webhookURL string) *WebhookNotifier {
	return &WebhookNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *WebhookNotifier) Send(alert *state.Alert) error {
	return n.SendRaw(alert.Title, alert.Body)
}

func (n *WebhookNotifier) SendRaw(title, body string) error {
	payload := map[string]interface{}{
		"text": fmt.Sprintf("*Flare Alert*\n%s\n\n%s", title, body),
	}

	// Slack-compatible format
	slackPayload := map[string]interface{}{
		"text":       fmt.Sprintf("🚨 *Flare Alert*: %s", title),
		"attachments": []map[string]interface{}{
			{
				"text":  body,
				"color": "#EF4444",
				"ts":    time.Now().Unix(),
			},
		},
	}

	data, err := json.Marshal(slackPayload)
	if err != nil {
		// Fallback to simple format
		data, _ = json.Marshal(payload)
	}

	resp, err := n.client.Post(n.webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}
